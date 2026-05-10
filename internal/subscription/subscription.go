package subscription

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xraya/xraya/internal/node"
)

// Group represents a subscription source.
type Group struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	URL     string    `json:"url"`
	Updated time.Time `json:"updated"`
}

// Fetch downloads a subscription URL and returns all parsed nodes.
func Fetch(sub Group) ([]*node.Node, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", sub.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", sub.URL, err)
	}
	req.Header.Set("User-Agent", "xraya/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", sub.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return ParseContent(string(body), sub.ID)
}

// ParseContent parses raw subscription content (base64 or plain links).
func ParseContent(content, groupID string) ([]*node.Node, error) {
	content = strings.TrimSpace(content)

	// Try base64 decode
	if !looksLikeLinks(content) {
		if dec, err := tryBase64(content); err == nil {
			content = dec
		}
	}

	var nodes []*node.Node
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n, err := node.ParseLink(line)
		if err != nil {
			continue // skip unknown protocols silently
		}
		n.GroupID = groupID
		nodes = append(nodes, n)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no valid nodes found")
	}
	return nodes, nil
}

func looksLikeLinks(s string) bool {
	for _, pfx := range []string{
		"vmess://", "vless://", "trojan://", "ss://",
		"hysteria2://", "hy2://", "socks5://",
	} {
		if strings.Contains(s, pfx) {
			return true
		}
	}
	return false
}

func tryBase64(s string) (string, error) {
	s = strings.TrimRight(s, "=")
	for _, enc := range []interface {
		DecodeString(string) ([]byte, error)
	}{
		base64.URLEncoding.WithPadding(base64.NoPadding),
		base64.StdEncoding.WithPadding(base64.NoPadding),
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("not base64")
}
