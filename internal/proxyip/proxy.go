// Package proxyip builds mobile proxy imports without exposing provider secrets to the UI.
package proxyip

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const ListURL = "http://47.96.125.116/wmc/ybProxy/getAll"
const ClashEndpoint = "http://47.96.125.116:8088/proxy/getclash"
const IPURL = "http://ip-api.com/json"

type Line struct {
	ID, Name, Hostname, UsernameFormat, Password string
	Port                                         int
}
type PublicLine struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

func (l Line) Public() PublicLine { return PublicLine{l.ID, l.Name, l.Hostname, l.Port} }

type Options struct {
	ProxyID string `json:"proxyId"`
	Region  string `json:"region"`
	AppID   string `json:"appId"`
	Name    string `json:"name"`
}

func Defaults() Options { return Options{Region: "US", AppID: "com.mobile.legends", Name: "cs"} }

var hostPattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
var appPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+$`)
var regionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func Normalize(o Options) (Options, error) {
	o.Region = strings.ToUpper(strings.TrimSpace(o.Region))
	o.AppID = strings.TrimSpace(o.AppID)
	o.Name = strings.TrimSpace(o.Name)
	if !regionPattern.MatchString(o.Region) {
		return o, errors.New("请输入两位地区代码，例如 US、JP、HK")
	}
	if !appPattern.MatchString(o.AppID) || len(o.AppID) > 200 {
		return o, errors.New("请输入有效的安卓应用包名，例如 com.mobile.legends")
	}
	if o.Name == "" || len(o.Name) > 100 || strings.ContainsAny(o.Name, "\x00\r\n") {
		return o, errors.New("节点名称不能为空，且不能包含换行")
	}
	return o, nil
}

func ParseLines(data []byte) ([]Line, int, error) {
	var payload struct {
		Status json.RawMessage `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, 0, errors.New("代理接口没有返回有效 JSON")
	}
	if string(payload.Status) != "200" && string(payload.Status) != `"200"` && string(payload.Status) != `"success"` {
		return nil, 0, errors.New("代理接口返回失败，请稍后刷新")
	}
	var rows []json.RawMessage
	if len(payload.Data) == 0 || payload.Data[0] != '[' || json.Unmarshal(payload.Data, &rows) != nil {
		return nil, 0, errors.New("代理接口的 data 不是列表")
	}
	lines := make([]Line, 0, len(rows))
	for index, raw := range rows {
		var row struct {
			Name           string
			Hostname       string
			Port           json.RawMessage
			UsernameFormat string
			Password       *string
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		p := strings.Trim(string(row.Port), `"`)
		port, err := strconv.Atoi(p)
		host := strings.TrimSpace(row.Hostname)
		if err != nil || port < 1 || port > 65535 || !hostPattern.MatchString(host) || row.UsernameFormat == "" || row.Password == nil {
			continue
		}
		name := row.Name
		if name == "" {
			name = fmt.Sprintf("代理 %d", index+1)
		}
		key, _ := json.Marshal([]any{name, host, port, index})
		sum := sha256.Sum256(key)
		lines = append(lines, Line{ID: hex.EncodeToString(sum[:]), Name: name, Hostname: host, Port: port, UsernameFormat: row.UsernameFormat, Password: *row.Password})
	}
	if len(rows) > 0 && len(lines) == 0 {
		return nil, len(rows), errors.New("代理列表没有有效记录，请检查接口字段")
	}
	return lines, len(rows) - len(lines), nil
}

func NewSession() (string, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%012d", n), nil
}

type Import struct{ ShadowURL, ClashURL, Session, Region string }

// Custom URI handlers may percent-decode query parameters without applying
// application/x-www-form-urlencoded's '+' -> space rule. Literal '+' is already
// escaped as %2B by Values.Encode; only its encoded spaces need replacement.
func percentQuery(values url.Values) string {
	return strings.ReplaceAll(values.Encode(), "+", "%20")
}

