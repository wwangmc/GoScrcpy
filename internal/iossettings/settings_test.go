package iossettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
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

func TestExtensionPresetsAndPayloadArePreserved(t *testing.T) {
	choices, err := Choices()
	if err != nil || len(choices) != 47 {
		t.Fatalf("choices=%d error=%v", len(choices), err)
	}
	original, _ := presets.ReadFile("data/opt.json")
	var operators map[string]json.RawMessage
	_ = json.Unmarshal(original, &operators)
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
		key := choice.Region
		if key == "KW" {
			key = "kw"
		}
		var want, actual any
		_ = json.Unmarshal(operators[key], &want)
		raw, err := json.Marshal(r.body)
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &actual)
		if !reflect.DeepEqual(want, actual) {
			t.Fatalf("carrier payload changed for %s", choice.Region)
		}
	}
	us, err := Lookup(" us ")
	if err != nil || us.Locale != "en_US" || us.Language != "en-US" || us.TimeZone != "America/New_York" || us.CarrierCount != 13 {
		t.Fatalf("%+v %v", us, err)
	}
	if kw, err := Lookup("kw"); err != nil || kw.Region != "KW" {
		t.Fatal("lowercase carrier key was not normalized")
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
