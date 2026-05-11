package builder

import (
	"encoding/json"
	"fmt"

	"github.com/xraya/xraya/internal/node"
)

// RouteMode defines the traffic splitting strategy.
type RouteMode string

const (
	RouteModeWhitelist RouteMode = "whitelist" // mainland whitelist (proxy non-CN)
	RouteModeBlacklist RouteMode = "blacklist"  // GFW blacklist (only proxy blocked)
	RouteModeRoutingA  RouteMode = "routingA"   // custom RoutingA rules
)

// ProxyMode is how traffic enters xray.
type ProxyMode string

const (
	ProxyModeSocks5 ProxyMode = "socks5"
	ProxyModeHTTP   ProxyMode = "http"
	ProxyModeTProxy ProxyMode = "tproxy" // TProxy transparent (TCP+UDP)
	ProxyModeRedir  ProxyMode = "redir"  // REDIRECT transparent (TCP only)
)

// Settings is everything the user can configure.
type Settings struct {
	RouteMode   RouteMode `json:"routeMode"`
	RoutingA    string    `json:"routingA"`    // raw RoutingA text, used when RouteMode==routingA
	ProxyMode   ProxyMode `json:"proxyMode"`
	Socks5Port  int       `json:"socks5Port"`  // default 20170
	HTTPPort    int       `json:"httpPort"`    // default 20171
	TProxyPort  int       `json:"tproxyPort"`  // default 52345
	DNSPort     int       `json:"dnsPort"`     // default 53 (tproxy/redir only)
	DNSUPD      string    `json:"dnsUpstream"` // upstream DNS, default "8.8.8.8"
	DNSLocal    string    `json:"dnsLocal"`    // local DNS, default "114.114.114.114"
	IPv6        bool      `json:"ipv6"`
	Sniffing    bool      `json:"sniffing"`    // domain sniffing on socks/http inbound
}

func DefaultSettings() Settings {
	return Settings{
		RouteMode:  RouteModeWhitelist,
		ProxyMode:  ProxyModeSocks5,
		Socks5Port: 20170,
		HTTPPort:   20171,
		TProxyPort: 52345,
		DNSPort:    15353,
		DNSUPD:     "8.8.8.8",
		DNSLocal:   "114.114.114.114",
		Sniffing:   true,
	}
}

// Build constructs a complete xray config.json as a JSON byte slice.
// gid 是 xraya 系统组的 GID，写入 config 日志字段供调试；
// 防火墙 skgid 规则已在 firewall 包里使用同一 GID，此处无需重复写入。
func Build(n *node.Node, s Settings, gid uint32) ([]byte, error) {
	outbound, err := buildOutbound(n)
	if err != nil {
		return nil, fmt.Errorf("build outbound: %w", err)
	}

	cfg := map[string]interface{}{
		"log":       buildLog(),
		"dns":       buildDNS(s),
		"inbounds":  buildInbounds(s),
		"outbounds": buildOutbounds(outbound, s),
		"routing":   buildRouting(s),
	}

	return json.MarshalIndent(cfg, "", "  ")
}

// ─── Log ─────────────────────────────────────────────────────────────────────

func buildLog() map[string]interface{} {
	return map[string]interface{}{
		"access":   "none",
		"error":    "none",
		"loglevel": "warning",
	}
}

// ─── DNS ─────────────────────────────────────────────────────────────────────

func buildDNS(s Settings) map[string]interface{} {
	servers := []interface{}{
		// Foreign domains → upstream DNS via proxy outbound
		map[string]interface{}{
			"address":      s.DNSUPD,
			"port":         53,
			"domains":      []string{"geosite:geolocation-!cn"},
			"outboundTag":  "proxy",
		},
		// CN domains → local DNS via direct outbound
		map[string]interface{}{
			"address":      s.DNSLocal,
			"port":         53,
			"domains":      []string{"geosite:cn"},
			"outboundTag":  "direct",
		},
		// Fallback → local DNS direct
		map[string]interface{}{
			"address":      s.DNSLocal,
			"port":         53,
			"outboundTag":  "direct",
		},
	}
	return map[string]interface{}{
		"tag":     "dns",
		"servers": servers,
		"hosts": map[string]interface{}{
			"courier.push.apple.com": []string{"1-courier.push.apple.com"},
		},
	}
}

// ─── Inbounds ────────────────────────────────────────────────────────────────

