package iossettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

type recorder struct {
	calls int
	path  string
	body  any
	err   error
}

func (r *recorder) JSON(_ context.Context, path string, body any) (string, error) {
	r.calls++
	r.path = path
	r.body = body
	return "", r.err
}

func TestSystemPresetsNeverSendApplicationConfiguration(t *testing.T) {
	choices, err := Choices()
	if err != nil || len(choices) != 47 {
		t.Fatalf("choices=%d error=%v", len(choices), err)
	}
	for _, choice := range choices {
		r := &recorder{}
		got, err := Apply(context.Background(), r, choice.Region)
		if err != nil || got != choice || r.calls != 1 {
			t.Fatalf("%s: %v", choice.Region, err)
		}
		u, err := url.Parse(r.path)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/setAllSetting" || u.Query().Get("timeZone") != choice.TimeZone || u.Query().Get("locale") != choice.Locale || u.Query().Get("lang") != choice.Language || len(u.Query()) != 3 {
			t.Fatal("extension query parameters changed", r.path)
		}
		raw, err := json.Marshal(r.body)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "{}" {
			t.Fatalf("application configuration sent for %s: %s", choice.Region, raw)
		}
	}
	us, err := Lookup(" us ")
	if err != nil || us.Name == "" || us.Locale != "en_US" || us.Language != "en-US" || us.TimeZone != "America/New_York" {
		t.Fatalf("%+v %v", us, err)
	}
	if kw, err := Lookup("kw"); err != nil || kw.Region != "KW" {
		t.Fatal("lowercase region key was not normalized")
	}
}

func TestInvalidRegionNeverTouchesPhoneAndFailureIsNotRetried(t *testing.T) {
	r := &recorder{}
	for _, region := range []string{"", "CN", "US&lang=bad", "UNKNOWN"} {
		if _, err := Apply(context.Background(), r, region); err == nil || r.calls != 0 {
			t.Fatal("sent unsupported settings")
		}
	}
	r.err = errors.New("uncertain response")
	if _, err := Apply(context.Background(), r, "US"); !errors.Is(err, r.err) || r.calls != 1 {
		t.Fatal("retried or hid uncertain setting mutation")
	}
}
