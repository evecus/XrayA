package firewall

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Mode is the transparent proxy firewall mode.
type Mode string

const (
	ModeTProxy Mode = "tproxy"
	ModeRedir  Mode = "redir"
	ModeNone   Mode = "none"
)

// Manager manages nft/iptables rules for transparent proxy.
type Manager struct {
	mode       Mode
	tproxyPort int
	nftPath    string
	useNft     bool
}

func New(mode Mode, tproxyPort int, nftPath string) *Manager {
	return &Manager{
		mode:       mode,
		tproxyPort: tproxyPort,
		nftPath:    nftPath,
		useNft:     isNftablesSupported(),
	}
}

// Setup installs firewall rules.
func (m *Manager) Setup() error {
	if m.mode == ModeNone {
		return nil
	}
	return runScript(m.setupCommands())
}

// Cleanup removes firewall rules.
func (m *Manager) Cleanup() {
	if m.mode == ModeNone {
		return
	}
	_ = runScript(m.cleanupCommands())
}

func (m *Manager) setupCommands() string {
	if m.useNft {
		return m.nftSetup()
	}
	if m.mode == ModeRedir {
		return m.iptablesRedirSetup()
	}
	return m.iptablesTproxySetup()
}

func (m *Manager) cleanupCommands() string {
	if m.useNft {
		return "nft delete table inet xraya\n"
	}
	if m.mode == ModeRedir {
		return m.iptablesRedirClean()
	}
	return m.iptablesTproxyClean()
}

// ─── nftables ────────────────────────────────────────────────────────────────

// bypass4 / bypass6: 不代理的目标地址（保留地址，但保留 192.168.0.0/16 用于局域网代理）
// 注意：192.168.0.0/16 不在 bypass 里，这样局域网设备流量也会被代理
const bypassSets = `
    set bypass4 {
        type ipv4_addr; flags interval; auto-merge
        elements = {
            0.0.0.0/8,
            10.0.0.0/8,
            100.64.0.0/10,
            127.0.0.0/8,
            169.254.0.0/16,
            172.16.0.0/12,
            192.0.0.0/24,
            192.0.2.0/24,
            198.51.100.0/24,
            203.0.113.0/24,
            224.0.0.0/4,
            240.0.0.0/4,
            255.255.255.255/32
        }
    }
    set bypass6 {
        type ipv6_addr; flags interval; auto-merge
        elements = { ::1/128, fc00::/7, fe80::/10, ff00::/8 }
    }
`

