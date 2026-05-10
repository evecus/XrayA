package node

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseLink detects protocol and dispatches to the correct parser.
func ParseLink(link string) (*Node, error) {
	link = strings.TrimSpace(link)
	switch {
	case strings.HasPrefix(link, "vmess://"):
		return parseVMess(link)
	case strings.HasPrefix(link, "vless://"):
		return parseVLESS(link)
	case strings.HasPrefix(link, "trojan://"):
		return parseTrojan(link)
	case strings.HasPrefix(link, "ss://"):
		return parseShadowsocks(link)
	case strings.HasPrefix(link, "hysteria2://"),
		strings.HasPrefix(link, "hy2://"):
		return parseHysteria2(link)
	case strings.HasPrefix(link, "socks5://"):
		return parseSocks5(link)
	case strings.HasPrefix(link, "http://"), strings.HasPrefix(link, "https://"):
		return parseHTTP(link)
	}
	return nil, fmt.Errorf("unsupported protocol: %s", link)
}

// ─── VMess ────────────────────────────────────────────────────────────────────

func parseVMess(link string) (*Node, error) {
	b64 := strings.TrimPrefix(link, "vmess://")
	raw, err := base64Decode(b64)
	if err != nil {
		return nil, fmt.Errorf("vmess base64: %w", err)
	}
	var v struct {
		Add  string      `json:"add"`
		Port interface{} `json:"port"`
		ID   string      `json:"id"`
		Aid  interface{} `json:"aid"`
		Scy  string      `json:"scy"`
		Net  string      `json:"net"`
		TLS  string      `json:"tls"`
		SNI  string      `json:"sni"`
		Path string      `json:"path"`
		Host string      `json:"host"`
		Type string      `json:"type"` // headerType
		PS   string      `json:"ps"`
		Flow string      `json:"flow"`
		Fp   string      `json:"fp"`
		GrpcSvc string   `json:"grpc-service-name"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("vmess json: %w", err)
	}
	port := toInt(v.Port)
	aid := toInt(v.Aid)
	name := v.PS
	if name == "" {
		name = fmt.Sprintf("%s:%d", v.Add, port)
	}
	extra, _ := json.Marshal(ExtraVMess{
		UUID:     v.ID,
		AlterId:  aid,
		Security: orDefault(v.Scy, "auto"),
		Network:  orDefault(v.Net, "tcp"),
		TLS:      v.TLS == "tls",
		SNI:      v.SNI,
		Path:     v.Path,
		Host:     v.Host,
		GrpcSvc:  v.GrpcSvc,
		Flow:     v.Flow,
		Fp:       v.Fp,
	})
	return &Node{
		Name: name, Protocol: ProtoVMess,
		Address: v.Add, Port: port,
		Link: link, Extra: extra,
	}, nil
}

// ─── VLESS ────────────────────────────────────────────────────────────────────

func parseVLESS(link string) (*Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	name, _ = url.QueryUnescape(name)
	extra, _ := json.Marshal(ExtraVLESS{
		UUID:       u.User.Username(),
		Flow:       q.Get("flow"),
		Encryption: orDefault(q.Get("encryption"), "none"),
		Network:    orDefault(q.Get("type"), "tcp"),
		TLS:        orDefault(q.Get("security"), "none"),
		SNI:        q.Get("sni"),
		Fp:         q.Get("fp"),
		PbKey:      q.Get("pbk"),
		ShortID:    q.Get("sid"),
		Path:       q.Get("path"),
		Host:       q.Get("host"),
		GrpcSvc:    q.Get("serviceName"),
		Insecure:   q.Get("allowInsecure") == "1",
	})
	return &Node{
		Name: name, Protocol: ProtoVLESS,
		Address: u.Hostname(), Port: port,
		Link: link, Extra: extra,
	}, nil
}

// ─── Trojan ───────────────────────────────────────────────────────────────────

func parseTrojan(link string) (*Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	name, _ = url.QueryUnescape(name)
	extra, _ := json.Marshal(ExtraTrojan{
		Password: u.User.Username(),
		SNI:      q.Get("sni"),
		Network:  orDefault(q.Get("type"), "tcp"),
		Path:     q.Get("path"),
		Host:     q.Get("host"),
		GrpcSvc:  q.Get("serviceName"),
		Insecure: q.Get("allowInsecure") == "1",
	})
	return &Node{
		Name: name, Protocol: ProtoTrojan,
		Address: u.Hostname(), Port: port,
		Link: link, Extra: extra,
	}, nil
}

// ─── Shadowsocks ──────────────────────────────────────────────────────────────

func parseShadowsocks(link string) (*Node, error) {
	// ss://BASE64(method:password)@host:port#name
	// or ss://BASE64(method:password@host:port)#name
	raw := strings.TrimPrefix(link, "ss://")
	name := ""
	if idx := strings.Index(raw, "#"); idx >= 0 {
		name, _ = url.QueryUnescape(raw[idx+1:])
		raw = raw[:idx]
	}
	var method, password, host string
	var port int

	if strings.Contains(raw, "@") {
		// SIP002
		u, err := url.Parse("ss://" + raw)
		if err != nil {
			return nil, err
		}
		userInfo := u.User.Username()
		if decoded, err := base64Decode(userInfo); err == nil {
			userInfo = string(decoded)
		}
		parts := strings.SplitN(userInfo, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("ss: invalid userinfo")
		}
		method, password = parts[0], parts[1]
		host = u.Hostname()
		port, _ = strconv.Atoi(u.Port())
		if name == "" {
			name = u.Fragment
		}
	} else {
		decoded, err := base64Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("ss base64: %w", err)
		}
		s := string(decoded)
		atIdx := strings.LastIndex(s, "@")
		if atIdx < 0 {
			return nil, fmt.Errorf("ss: missing @")
		}
		userInfo := s[:atIdx]
		hostPort := s[atIdx+1:]
		parts := strings.SplitN(userInfo, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("ss: invalid userinfo")
		}
		method, password = parts[0], parts[1]
		hpParts := strings.Split(hostPort, ":")
		host = strings.Join(hpParts[:len(hpParts)-1], ":")
		port, _ = strconv.Atoi(hpParts[len(hpParts)-1])
	}
	if name == "" {
		name = fmt.Sprintf("%s:%d", host, port)
	}
	extra, _ := json.Marshal(ExtraShadowsocks{
		Method: method, Password: password,
	})
	return &Node{
		Name: name, Protocol: ProtoShadowsocks,
		Address: host, Port: port,
		Link: link, Extra: extra,
	}, nil
}

// ─── Hysteria2 ────────────────────────────────────────────────────────────────

func parseHysteria2(link string) (*Node, error) {
	link = strings.Replace(link, "hy2://", "hysteria2://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	portStr := u.Port()
	if portStr == "" {
		portStr = "443"
	}
	port, _ := strconv.Atoi(portStr)
	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = u.Hostname()
	}
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	name, _ = url.QueryUnescape(name)
	extra, _ := json.Marshal(ExtraHysteria2{
		Password:  u.User.Username(),
		SNI:       sni,
		Insecure:  q.Get("insecure") == "1",
		Obfs:      q.Get("obfs"),
		ObfsParam: q.Get("obfs-password"),
		PinSHA256: q.Get("pinSHA256"),
	})
	return &Node{
		Name: name, Protocol: ProtoHysteria2,
		Address: u.Hostname(), Port: port,
		Link: link, Extra: extra,
	}, nil
}

// ─── SOCKS5 ───────────────────────────────────────────────────────────────────

func parseSocks5(link string) (*Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(u.Port())
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	pass, _ := u.User.Password()
	extra, _ := json.Marshal(ExtraSocks5{
		Username: u.User.Username(),
		Password: pass,
	})
	return &Node{
		Name: name, Protocol: ProtoSocks5,
		Address: u.Hostname(), Port: port,
		Link: link, Extra: extra,
	}, nil
}

// ─── HTTP proxy ───────────────────────────────────────────────────────────────

func parseHTTP(link string) (*Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		if u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("%s:%d", u.Hostname(), port)
	}
	return &Node{
		Name: name, Protocol: ProtoHTTP,
		Address: u.Hostname(), Port: port,
		Link: link,
	}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func base64Decode(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	for _, enc := range []encoding{base64.URLEncoding, base64.StdEncoding,
		base64.RawURLEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("base64 decode failed")
}

type encoding interface{ DecodeString(string) ([]byte, error) }

func toInt(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
