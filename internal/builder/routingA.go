package builder

import (
	"strings"

	routingA "github.com/v2rayA/RoutingA"
)

// InjectRoutingA parses RoutingA text into xray routing rules.
func InjectRoutingA(raText string, trafficTags []string) ([]interface{}, error) {
	parser := routingA.NewParser(strings.TrimSpace(raText))
	result, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	var rules []interface{}
	for _, r := range result.Rules {
		xrule := map[string]interface{}{
			"type":        "field",
			"outboundTag": mapOutbound(r.Outbound),
			"inboundTag":  trafficTags,
		}
		if len(r.Domain) > 0 {
			xrule["domain"] = r.Domain
		}
		if len(r.IP) > 0 {
			xrule["ip"] = r.IP
		}
		if len(r.Port) > 0 {
			xrule["port"] = strings.Join(r.Port, ",")
		}
		if r.Protocol != "" {
			xrule["protocol"] = []string{r.Protocol}
		}
		rules = append(rules, xrule)
	}
	return rules, nil
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