func Build(line Line, options Options, session string) (Import, error) {
	o, err := Normalize(options)
	if err != nil {
		return Import{}, err
	}
	if len(session) < 5 || len(session) > 20 || strings.Trim(session, "0123456789") != "" {
		return Import{}, errors.New("无效代理会话")
	}
	if !hostPattern.MatchString(line.Hostname) || line.Port < 1 || line.Port > 65535 {
		return Import{}, errors.New("无效代理服务器")
	}
	fill := func(value string) (string, error) {
		value = strings.ReplaceAll(strings.ReplaceAll(value, "{region}", o.Region), "{uuid}", session)
		if strings.ContainsAny(value, "{}\x00\r\n") {
			return "", errors.New("代理账号或密码含有不支持的占位符或无效字符")
		}
		return value, nil
	}
	user, err := fill(line.UsernameFormat)
	if err != nil {
		return Import{}, err
	}
	pass, err := fill(line.Password)
	if err != nil {
		return Import{}, err
	}
	remarks := "DAILI_" + line.Name + "_" + o.Region
	shadow := "shadowrocket://add/socks://" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s@%s:%d", user, pass, line.Hostname, line.Port))) + "?" + url.Values{"remarks": {remarks}, "obfs": {"none"}}.Encode()
	// JSON quoted scalars are valid YAML scalars, including passwords with ':' or '#'.
	quote := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	fragment := fmt.Sprintf("server: %s, port: %d, type: socks5, username: %s, password: %s", quote(line.Hostname), line.Port, quote(user), quote(pass))
	configURL := ClashEndpoint + "?" + percentQuery(url.Values{"appid": {o.AppID}, "name1": {o.Name}, "proxies1": {fragment}})
	// Encode the complete download URL again so its '&' stay inside the url parameter.
	clash := "clash://install-config?" + percentQuery(url.Values{"url": {configURL}, "name": {remarks + "_" + session}})
	return Import{shadow, clash, session, o.Region}, nil
}

func ShellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }

const exitMarker = "__GOSCRCPY_PROXY_EXIT__:"

var ErrCurlMissing = errors.New("手机未安装 curl，请先在手机上安装 curl 后查询出口 IP")

func ShellCommand(command string) string {
	return command + ` 2>&1; gs_proxy_exit=$?; printf '\n` + exitMarker + `%s\n' "$gs_proxy_exit"`
}

func CurlQueryCommand(binary string) string {
	return ShellCommand(ShellQuote(binary) + " -fsS --connect-timeout 8 --max-time 20 " + ShellQuote(IPURL))
}
func ShellOutput(output string, query bool) (string, error) {
	i := strings.LastIndex(output, "\n"+exitMarker)
	if i < 0 {
		return "", errors.New("手机命令未返回完成标记，无法确认执行结果")
	}
	code, err := strconv.Atoi(strings.TrimSpace(output[i+len(exitMarker)+1:]))
	if err != nil {
		return "", errors.New("手机命令返回无效退出码")
	}
	if code != 0 {
		if query && code == 127 {
			return "", ErrCurlMissing
		}
		return "", fmt.Errorf("手机命令执行失败（退出码 %d），请检查%s", code, map[bool]string{true: "curl 和手机网络", false: "uiopen 和 Shadowrocket"}[query])
	}
	return strings.TrimSpace(output[:i]), nil
}

type IPResult struct {
	MissingCurl bool   `json:"missingCurl,omitempty"`
	Query       string `json:"query"`
	Country     string `json:"country"`
	RegionName  string `json:"regionName"`
	City        string `json:"city"`
	ISP         string `json:"isp"`
	Org         string `json:"org"`
	Timezone    string `json:"timezone"`
}

func ParseIP(output string) (IPResult, error) {
	var result struct {
		IPResult
		Status string `json:"status"`
	}
	if json.Unmarshal([]byte(output), &result) != nil || result.Status != "success" || result.Query == "" {
		return IPResult{}, errors.New("手机出口 IP 查询失败，返回数据无效或请求过于频繁")
	}
	return result.IPResult, nil
}