func (m *Manager) nftSetup() string {
	port := m.tproxyPort
	var table string

	if m.mode == ModeRedir {
		// ── REDIRECT 模式 ────────────────────────────────────────────────
		// 代理本机出站 TCP + 局域网转发 TCP
		// UDP 不支持（REDIRECT 是 NAT，UDP 需要 TProxy）
		table = fmt.Sprintf(`
table inet xraya {
%s
    # 绕过规则链：目标在 bypass 集合里则跳过代理
    chain tp_rule {
        ip  daddr @bypass4 return
        ip6 daddr @bypass6 return
        meta mark & 0x00000080 == 0x00000080 return
        meta l4proto tcp redirect to :%d
    }

    # 处理本机发出的 TCP 流量
    chain output {
        type nat hook output priority dstnat - 5; policy accept;
        meta nfproto { ipv4, ipv6 } meta l4proto tcp jump tp_rule
    }

    # 处理局域网转发进来的 TCP 流量（其他设备经过本机出口）
    chain prerouting {
        type nat hook prerouting priority dstnat - 5; policy accept;
        meta nfproto { ipv4, ipv6 } meta l4proto tcp jump tp_rule
    }
}
`, bypassSets, port)

	} else {
		// ── TPROXY 模式 ──────────────────────────────────────────────────
		// 代理本机出站 TCP+UDP + 局域网转发 TCP+UDP
		//
		// 流程：
		//   output  → tp_out → tp_rule → tp_mark（打 0x40 mark）
		//   prerouting → tp_pre：
		//     1. lo 接口：只处理已打 0x40 mark 的包（本机重路由回来的）→ tproxy
		//     2. 其他接口（局域网入流量）→ tp_rule → tp_mark → tproxy
		table = fmt.Sprintf(`
table inet xraya {
%s
    # ── 打 mark 链 ───────────────────────────────────────────────────────
    chain tp_mark {
        # 新 TCP 连接 syn 包打 mark
        tcp flags & (fin | syn | rst | ack) == syn meta mark set meta mark | 0x00000040
        # 新 UDP 流打 mark
        meta l4proto udp ct state new meta mark set meta mark | 0x00000040
        # 把 mark 同步到 conntrack，让后续包复用
        ct mark set meta mark
    }

    # ── 决策链：是否需要代理 ─────────────────────────────────────────────
    chain tp_rule {
        # 已有 conntrack mark 则复用（加速已知连接）
        meta mark set ct mark
        # 已打过 0x40 的直接跳过（已处理）
        meta mark & 0x000000c0 == 0x00000040 return
        # bypass 目标地址
        ip  daddr @bypass4 return
        ip6 daddr @bypass6 return
        # xray 自身流量（mark 0x80）跳过，避免死循环
        meta mark & 0x00000080 == 0x00000080 return
        # 其余流量打 mark
        jump tp_mark
    }

    # ── 本机出站处理（output hook）───────────────────────────────────────
    chain tp_out {
        # xray 自身流量跳过
        meta mark & 0x00000080 == 0x00000080 return
        # 本机发出的流量走决策链
        meta l4proto { tcp, udp } jump tp_rule
    }
    chain output {
        type route hook output priority mangle - 5; policy accept;
        meta nfproto { ipv4, ipv6 } jump tp_out
    }

    # ── prerouting：tproxy 接收 + 局域网代理 ────────────────────────────
    chain tp_pre {
        # lo 接口：只处理已打 mark 的包（本机 output 重路由回来的），转给 tproxy
        iifname "lo" meta mark & 0x000000c0 != 0x00000040 return
        iifname "lo" meta l4proto { tcp, udp } tproxy ip  to 127.0.0.1:%d
        iifname "lo" meta l4proto { tcp, udp } tproxy ip6 to [::1]:%d

        # 非 lo 接口（局域网转发流量）：先走决策链打 mark，再 tproxy
        iifname != "lo" meta l4proto { tcp, udp } jump tp_rule
        iifname != "lo" meta l4proto { tcp, udp } meta mark & 0x000000c0 == 0x00000040 tproxy ip  to 127.0.0.1:%d
        iifname != "lo" meta l4proto { tcp, udp } meta mark & 0x000000c0 == 0x00000040 tproxy ip6 to [::1]:%d
    }
    chain prerouting {
        type filter hook prerouting priority mangle - 5; policy accept;
        meta nfproto { ipv4, ipv6 } jump tp_pre
    }
}
`, bypassSets, port, port, port, port)
	}

	if err := os.WriteFile(m.nftPath, []byte(table), 0644); err != nil {
		return fmt.Sprintf("# error writing nft config: %v\n", err)
	}

	var cmds string
	if m.mode == ModeTProxy {
		// TProxy 需要策略路由：mark 0x40 的包走 table 100 → lo
		// 使用 "replace" 而非 "add"，保证幂等（重启 xraya 不会因规则已存在而报错）
		cmds = fmt.Sprintf(
			"ip rule add fwmark 0x40/0xc0 table 100\n"+
				"ip route replace local 0.0.0.0/0 dev lo table 100\n"+
				"ip -6 rule add fwmark 0x40/0xc0 table 100\n"+
				"ip -6 route replace local ::/0 dev lo table 100\n"+
				"sysctl -w net.ipv4.ip_forward=1\n"+
				"sysctl -w net.ipv6.conf.all.forwarding=1\n"+
				"nft -f %s\n", m.nftPath)
	} else {
		// Redir 只需要开启转发
		cmds = fmt.Sprintf(
			"sysctl -w net.ipv4.ip_forward=1\n"+
				"sysctl -w net.ipv6.conf.all.forwarding=1\n"+
				"nft -f %s\n", m.nftPath)
	}
	return cmds
}

// ─── legacy iptables TProxy ──────────────────────────────────────────────────

