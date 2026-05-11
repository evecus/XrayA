package firewall

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
)

type Mode string

const (
	ModeTProxy Mode = "tproxy"
	ModeRedir  Mode = "redir"
	ModeNone   Mode = "none"
)

// Manager 持有防火墙配置
type Manager struct {
	mode     Mode
	port     int   // TProxy / Redir 端口
	dnsPort  int   // DNS 劫持监听端口（xray dns-in，默认 15353）
	nftPath  string
	gid      uint32
	ipv6     bool
}

func New(mode Mode, port int, dnsPort int, nftPath string, gid uint32, ipv6 bool) *Manager {
	return &Manager{mode: mode, port: port, dnsPort: dnsPort, nftPath: nftPath, gid: gid, ipv6: ipv6}
}

// Setup 安装 nftables 规则并配置策略路由（仅 tproxy 模式需要）。
func (m *Manager) Setup() error {
	if m.mode == ModeNone {
		return nil
	}
	conf := m.buildTable()
	if err := os.WriteFile(m.nftPath, []byte(conf), 0644); err != nil {
		return fmt.Errorf("write nft conf: %w", err)
	}
	if m.mode == ModeTProxy {
		if err := m.setupTProxyRoutes(); err != nil {
			return err
		}
	}
	if err := runCmd("nft -f " + m.nftPath); err != nil {
		return fmt.Errorf("nft apply: %w", err)
	}
	log.Printf("xraya: firewall: nftables rules applied (mode=%s gid=%d ipv6=%v)", m.mode, m.gid, m.ipv6)
	return nil
}

// Cleanup 删除 nftables 表和策略路由。
func (m *Manager) Cleanup() {
	if m.mode == ModeNone {
		return
	}
	_ = runCmd("nft delete table inet xraya")
	if m.mode == ModeTProxy {
		m.cleanupTProxyRoutes()
	}
	if m.nftPath != "" {
		_ = os.Remove(m.nftPath)
	}
	log.Printf("xraya: firewall: rules removed")
}

// ── nftables table builder ────────────────────────────────────────────────

// privateV4 是所有私有 / 保留 IPv4 段（不代理）。
// 注意不含 192.168.0.0/16，如需代理局域网设备流量可按需删除。
const privateV4 = `{ 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8,
            169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24,
            192.88.99.0/24, 192.168.0.0/16, 198.18.0.0/15,
            198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/3 }`

const privateV6 = `{ ::/127, fc00::/7, fe80::/10, ff00::/8 }`

func (m *Manager) buildTable() string {
	var s strings.Builder
	s.WriteString("table inet xraya {\n")

	// ── bypass 集合 ─────────────────────────────────────────────────────────
	s.WriteString(fmt.Sprintf(`
    set bypass4 {
        type ipv4_addr
        flags interval
        auto-merge
        elements = %s
    }
`, privateV4))

	if m.ipv6 {
		s.WriteString(fmt.Sprintf(`
    set bypass6 {
        type ipv6_addr
        flags interval
        auto-merge
        elements = %s
    }
`, privateV6))
	}

	// ── mark 链（TProxy 模式）─────────────────────────────────────────────
	if m.mode == ModeTProxy {
		s.WriteString(`
    chain tp_mark {
        # 新 TCP SYN 和新 UDP 流打 mark 0x40，并同步到 conntrack
        tcp flags & (fin | syn | rst | ack) == syn meta mark set mark | 0x40
        meta l4proto udp ct state new meta mark set mark | 0x40
        ct mark set mark
    }
`)
	}

	// ── 决策链：判断流量是否需要代理 ─────────────────────────────────────
	s.WriteString(m.buildRuleChain())

	// ── prerouting（接收 tproxy 流量 + 可选局域网代理）────────────────────
	s.WriteString(m.buildPrerouting())

	// ── output（本机出站，skgid 绕过 xray 自身）──────────────────────────
	s.WriteString(m.buildOutput())

	// ── hook 注册 ─────────────────────────────────────────────────────────
	s.WriteString(`
    chain prerouting_hook {
        type filter hook prerouting priority mangle - 5; policy accept;
        jump xraya_pre
    }

    chain output_hook {
        type route hook output priority mangle - 5; policy accept;
        jump xraya_out
    }
`)

	s.WriteString("}\n")
	return s.String()
}