func buildInbounds(s Settings) []interface{} {
	var inbounds []interface{}

	sniff := buildSniffing(s.Sniffing)

	switch s.ProxyMode {
	case ProxyModeSocks5, ProxyModeHTTP:
		// Socks5 inbound
		inbounds = append(inbounds, map[string]interface{}{
			"tag":      "socks-in",
			"listen":   "0.0.0.0",
			"port":     s.Socks5Port,
			"protocol": "socks",
			"settings": map[string]interface{}{
				"auth": "noauth", "udp": true,
			},
			"sniffing": sniff,
		})
		// HTTP inbound
		inbounds = append(inbounds, map[string]interface{}{
			"tag":      "http-in",
			"listen":   "0.0.0.0",
			"port":     s.HTTPPort,
			"protocol": "http",
			"sniffing": sniff,
		})
		// socks5 模式不做 DNS 劫持，不生成 dns-in

	case ProxyModeTProxy:
		// TProxy inbound (TCP+UDP)
		inbounds = append(inbounds,
			map[string]interface{}{
				"tag":      "tproxy-in",
				"listen":   "0.0.0.0",
				"port":     s.TProxyPort,
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{
					"network":        "tcp,udp",
					"followRedirect": true,
				},
				"streamSettings": map[string]interface{}{
					"sockopt": map[string]interface{}{
						"tproxy": "tproxy",
					},
				},
				"sniffing": sniff,
			},
			// DNS 劫持 inbound（高位端口，防火墙重定向 :53 → 此端口）
			dnsInbound(s),
		)

	case ProxyModeRedir:
		// REDIRECT inbound (TCP only)
		inbounds = append(inbounds,
			map[string]interface{}{
				"tag":      "redir-in",
				"listen":   "0.0.0.0",
				"port":     s.TProxyPort,
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{
					"network":        "tcp",
					"followRedirect": true,
				},
				"sniffing": sniff,
			},
			// DNS 劫持 inbound（高位端口，防火墙重定向 :53 → 此端口）
			dnsInbound(s),
		)
	}

	return inbounds
}

func dnsInbound(s Settings) map[string]interface{} {
	// 监听高位端口（默认 15353），防火墙将 :53 流量重定向到此端口。
	// 不直接监听 53，避免与 systemd-resolved 等系统 DNS 服务冲突。
	return map[string]interface{}{
		"tag":      "dns-in",
		"listen":   "0.0.0.0",
		"port":     s.DNSPort,
		"protocol": "dokodemo-door",
		"settings": map[string]interface{}{
			"address": "8.8.8.8",
			"port":    53,
			"network": "tcp,udp",
		},
	}
}

func buildSniffing(enabled bool) map[string]interface{} {
	if !enabled {
		return map[string]interface{}{"enabled": false}
	}
	return map[string]interface{}{
		"enabled":             true,
		"destOverride":        []string{"http", "tls", "quic"},
		"metadataOnly":        false,
		"routeOnly":           false,
	}
}

// ─── Outbounds ───────────────────────────────────────────────────────────────

func buildOutbounds(proxy map[string]interface{}, s Settings) []interface{} {
	proxy["tag"] = "proxy"
	out := []interface{}{
		proxy,
		map[string]interface{}{"tag": "direct", "protocol": "freedom",
			"settings": map[string]interface{}{"domainStrategy": "UseIP"}},
		map[string]interface{}{"tag": "block", "protocol": "blackhole"},
		map[string]interface{}{"tag": "dns-out", "protocol": "dns"},
	}
	return out
}

// ─── Routing ─────────────────────────────────────────────────────────────────

func buildRouting(s Settings) map[string]interface{} {
	var rules []interface{}

	// Determine which inbound tags carry user traffic
	var trafficTags []string
	switch s.ProxyMode {
	case ProxyModeSocks5, ProxyModeHTTP:
		trafficTags = []string{"socks-in", "http-in"}
	case ProxyModeTProxy:
		trafficTags = []string{"tproxy-in"}
	case ProxyModeRedir:
		trafficTags = []string{"redir-in"}
	}

	// DNS routing (tproxy/redir modes)
	if s.ProxyMode == ProxyModeTProxy || s.ProxyMode == ProxyModeRedir {
		rules = append(rules, map[string]interface{}{
			"type":        "field",
			"inboundTag":  []string{"dns-in"},
			"outboundTag": "dns-out",
		})
	}

	// Block ads
	rules = append(rules, map[string]interface{}{
		"type":        "field",
		"domain":      []string{"geosite:category-ads-all"},
		"outboundTag": "block",
		"inboundTag":  trafficTags,
	})

	switch s.RouteMode {
	case RouteModeWhitelist:
		rules = append(rules, whitelistRules(trafficTags)...)
	case RouteModeBlacklist:
		rules = append(rules, blacklistRules(trafficTags)...)
	case RouteModeRoutingA:
		raRules, err := InjectRoutingA(s.RoutingA, trafficTags)
		if err != nil {
			// fallback: proxy everything
			rules = append(rules, map[string]interface{}{
				"type": "field", "outboundTag": "proxy", "inboundTag": trafficTags,
			})
		} else {
			rules = append(rules, raRules...)
		}
	}

	return map[string]interface{}{
		"domainStrategy": "IPIfNonMatch",
		"rules":          rules,
	}
}

