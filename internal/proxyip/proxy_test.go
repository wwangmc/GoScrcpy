package proxyip

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProviderPayloadAndPublicFields(t *testing.T) {
	lines, skipped, err := ParseLines([]byte(`{"status":200,"data":[{"name":"test","hostname":"proxy.example","port":"9000","usernameFormat":"u-{region}-{uuid}","password":"secret"},{"hostname":"bad;cmd","port":9000}]}`))
	if err != nil || len(lines) != 1 || skipped != 1 {
		t.Fatalf("lines=%d skipped=%d error=%v", len(lines), skipped, err)
	}
	b, _ := json.Marshal(lines[0].Public())
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "username") {
		t.Fatal("provider secrets reached UI")
	}
	for _, s := range []string{`{"status":500,"data":[]}`, `{"status":200,"data":null}`, `{"status":200,"data":[{}]}`} {
		if _, _, err := ParseLines([]byte(s)); err == nil {
			t.Fatal("accepted malformed provider response")
		}
	}
}

func TestClashNestedURLPreservesParametersAndYAMLScalars(t *testing.T) {
	line := Line{Name: "线路 A&B", Hostname: "proxy.example", Port: 9000, UsernameFormat: "u-{region}-{uuid}", Password: "p:{region}&{uuid}#'\""}
	o := Defaults()
	o.AppID = "com.mobile.legends"
	o.Name = "cs"
	config, err := Build(line, o, "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	outer, err := url.Parse(config.ClashURL)
	if err != nil {
		t.Fatal(err)
	}
	if outer.Scheme != "clash" || outer.Host != "install-config" || len(outer.Query()) != 2 {
		t.Fatal("invalid outer scheme")
	}
	inner, err := url.Parse(outer.Query().Get("url"))
	if err != nil {
		t.Fatal(err)
	}
	if inner.Scheme+"://"+inner.Host+inner.Path != ClashEndpoint || inner.Query().Get("appid") != "com.mobile.legends" || inner.Query().Get("name1") != "cs" || len(inner.Query()) != 3 {
		t.Fatal("nested parameters lost")
	}
	fragment := inner.Query().Get("proxies1")
	if !strings.Contains(fragment, `server: "proxy.example", port: 9000, type: socks5`) || !strings.Contains(fragment, `username: "u-US-123456789012"`) || !strings.Contains(fragment, `password: "p:US\u0026123456789012#'\""`) {
		t.Fatalf("wrong YAML scalar quoting: %s", fragment)
	}
	shadow, err := url.Parse(config.ShadowURL)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimPrefix(strings.Split(config.ShadowURL, "?")[0], "shadowrocket://add/socks://")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "u-US-123456789012:p:US&123456789012") || shadow.Query().Get("remarks") != "DAILI_线路 A&B_US" {
		t.Fatal("Shadowrocket credentials or remarks changed")
	}
	line.Password = "{unsupported}"
	if _, err = Build(line, o, "12345"); err == nil {
		t.Fatal("accepted unresolved placeholder")
	}
	o.AppID = "com.test;bad"
	if _, err = Normalize(o); err == nil {
		t.Fatal("accepted invalid app id")
	}
}