// buildRuleChain 生成决策链 xraya_rule：
// 按优先级依次：已打 mark → 本机地址 → 私有地址 → 打 mark
func (m *Manager) buildRuleChain() string {
	var s strings.Builder
	s.WriteString("\n    chain xraya_rule {\n")

	if m.mode == ModeTProxy {
		// 已处理的连接（conntrack mark 复用）
		s.WriteString("        meta mark set ct mark\n")
		s.WriteString("        meta mark & 0xc0 == 0x40 return\n")
	}

	// 本机地址（lo、任播等）→ 直连
	s.WriteString("        fib daddr type { local, broadcast, anycast, multicast } return\n")

	// 私有 / 保留 IPv4 → 直连
	s.WriteString("        ip daddr @bypass4 return\n")

	if m.ipv6 {
		s.WriteString("        ip6 daddr @bypass6 return\n")
	}

	// 其余流量打 mark → 进入 tproxy / redir
	switch m.mode {
	case ModeTProxy:
		s.WriteString("        meta l4proto { tcp, udp } jump tp_mark\n")
	case ModeRedir:
		// redir 只支持 TCP，redirect target 在 NAT 表处理
		// mangle 这里只负责打 mark 供路由
		s.WriteString("        meta l4proto tcp meta mark set mark | 0x40\n")
	}

	s.WriteString("    }\n")
	return s.String()
}

// buildPrerouting 生成 xraya_pre 链：
// - lo 接口：只处理已打 mark 的包（output 重路由回来的本机流量）
// - 其他接口：调用决策链（局域网代理）
// - DNS 劫持：将发往 :53 的流量重定向到 xray dns-in 高位端口
func (m *Manager) buildPrerouting() string {
	var s strings.Builder
	s.WriteString("\n    chain xraya_pre {\n")

	if m.mode == ModeTProxy {
		// ── DNS 劫持（tproxy 模式）──────────────────────────────────────────
		// 先于通用 tproxy 规则匹配，将所有发往 53 的 TCP/UDP 重定向到 dns-in 端口。
		// 必须同时打 mark 0x40，策略路由（table 100）才能把包路由到 lo 让 tproxy 生效。
		// iifname != "lo" 避免处理 lo 上的回环 DNS（本机 xray 自身发出的已由 output 链豁免）。
		s.WriteString(fmt.Sprintf(
			"        iifname != \"lo\" meta l4proto { tcp, udp } th dport 53 meta mark set meta mark | 0x00000040 tproxy ip  to 127.0.0.1:%d\n",
			m.dnsPort))
		if m.ipv6 {
			s.WriteString(fmt.Sprintf(
				"        iifname != \"lo\" meta l4proto { tcp, udp } th dport 53 meta mark set meta mark | 0x00000040 tproxy ip6 to [::1]:%d\n",
				m.dnsPort))
		}

		// lo 上没打 mark 的包直接放行（避免处理非 tproxy 流量）
		s.WriteString("        iifname \"lo\" meta mark & 0xc0 != 0x40 return\n")
		// 已打 mark 的包转给 tproxy
		s.WriteString(fmt.Sprintf(
			"        meta nfproto ipv4 meta l4proto { tcp, udp } meta mark & 0xc0 == 0x40 tproxy ip  to 127.0.0.1:%d\n",
			m.port))
		if m.ipv6 {
			s.WriteString(fmt.Sprintf(
				"        meta nfproto ipv6 meta l4proto { tcp, udp } meta mark & 0xc0 == 0x40 tproxy ip6 to [::1]:%d\n",
				m.port))
		}
		// 局域网转发流量：非 lo 接口调用决策链
		s.WriteString("        iifname != \"lo\" meta l4proto { tcp, udp } jump xraya_rule\n")
		// 再次 tproxy（决策链打完 mark 后）
		s.WriteString(fmt.Sprintf(
			"        iifname != \"lo\" meta nfproto ipv4 meta l4proto { tcp, udp } meta mark & 0xc0 == 0x40 tproxy ip  to 127.0.0.1:%d\n",
			m.port))
		if m.ipv6 {
			s.WriteString(fmt.Sprintf(
				"        iifname != \"lo\" meta nfproto ipv6 meta l4proto { tcp, udp } meta mark & 0xc0 == 0x40 tproxy ip6 to [::1]:%d\n",
				m.port))
		}
	}

	if m.mode == ModeRedir {
		// ── DNS 劫持（redir 模式）───────────────────────────────────────────
		// redir 不支持 tproxy，用 DNAT 把 :53 重定向到 dns-in 高位端口。
		// xray dokodemo-door 的 followRedirect=true 对 DNAT UDP 可以正确还原目标。
		s.WriteString(fmt.Sprintf(
			"        iifname != \"lo\" meta l4proto { tcp, udp } th dport 53 dnat ip  to 127.0.0.1:%d\n",
			m.dnsPort))
		if m.ipv6 {
			s.WriteString(fmt.Sprintf(
				"        iifname != \"lo\" meta l4proto { tcp, udp } th dport 53 dnat ip6 to [::1]:%d\n",
				m.dnsPort))
		}
	}

	s.WriteString("    }\n")
	return s.String()
}