func whitelistRules(tags []string) []interface{} {
	return []interface{}{
		// Private / CN IP → direct
		rule(tags, "direct", nil, []string{"geoip:private", "geoip:cn"}),
		// CN domains → direct
		rule(tags, "direct", []string{"geosite:cn"}, nil),
		// Non-CN → proxy (catch-all)
		rule(tags, "proxy", nil, nil),
	}
}

func blacklistRules(tags []string) []interface{} {
	return []interface{}{
		// Private → direct
		rule(tags, "direct", nil, []string{"geoip:private"}),
		// GFW-blocked → proxy
		rule(tags, "proxy", []string{"geosite:geolocation-!cn"}, nil),
		// Everything else → direct
		rule(tags, "direct", nil, nil),
	}
}

func rule(tags []string, out string, domains, ips []string) map[string]interface{} {
	r := map[string]interface{}{
		"type":        "field",
		"outboundTag": out,
		"inboundTag":  tags,
	}
	if len(domains) > 0 {
		r["domain"] = domains
	}
	if len(ips) > 0 {
		r["ip"] = ips
	}
	return r
}

// ─── Per-protocol outbound builders ─────────────────────────────────────────

func buildOutbound(n *node.Node) (map[string]interface{}, error) {
	switch n.Protocol {
	case node.ProtoVMess:
		return vmessOutbound(n)
	case node.ProtoVLESS:
		return vlessOutbound(n)
	case node.ProtoTrojan:
		return trojanOutbound(n)
	case node.ProtoShadowsocks:
		return ssOutbound(n)
	case node.ProtoHysteria2:
		return hy2Outbound(n)
	case node.ProtoSocks5:
		return socks5Outbound(n)
	case node.ProtoHTTP:
		return httpOutbound(n)
	}
	return nil, fmt.Errorf("unsupported protocol: %s", n.Protocol)
}

func vmessOutbound(n *node.Node) (map[string]interface{}, error) {
	var x node.ExtraVMess
	if err := json.Unmarshal(n.Extra, &x); err != nil {
		return nil, err
	}
	stream := buildStream(x.Network, x.TLS, x.SNI, x.Path, x.Host, x.GrpcSvc, x.Fp, false, "")
	return map[string]interface{}{
		"protocol": "vmess",
		"settings": map[string]interface{}{
			"vnext": []interface{}{map[string]interface{}{
				"address": n.Address,
				"port":    n.Port,
				"users": []interface{}{map[string]interface{}{
					"id":       x.UUID,
					"alterId":  x.AlterId,
					"security": x.Security,
					"flow":     x.Flow,
				}},
			}},
		},
		"streamSettings": stream,
	}, nil
}

func vlessOutbound(n *node.Node) (map[string]interface{}, error) {
	var x node.ExtraVLESS
	if err := json.Unmarshal(n.Extra, &x); err != nil {
		return nil, err
	}
	isTLS := x.TLS == "tls" || x.TLS == "reality"
	stream := buildStream(x.Network, isTLS, x.SNI, x.Path, x.Host, x.GrpcSvc, x.Fp, x.TLS == "reality", x.PbKey)
	if x.TLS == "reality" {
		if ss, ok := stream["realitySettings"]; ok {
			if rm, ok := ss.(map[string]interface{}); ok {
				rm["shortId"] = x.ShortID
			}
		}
	}
	return map[string]interface{}{
		"protocol": "vless",
		"settings": map[string]interface{}{
			"vnext": []interface{}{map[string]interface{}{
				"address": n.Address,
				"port":    n.Port,
				"users": []interface{}{map[string]interface{}{
					"id":         x.UUID,
					"flow":       x.Flow,
					"encryption": x.Encryption,
				}},
			}},
		},
		"streamSettings": stream,
	}, nil
}

func trojanOutbound(n *node.Node) (map[string]interface{}, error) {
	var x node.ExtraTrojan
	if err := json.Unmarshal(n.Extra, &x); err != nil {
		return nil, err
	}
	stream := buildStream(x.Network, true, x.SNI, x.Path, x.Host, x.GrpcSvc, "", false, "")
	if x.Insecure {
		if ts, ok := stream["tlsSettings"].(map[string]interface{}); ok {
			ts["allowInsecure"] = true
		}
	}
	return map[string]interface{}{
		"protocol": "trojan",
		"settings": map[string]interface{}{
			"servers": []interface{}{map[string]interface{}{
				"address":  n.Address,
				"port":     n.Port,
				"password": x.Password,
			}},
		},
		"streamSettings": stream,
	}, nil
}

