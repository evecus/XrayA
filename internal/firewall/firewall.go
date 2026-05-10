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
	nftPath    string // path to write .nft config file
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
	cmds := m.setupCommands()
	return runScript(cmds)
}

// Cleanup removes firewall rules.
func (m *Manager) Cleanup() {
	if m.mode == ModeNone {
		return
	}
	_ = runScript(m.cleanupCommands())
}

// ─── TProxy ──────────────────────────────────────────────────────────────────

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

// ─── nftables setup ──────────────────────────────────────────────────────────

func (m *Manager) nftSetup() string {
	port := m.tproxyPort
	var table string

	if m.mode == ModeRedir {
		table = fmt.Sprintf(`
table inet xraya {
    set bypass4 {
        type ipv4_addr; flags interval; auto-merge
        elements = {
            0.0.0.0/32, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8,
            169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24,
            192.168.0.0/16, 198.51.100.0/24, 203.0.113.0/24,
            224.0.0.0/4, 240.0.0.0/4
        }
    }
    set bypass6 {
        type ipv6_addr; flags interval; auto-merge
        elements = { ::1/128, fe80::/10, ff00::/8 }
    }

    chain tp_rule {
        ip  daddr @bypass4 return
        ip6 daddr @bypass6 return
        meta mark & 0x80 == 0x80 return
        meta l4proto tcp redirect to :%d
    }
    chain tp_pre {
        type nat hook prerouting priority dstnat - 5
        meta nfproto { ipv4, ipv6 } meta l4proto tcp jump tp_rule
    }
    chain tp_out {
        type nat hook output priority -105
        meta nfproto { ipv4, ipv6 } meta l4proto tcp jump tp_rule
    }
}
`, port)
	} else {
		// TProxy
		table = fmt.Sprintf(`
table inet xraya {
    chain tp_out {
        meta mark & 0x80 == 0x80 return
        meta l4proto { tcp, udp } fib saddr type local fib daddr type != local jump tp_rule
    }
    chain tp_pre {
        iifname "lo" mark & 0xc0 != 0x40 return
        meta l4proto { tcp, udp } fib saddr type != local fib daddr type != local jump tp_rule
        meta l4proto { tcp, udp } mark & 0xc0 == 0x40 tproxy ip  to 127.0.0.1:%d
        meta l4proto { tcp, udp } mark & 0xc0 == 0x40 tproxy ip6 to [::1]:%d
    }
    chain output {
        type route hook output priority mangle - 5; policy accept
        meta nfproto { ipv4, ipv6 } jump tp_out
    }
    chain prerouting {
        type filter hook prerouting priority mangle - 5; policy accept
        meta nfproto { ipv4, ipv6 } jump tp_pre
    }
    chain tp_rule {
        meta mark set ct mark
        meta mark & 0xc0 == 0x40 return
        meta l4proto { tcp, udp } th dport 53 jump tp_mark
        meta mark & 0xc0 == 0x40 return
        jump tp_mark
    }
    chain tp_mark {
        tcp flags & (fin|syn|rst|ack) == syn meta mark set mark | 0x40
        meta l4proto udp ct state new meta mark set mark | 0x40
        ct mark set mark
    }
}
`, port, port)
	}

	// Write nft config file and load it
	if err := os.WriteFile(m.nftPath, []byte(table), 0644); err != nil {
		return fmt.Sprintf("# error writing nft config: %v\n", err)
	}
	cmds := fmt.Sprintf("ip rule add fwmark 0x40/0xc0 table 100\nip route add local 0.0.0.0/0 dev lo table 100\nnft -f %s\n", m.nftPath)
	if m.mode == ModeRedir {
		cmds = fmt.Sprintf("nft -f %s\n", m.nftPath)
	}
	return cmds
}

// ─── legacy iptables setups ──────────────────────────────────────────────────

