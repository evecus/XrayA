package builder

import (
	"fmt"
	"strings"

	RoutingA "github.com/v2rayA/RoutingA"
)

// InjectRoutingA parses RoutingA text into xray routing rules.
func InjectRoutingA(raText string, trafficTags []string) ([]interface{}, error) {
	lines := strings.Split(strings.TrimSpace(raText), "\n")
	rules, err := RoutingA.Parse(strings.Join(lines, "\n"))
	if err != nil {
		return nil, fmt.Errorf("RoutingA parse: %w", err)
	}

	defaultOutbound := "proxy"
	var xrules []interface{}

	// First pass: pick up "default" define
	for _, rule := range rules {
		if d, ok := rule.(RoutingA.Define); ok && d.Name == "default" {
			if v, ok := d.Value.(string); ok {
				defaultOutbound = mapOutbound(v)
			}
		}
	}

	// Second pass: convert Routing rules
	for _, rule := range rules {
		r, ok := rule.(RoutingA.Routing)
		if !ok {
			continue
		}
		xrule := map[string]interface{}{
			"type":        "field",
			"outboundTag": mapOutbound(r.Out),
			"inboundTag":  trafficTags,
		}
		var domains, ips, protocols []string
		var port, network string
		for _, f := range r.And {
			switch f.Name {
			case "domain", "domains":
				for k, vv := range f.NamedParams {
					for _, v := range vv {
						domains = append(domains, fmt.Sprintf("%s:%s", k, v))
					}
				}
				domains = append(domains, f.Params...)
			case "ip":
				for k, vv := range f.NamedParams {
					for _, v := range vv {
						ips = append(ips, fmt.Sprintf("%s:%s", k, v))
					}
				}
				ips = append(ips, f.Params...)
			case "port":
				port = strings.Join(f.Params, ",")
			case "network":
				network = strings.Join(f.Params, ",")
			case "protocol":
				protocols = f.Params
			}
		}
		if len(domains) > 0 {
			xrule["domain"] = domains
		}
		if len(ips) > 0 {
			xrule["ip"] = ips
		}
		if port != "" {
			xrule["port"] = port
		}
		if network != "" {
			xrule["network"] = network
		}
		if len(protocols) > 0 {
			xrule["protocol"] = protocols
		}
		xrules = append(xrules, xrule)
	}

	// Append catch-all default rule
	xrules = append(xrules, map[string]interface{}{
		"type":        "field",
		"outboundTag": defaultOutbound,
		"inboundTag":  trafficTags,
	})

	return xrules, nil
}

func mapOutbound(o string) string {
	switch strings.ToLower(o) {
	case "proxy", "":
		return "proxy"
	case "direct":
		return "direct"
	case "block", "reject":
		return "block"
	default:
		return o
	}
}