func ssOutbound(n *node.Node) (map[string]interface{}, error) {
	var x node.ExtraShadowsocks
	if err := json.Unmarshal(n.Extra, &x); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"protocol": "shadowsocks",
		"settings": map[string]interface{}{
			"servers": []interface{}{map[string]interface{}{
				"address":  n.Address,
				"port":     n.Port,
				"method":   x.Method,
				"password": x.Password,
			}},
		},
	}, nil
}

func hy2Outbound(n *node.Node) (map[string]interface{}, error) {
	var x node.ExtraHysteria2
	if err := json.Unmarshal(n.Extra, &x); err != nil {
		return nil, err
	}

	// xray-core hysteria2 outbound 正确格式：
	// protocol 为 "hysteria"，settings 直接含 address/port/version/auth，
	// streamSettings 中 network 为 "hysteria"，security 为 "tls"。
	// 参考：https://xtls.github.io/config/outbound-protocols/hysteria2.html
	settings := map[string]interface{}{
		"address": n.Address,
		"port":    n.Port,
		"version": 2,
		"auth":    x.Password,
	}

	tlsSettings := map[string]interface{}{
		"allowInsecure": x.Insecure,
		"fingerprint":   "chrome",
	}
	if x.SNI != "" {
		tlsSettings["serverName"] = x.SNI
	}
	if x.PinSHA256 != "" {
		tlsSettings["pinnedPeerCertificateChainSha256"] = x.PinSHA256
	}

	streamSettings := map[string]interface{}{
		"network":     "hysteria",
		"security":    "tls",
		"tlsSettings": tlsSettings,
	}

	if x.Obfs != "" {
		// salamander 混淆通过 hysteria streamSettings 传递
		streamSettings["hysteriaSettings"] = map[string]interface{}{
			"obfs": x.Obfs,
		}
	}

	return map[string]interface{}{
		"protocol":       "hysteria",
		"settings":       settings,
		"streamSettings": streamSettings,
	}, nil
}
func socks5Outbound(n *node.Node) (map[string]interface{}, error) {
	var x node.ExtraSocks5
	if err := json.Unmarshal(n.Extra, &x); err != nil {
		return nil, err
	}
	srv := map[string]interface{}{
		"address": n.Address, "port": n.Port,
	}
	if x.Username != "" {
		srv["users"] = []interface{}{map[string]interface{}{
			"user": x.Username, "pass": x.Password,
		}}
	}
	return map[string]interface{}{
		"protocol": "socks",
		"settings": map[string]interface{}{"servers": []interface{}{srv}},
	}, nil
}

func httpOutbound(n *node.Node) (map[string]interface{}, error) {
	return map[string]interface{}{
		"protocol": "http",
		"settings": map[string]interface{}{
			"servers": []interface{}{map[string]interface{}{
				"address": n.Address, "port": n.Port,
			}},
		},
	}, nil
}

// ─── Stream settings builder ──────────────────────────────────────────────────

func buildStream(network string, tls bool, sni, path, host, grpcSvc, fp string, isReality bool, pbKey string) map[string]interface{} {
	stream := map[string]interface{}{
		"network": orStr(network, "tcp"),
	}

	// TLS / Reality
	if tls {
		if isReality {
			stream["security"] = "reality"
			stream["realitySettings"] = map[string]interface{}{
				"serverName":  sni,
				"fingerprint": orStr(fp, "chrome"),
				"publicKey":   pbKey,
			}
		} else {
			stream["security"] = "tls"
			stream["tlsSettings"] = map[string]interface{}{
				"serverName":    sni,
				"fingerprint":   orStr(fp, "chrome"),
				"allowInsecure": false,
			}
		}
	}

	// Transport-specific settings
	switch network {
	case "ws":
		ws := map[string]interface{}{"path": orStr(path, "/")}
		if host != "" {
			ws["headers"] = map[string]interface{}{"Host": host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		stream["grpcSettings"] = map[string]interface{}{
			"serviceName": grpcSvc,
			"multiMode":   false,
		}
	case "h2":
		h2 := map[string]interface{}{"path": orStr(path, "/")}
		if host != "" {
			h2["host"] = []string{host}
		}
		stream["httpSettings"] = h2
	case "http":
		h := map[string]interface{}{"path": orStr(path, "/")}
		if host != "" {
			h["host"] = []string{host}
		}
		stream["tcpSettings"] = map[string]interface{}{
			"header": map[string]interface{}{
				"type":    "http",
				"request": h,
			},
		}
	}

	return stream
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