func TestClashURLSupportsPercentOnlyDecodingAtBothLevels(t *testing.T) {
	line := Line{Name: "线路 A+B & C", Hostname: "proxy.example", Port: 443,
		UsernameFormat: "user+ space-{region}-{uuid}", Password: "p+ space%20:#"}
	o := Defaults()
	o.Name = "cs A+B & C"
	config, err := Build(line, o, "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	outer, err := url.Parse(config.ClashURL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(outer.RawQuery, "+") || !strings.Contains(outer.RawQuery, "%2520") {
		t.Fatal("outer URI must preserve the inner percent-encoded spaces")
	}
	// A percent-only URI decoder intentionally differs from url.QueryUnescape:
	// the latter would hide this regression by accepting '+' for a space.
	decode := func(rawQuery, key string) string {
		t.Helper()
		for _, field := range strings.Split(rawQuery, "&") {
			k, v, _ := strings.Cut(field, "=")
			if k != key {
				continue
			}
			value, err := url.PathUnescape(v)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		t.Fatalf("missing query parameter %s", key)
		return ""
	}
	if got := decode(outer.RawQuery, "name"); got != "DAILI_线路 A+B & C_US_123456789012" {
		t.Fatal("outer URI changed spaces, literal plus signs or ampersands")
	}
	inner, err := url.Parse(decode(outer.RawQuery, "url"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(inner.RawQuery, "+") || !strings.Contains(inner.RawQuery, "%20") || !strings.Contains(inner.RawQuery, "%2B") {
		t.Fatal("download URL must distinguish spaces from literal plus signs")
	}
	if got := decode(inner.RawQuery, "name1"); got != o.Name {
		t.Fatal("config name changed")
	}
	want := `server: "proxy.example", port: 443, type: socks5, username: "user+ space-US-123456789012", password: "p+ space%20:#"`
	if got := decode(inner.RawQuery, "proxies1"); got != want {
		t.Fatal("configuration changed after percent-only decoding")
	}
	if inner.Query().Get("proxies1") != want {
		t.Fatal("standard HTTP query decoding changed the configuration")
	}
}

func TestShellAndIPResponseValidation(t *testing.T) {
	if got := ShellQuote("a'b&c"); got != `'a'"'"'b&c'` {
		t.Fatalf("unsafe quote %q", got)
	}
	data, err := ShellOutput("{\"status\":\"success\",\"query\":\"1.2.3.4\"}\n"+exitMarker+"0\n", true)
	if err != nil {
		t.Fatal(err)
	}
	ip, err := ParseIP(data)
	if err != nil || ip.Query != "1.2.3.4" {
		t.Fatalf("%+v %v", ip, err)
	}
	for _, output := range []string{"no marker", "\n" + exitMarker + "bad", "\n" + exitMarker + "28"} {
		if _, err := ShellOutput(output, true); err == nil {
			t.Fatal("accepted uncertain device command")
		}
	}
	if _, err = ShellOutput("curl: not found\n"+exitMarker+"127", true); !errors.Is(err, ErrCurlMissing) {
		t.Fatal("missing curl not explained")
	}
	if _, err = ParseIP(`{"status":"fail","query":"1.2.3.4"}`); err == nil {
		t.Fatal("accepted failed IP lookup")
	}
}

type fakeAPI struct {
	calls []string
	fail  string
	vpn   string
}

func (a *fakeAPI) JSON(_ context.Context, path string, body any) (string, error) {
	a.calls = append(a.calls, path)
	if a.fail == path {
		return "", errors.New("uncertain response")
	}
	if path == "/runShell" {
		if !strings.Contains(body.(map[string]string)["cmd"], "uiopen '") {
			return "", errors.New("unquoted URL")
		}
		return "\n" + exitMarker + "0\n", nil
	}
	if path == "/vpnStatus" {
		return a.vpn, nil
	}
	return "", nil
}
func TestIOSOrderAndFailureStopsFurtherMutations(t *testing.T) {
	pause := func(context.Context, time.Duration) error { return nil }
	config := Import{ShadowURL: "shadowrocket://add/socks://test"}
	a := &fakeAPI{vpn: "open"}
	if err := ChangeIOS(context.Background(), a, config, pause); err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.calls, ",") != "/turnOffVPN,/switchApp?pkgName=com.liguangming.Shadowrocket,/runShell,/runShell,/turnOnVPN,/vpnStatus" {
		t.Fatal(a.calls)
	}
	a = &fakeAPI{fail: "/runShell"}
	if err := ChangeIOS(context.Background(), a, config, pause); err == nil || len(a.calls) != 3 {
		t.Fatal("continued after uncertain import")
	}
	a = &fakeAPI{vpn: "close"}
	if err := ChangeIOS(context.Background(), a, config, pause); err == nil {
		t.Fatal("claimed success without VPN")
	}
}