func (m *Manager) iptablesTproxySetup() string {
	p := m.tproxyPort
	return fmt.Sprintf(`
sysctl -w net.ipv4.ip_forward=1
ip rule add fwmark 0x40/0xc0 table 100
ip route replace local 0.0.0.0/0 dev lo table 100
iptables -w 2 -t mangle -N XRAYA_MARK
iptables -w 2 -t mangle -N XRAYA_RULE
iptables -w 2 -t mangle -N XRAYA_OUT
iptables -w 2 -t mangle -N XRAYA_PRE
iptables -w 2 -t mangle -I OUTPUT     -j XRAYA_OUT
iptables -w 2 -t mangle -I PREROUTING -j XRAYA_PRE
iptables -w 2 -t mangle -A XRAYA_MARK -p tcp -m tcp --syn -j MARK --set-xmark 0x40/0x40
iptables -w 2 -t mangle -A XRAYA_MARK -p udp -m conntrack --ctstate NEW -j MARK --set-xmark 0x40/0x40
iptables -w 2 -t mangle -A XRAYA_MARK -j CONNMARK --save-mark
iptables -w 2 -t mangle -A XRAYA_RULE -j CONNMARK --restore-mark
iptables -w 2 -t mangle -A XRAYA_RULE -m mark --mark 0x40/0xc0 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -m mark --mark 0x80/0x80 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 127.0.0.0/8 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 10.0.0.0/8 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 100.64.0.0/10 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 169.254.0.0/16 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 172.16.0.0/12 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 224.0.0.0/4 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -d 240.0.0.0/4 -j RETURN
iptables -w 2 -t mangle -A XRAYA_RULE -j XRAYA_MARK
iptables -w 2 -t mangle -A XRAYA_OUT -m mark --mark 0x80/0x80 -j RETURN
iptables -w 2 -t mangle -A XRAYA_OUT -p tcp -j XRAYA_RULE
iptables -w 2 -t mangle -A XRAYA_OUT -p udp -j XRAYA_RULE
iptables -w 2 -t mangle -A XRAYA_PRE -i lo -m mark ! --mark 0x40/0xc0 -j RETURN
iptables -w 2 -t mangle -A XRAYA_PRE -p tcp -j TPROXY --on-port %d --on-ip 127.0.0.1 --tproxy-mark 0x40/0x40
iptables -w 2 -t mangle -A XRAYA_PRE -p udp -j TPROXY --on-port %d --on-ip 127.0.0.1 --tproxy-mark 0x40/0x40
iptables -w 2 -t mangle -I XRAYA_PRE -i lo -m mark --mark 0x40/0xc0 -p tcp -j TPROXY --on-port %d --on-ip 127.0.0.1
iptables -w 2 -t mangle -I XRAYA_PRE -i lo -m mark --mark 0x40/0xc0 -p udp -j TPROXY --on-port %d --on-ip 127.0.0.1
`, p, p, p, p)
}

func (m *Manager) iptablesTproxyClean() string {
	return `
ip rule del fwmark 0x40/0xc0 table 100
ip route del local 0.0.0.0/0 dev lo table 100
iptables -w 2 -t mangle -D OUTPUT     -j XRAYA_OUT
iptables -w 2 -t mangle -D PREROUTING -j XRAYA_PRE
iptables -w 2 -t mangle -F XRAYA_OUT;  iptables -w 2 -t mangle -X XRAYA_OUT
iptables -w 2 -t mangle -F XRAYA_PRE;  iptables -w 2 -t mangle -X XRAYA_PRE
iptables -w 2 -t mangle -F XRAYA_RULE; iptables -w 2 -t mangle -X XRAYA_RULE
iptables -w 2 -t mangle -F XRAYA_MARK; iptables -w 2 -t mangle -X XRAYA_MARK
`
}

// ─── legacy iptables Redir ───────────────────────────────────────────────────

func (m *Manager) iptablesRedirSetup() string {
	p := m.tproxyPort
	return fmt.Sprintf(`
sysctl -w net.ipv4.ip_forward=1
iptables -w 2 -t nat -N XRAYA_RULE
iptables -w 2 -t nat -N XRAYA_OUT
iptables -w 2 -t nat -N XRAYA_PRE
iptables -w 2 -t nat -A XRAYA_RULE -d 0.0.0.0/8 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 10.0.0.0/8 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 100.64.0.0/10 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 127.0.0.0/8 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 169.254.0.0/16 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 172.16.0.0/12 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 224.0.0.0/4 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -d 240.0.0.0/4 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -m mark --mark 0x80/0x80 -j RETURN
iptables -w 2 -t nat -A XRAYA_RULE -p tcp -j REDIRECT --to-ports %d
iptables -w 2 -t nat -A XRAYA_OUT -p tcp -j XRAYA_RULE
iptables -w 2 -t nat -A XRAYA_PRE -p tcp -j XRAYA_RULE
iptables -w 2 -t nat -I OUTPUT     -j XRAYA_OUT
iptables -w 2 -t nat -I PREROUTING -j XRAYA_PRE
`, p)
}

func (m *Manager) iptablesRedirClean() string {
	return `
iptables -w 2 -t nat -D OUTPUT     -j XRAYA_OUT
iptables -w 2 -t nat -D PREROUTING -j XRAYA_PRE
iptables -w 2 -t nat -F XRAYA_OUT;  iptables -w 2 -t nat -X XRAYA_OUT
iptables -w 2 -t nat -F XRAYA_PRE;  iptables -w 2 -t nat -X XRAYA_PRE
iptables -w 2 -t nat -F XRAYA_RULE; iptables -w 2 -t nat -X XRAYA_RULE
`
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func isNftablesSupported() bool {
	return exec.Command("nft", "--version").Run() == nil
}

func runScript(script string) error {
	var errs []string
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if err := exec.Command(parts[0], parts[1:]...).Run(); err != nil {
			// "ip rule add" returns exit 2 when the rule already exists
			// (RTNETLINK: File exists). This is harmless on re-start.
			if parts[0] == "ip" && len(parts) >= 3 && parts[1] == "rule" && parts[2] == "add" {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: %v", line, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("firewall errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