func (m *Manager) iptablesTproxySetup() string {
	p := m.tproxyPort
	return fmt.Sprintf(`
ip rule add fwmark 0x40/0xc0 table 100
ip route add local 0.0.0.0/0 dev lo table 100
iptables -w 2 -t mangle -N TP_MARK
iptables -w 2 -t mangle -N TP_RULE
iptables -w 2 -t mangle -N TP_OUT
iptables -w 2 -t mangle -N TP_PRE
iptables -w 2 -t mangle -I OUTPUT    -j TP_OUT
iptables -w 2 -t mangle -I PREROUTING -j TP_PRE
iptables -w 2 -t mangle -A TP_OUT -m mark --mark 0x80/0x80 -j RETURN
iptables -w 2 -t mangle -A TP_OUT -p tcp -m addrtype --src-type LOCAL ! --dst-type LOCAL -j TP_RULE
iptables -w 2 -t mangle -A TP_OUT -p udp -m addrtype --src-type LOCAL ! --dst-type LOCAL -j TP_RULE
iptables -w 2 -t mangle -A TP_PRE -i lo -m mark ! --mark 0x40/0xc0 -j RETURN
iptables -w 2 -t mangle -A TP_PRE -p tcp -m addrtype ! --src-type LOCAL ! --dst-type LOCAL -j TP_RULE
iptables -w 2 -t mangle -A TP_PRE -p udp -m addrtype ! --src-type LOCAL ! --dst-type LOCAL -j TP_RULE
iptables -w 2 -t mangle -A TP_PRE -p tcp -m mark --mark 0x40/0xc0 -j TPROXY --on-port %d --on-ip 127.0.0.1
iptables -w 2 -t mangle -A TP_PRE -p udp -m mark --mark 0x40/0xc0 -j TPROXY --on-port %d --on-ip 127.0.0.1
iptables -w 2 -t mangle -A TP_RULE -j CONNMARK --restore-mark
iptables -w 2 -t mangle -A TP_RULE -m mark --mark 0x40/0xc0 -j RETURN
iptables -w 2 -t mangle -A TP_RULE -p udp --dport 53 -j TP_MARK
iptables -w 2 -t mangle -A TP_RULE -p tcp --dport 53 -j TP_MARK
iptables -w 2 -t mangle -A TP_RULE -j TP_MARK
iptables -w 2 -t mangle -A TP_MARK -p tcp -m tcp --syn -j MARK --set-xmark 0x40/0x40
iptables -w 2 -t mangle -A TP_MARK -p udp -m conntrack --ctstate NEW -j MARK --set-xmark 0x40/0x40
iptables -w 2 -t mangle -A TP_MARK -j CONNMARK --save-mark
`, p, p)
}

func (m *Manager) iptablesTproxyClean() string {
	return `
ip rule del fwmark 0x40/0xc0 table 100
ip route del local 0.0.0.0/0 dev lo table 100
iptables -w 2 -t mangle -D OUTPUT    -j TP_OUT
iptables -w 2 -t mangle -D PREROUTING -j TP_PRE
iptables -w 2 -t mangle -F TP_OUT; iptables -w 2 -t mangle -X TP_OUT
iptables -w 2 -t mangle -F TP_PRE; iptables -w 2 -t mangle -X TP_PRE
iptables -w 2 -t mangle -F TP_RULE; iptables -w 2 -t mangle -X TP_RULE
iptables -w 2 -t mangle -F TP_MARK; iptables -w 2 -t mangle -X TP_MARK
`
}

func (m *Manager) iptablesRedirSetup() string {
	p := m.tproxyPort
	return fmt.Sprintf(`
iptables -w 2 -t nat -N TP_RULE
iptables -w 2 -t nat -N TP_PRE
iptables -w 2 -t nat -N TP_OUT
iptables -w 2 -t nat -A TP_RULE -d 10.0.0.0/8 -j RETURN
iptables -w 2 -t nat -A TP_RULE -d 127.0.0.0/8 -j RETURN
iptables -w 2 -t nat -A TP_RULE -d 169.254.0.0/16 -j RETURN
iptables -w 2 -t nat -A TP_RULE -d 172.16.0.0/12 -j RETURN
iptables -w 2 -t nat -A TP_RULE -d 192.168.0.0/16 -j RETURN
iptables -w 2 -t nat -A TP_RULE -d 224.0.0.0/4 -j RETURN
iptables -w 2 -t nat -A TP_RULE -d 240.0.0.0/4 -j RETURN
iptables -w 2 -t nat -A TP_RULE -m mark --mark 0x80/0x80 -j RETURN
iptables -w 2 -t nat -A TP_RULE -p tcp -j REDIRECT --to-ports %d
iptables -w 2 -t nat -I PREROUTING -p tcp -j TP_PRE
iptables -w 2 -t nat -I OUTPUT     -p tcp -j TP_OUT
iptables -w 2 -t nat -A TP_PRE -j TP_RULE
iptables -w 2 -t nat -A TP_OUT -j TP_RULE
`, p)
}

func (m *Manager) iptablesRedirClean() string {
	return `
iptables -w 2 -t nat -D PREROUTING -p tcp -j TP_PRE
iptables -w 2 -t nat -D OUTPUT     -p tcp -j TP_OUT
iptables -w 2 -t nat -F TP_PRE;  iptables -w 2 -t nat -X TP_PRE
iptables -w 2 -t nat -F TP_OUT;  iptables -w 2 -t nat -X TP_OUT
iptables -w 2 -t nat -F TP_RULE; iptables -w 2 -t nat -X TP_RULE
`
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func isNftablesSupported() bool {
	err := exec.Command("nft", "--version").Run()
	return err == nil
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
			errs = append(errs, fmt.Sprintf("%s: %v", line, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("firewall errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