// buildOutput 生成 xraya_out 链：
// - skgid <gid> → return：xray 进程（xraya group）的流量直接放行，避免循环
// - 其余本机出站 → 决策链
func (m *Manager) buildOutput() string {
	var s strings.Builder
	s.WriteString("\n    chain xraya_out {\n")

	// ★ 核心：xray 自身流量（以 xraya GID 运行）跳过代理，避免 CPU 爆满/流量循环
	s.WriteString(fmt.Sprintf("        skgid %d return\n", m.gid))

	nfproto := "meta nfproto ipv4"
	if m.ipv6 {
		nfproto = "meta nfproto { ipv4, ipv6 }"
	}
	// 本机出站（saddr 是本地地址，daddr 不是本地地址）→ 决策链
	s.WriteString(fmt.Sprintf(
		"        %s meta l4proto { tcp, udp } fib saddr type local fib daddr type != local jump xraya_rule\n",
		nfproto))

	s.WriteString("    }\n")
	return s.String()
}

// ── 策略路由（TProxy 需要）────────────────────────────────────────────────

func (m *Manager) setupTProxyRoutes() error {
	cmds := []string{
		"ip rule add fwmark 0x40/0xc0 table 100",
		"ip route replace local 0.0.0.0/0 dev lo table 100",
	}
	if m.ipv6 {
		cmds = append(cmds,
			"ip -6 rule add fwmark 0x40/0xc0 table 100",
			"ip -6 route replace local ::/0 dev lo table 100",
		)
	}
	for _, c := range cmds {
		if err := runCmd(c); err != nil {
			// "ip rule add" 在规则已存在时返回 exit 2（File exists），无害
			if isIPRuleExists(err) {
				continue
			}
			log.Printf("xraya: firewall: route: %v", err)
		}
	}
	return nil
}

func (m *Manager) cleanupTProxyRoutes() {
	cmds := []string{
		"ip rule del fwmark 0x40/0xc0 table 100",
		"ip route del local 0.0.0.0/0 dev lo table 100",
	}
	if m.ipv6 {
		cmds = append(cmds,
			"ip -6 rule del fwmark 0x40/0xc0 table 100",
			"ip -6 route del local ::/0 dev lo table 100",
		)
	}
	for _, c := range cmds {
		_ = runCmd(c)
	}
}

func isIPRuleExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "File exists")
}

// ── 本机接口 IP 管理（供 API 调用，动态更新 bypass 集合）────────────────

// AddLocalIP 将本机接口 IP 加入 bypass 集合，使发往本机的流量不被代理。
func AddLocalIP(cidr string) {
	set := "bypass4"
	ip, _, _ := net.ParseCIDR(cidr)
	if ip == nil {
		ip = net.ParseIP(cidr)
	}
	if ip != nil && ip.To4() == nil {
		set = "bypass6"
	}
	if err := runCmd(fmt.Sprintf("nft add element inet xraya %s { %s }", set, cidr)); err != nil {
		log.Printf("xraya: firewall: add local IP %s: %v", cidr, err)
	}
}

// RemoveLocalIP 从 bypass 集合中移除 IP。
func RemoveLocalIP(cidr string) {
	set := "bypass4"
	ip, _, _ := net.ParseCIDR(cidr)
	if ip == nil {
		ip = net.ParseIP(cidr)
	}
	if ip != nil && ip.To4() == nil {
		set = "bypass6"
	}
	if err := runCmd(fmt.Sprintf("nft delete element inet xraya %s { %s }", set, cidr)); err != nil {
		log.Printf("xraya: firewall: remove local IP %s: %v", cidr, err)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func runCmd(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil
	}
	out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (output: %s)", command, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsIPv6Supported 检查本机是否有 IPv6 lo 地址。
func IsIPv6Supported() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ipnet.IP.IsLoopback() && ipnet.IP.To4() == nil {
					return true
				}
			}
		}
	}
	return false
}
