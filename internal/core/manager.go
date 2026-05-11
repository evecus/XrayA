package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/xraya/xraya/internal/auth"
	"github.com/xraya/xraya/internal/builder"
	"github.com/xraya/xraya/internal/firewall"
	"github.com/xraya/xraya/internal/node"
	"github.com/xraya/xraya/internal/storage"
	"github.com/xraya/xraya/internal/subscription"
)

const xrayaGroup = "xraya"

type Status string

const (
	StatusStopped Status = "stopped"
	StatusRunning Status = "running"
	StatusError   Status = "error"
)

type state struct {
	ActiveNodeID string `json:"activeNodeId"`
	Running      bool   `json:"running"`
}

type Manager struct {
	dataDir string
	db      *storage.DB
	Auth    *auth.Manager

	mu       sync.Mutex
	status   Status
	errMsg   string
	proc     *os.Process
	cancel   context.CancelFunc
	logLines []string

	settings builder.Settings
	st       state
}

func NewManager(dataDir string) (*Manager, error) {
	dbPath := filepath.Join(dataDir, "xraya.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		return nil, err
	}
	authMgr, err := auth.New(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	m := &Manager{
		dataDir:  dataDir,
		db:       db,
		Auth:     authMgr,
		status:   StatusStopped,
		settings: builder.DefaultSettings(),
	}
	_ = db.LoadSetting("settings", &m.settings)
	_ = db.LoadSetting("state", &m.st)
	return m, nil
}

// ── Group helpers ──────────────────────────────────────────────────────────
// 与 Metaviz 相同的策略：确保 "xraya" system group 存在，
// 将 xray 进程以该 GID 运行，nftables 用 skgid 识别并跳过其流量。

func ensureXrayaGroup() (uint32, error) {
	if g, err := user.LookupGroup(xrayaGroup); err == nil {
		gid, err := strconv.ParseUint(g.Gid, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("parse gid %q: %w", g.Gid, err)
		}
		return uint32(gid), nil
	}
	log.Printf("xraya: group %q not found, creating", xrayaGroup)
	if path, err := exec.LookPath("groupadd"); err == nil {
		out, err := exec.Command(path, "--system", xrayaGroup).CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("groupadd: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}
		g, err := user.LookupGroup(xrayaGroup)
		if err != nil {
			return 0, fmt.Errorf("lookup group after create: %w", err)
		}
		gid, err := strconv.ParseUint(g.Gid, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("parse gid: %w", err)
		}
		return uint32(gid), nil
	}
	return writeGroupEntry(xrayaGroup)
}

func writeGroupEntry(name string) (uint32, error) {
	const groupFile = "/etc/group"
	data, err := os.ReadFile(groupFile)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", groupFile, err)
	}
	usedGIDs := make(map[uint32]bool)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 3 {
			continue
		}
		if gid, err := strconv.ParseUint(parts[2], 10, 32); err == nil {
			usedGIDs[uint32(gid)] = true
		}
	}
	var chosen uint32
	for candidate := uint32(500); candidate < 65000; candidate++ {
		if !usedGIDs[candidate] {
			chosen = candidate
			break
		}
	}
	if chosen == 0 {
		return 0, fmt.Errorf("no free GID available")
	}
	entry := fmt.Sprintf("%s:x:%d:\n", name, chosen)
	f, err := os.OpenFile(groupFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", groupFile, err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return 0, fmt.Errorf("write %s: %w", groupFile, err)
	}
	return chosen, nil
}

// ── Node CRUD ──────────────────────────────────────────────────────────────

func (m *Manager) AddNode(n *node.Node) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return m.db.UpsertEntity("node", n.ID, n)
}

func (m *Manager) DeleteNode(id string) error {
	m.mu.Lock()
	isActive := m.st.ActiveNodeID == id
	m.mu.Unlock()
	if isActive {
		m.Stop()
	}
	return m.db.DeleteEntity("node", id)
}

func (m *Manager) GetNode(id string) (*node.Node, error) {
	e, err := m.db.GetEntity("node", id)
	if err != nil || e == nil {
		return nil, fmt.Errorf("node %s not found", id)
	}
	var n node.Node
	if err := json.Unmarshal([]byte(e.Data), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (m *Manager) ListNodes() ([]*node.Node, error) {
	entities, err := m.db.ListEntities("node")
	if err != nil {
		return nil, err
	}
	nodes := make([]*node.Node, 0, len(entities))
	for _, e := range entities {
		var n node.Node
		if err := json.Unmarshal([]byte(e.Data), &n); err == nil {
			nodes = append(nodes, &n)
		}
	}
	return nodes, nil
}

// ── Subscription CRUD ──────────────────────────────────────────────────────

func (m *Manager) AddSubscription(g *subscription.Group) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return m.db.UpsertEntity("sub", g.ID, g)
}

func (m *Manager) DeleteSubscription(id string) error {
	nodes, _ := m.ListNodes()
	for _, n := range nodes {
		if n.GroupID == id {
			_ = m.db.DeleteEntity("node", n.ID)
		}
	}
	return m.db.DeleteEntity("sub", id)
}

func (m *Manager) ListSubscriptions() ([]*subscription.Group, error) {
	entities, err := m.db.ListEntities("sub")
	if err != nil {
		return nil, err
	}
	subs := make([]*subscription.Group, 0, len(entities))
	for _, e := range entities {
		var g subscription.Group
		if err := json.Unmarshal([]byte(e.Data), &g); err == nil {
			subs = append(subs, &g)
		}
	}
	return subs, nil
}

func (m *Manager) UpdateSubscription(id string) error {
	e, err := m.db.GetEntity("sub", id)
	if err != nil || e == nil {
		return fmt.Errorf("subscription %s not found", id)
	}
	var g subscription.Group
	if err := json.Unmarshal([]byte(e.Data), &g); err != nil {
		return err
	}
	nodes, err := subscription.Fetch(g)
	if err != nil {
		return err
	}
	allNodes, _ := m.ListNodes()
	for _, n := range allNodes {
		if n.GroupID == id {
			_ = m.db.DeleteEntity("node", n.ID)
		}
	}
	g.Updated = time.Now()
	_ = m.db.UpsertEntity("sub", id, &g)
	for _, n := range nodes {
		n.ID = uuid.New().String()
		n.GroupID = id
		_ = m.db.UpsertEntity("node", n.ID, n)
	}
	return nil
}

// ── Settings ───────────────────────────────────────────────────────────────

func (m *Manager) GetSettings() builder.Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

func (m *Manager) SetSettings(s builder.Settings) error {
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return m.db.SaveSetting("settings", &s)
}

// ── Xray process ───────────────────────────────────────────────────────────

func (m *Manager) Start(nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status == StatusRunning {
		m.stopLocked()
	}

	n, err := m.getNodeLocked(nodeID)
	if err != nil {
		return err
	}

	// 确保 xraya group 存在，并取得 GID
	gid, err := ensureXrayaGroup()
	if err != nil {
		return fmt.Errorf("xraya group: %w", err)
	}

	cfg, err := m.buildConfig(n, gid)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	cfgPath := filepath.Join(m.dataDir, "run", "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// 先设置防火墙（nftables），其中 skgid 规则已指向 xraya 组
	mode := firewallMode(m.settings.ProxyMode)
	if mode != firewall.ModeNone {
		if err := firewall.New(mode, m.settings.TProxyPort, m.settings.DNSPort,
			filepath.Join(m.dataDir, "xraya.nft"), gid, m.settings.IPv6).Setup(); err != nil {
			return fmt.Errorf("firewall: %w", err)
		}
	}

	xrayBin := m.findXray()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, xrayBin, "run", "-c", cfgPath)
	cmd.Env = append(os.Environ(),
		"XRAY_LOCATION_ASSET="+m.dataDir,
		"V2RAY_LOCATION_ASSET="+m.dataDir,
	)
	// xray 进程以 xraya GID 运行，nftables skgid 识别并放行其出站流量
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:         0,
			Gid:         gid,
			Groups:      []uint32{gid},
			NoSetGroups: false,
		},
	}

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		cancel()
		if mode != firewall.ModeNone {
			firewall.New(mode, m.settings.TProxyPort, m.settings.DNSPort,
				filepath.Join(m.dataDir, "xraya.nft"), gid, m.settings.IPv6).Cleanup()
		}
		m.status = StatusError
		m.errMsg = err.Error()
		return fmt.Errorf("start xray: %w", err)
	}

	m.proc = cmd.Process
	m.cancel = cancel
	m.status = StatusRunning
	m.errMsg = ""
	m.st.ActiveNodeID = nodeID
	m.st.Running = true
	_ = m.db.SaveSetting("state", &m.st)

	go m.captureLog(stdoutPipe)
	go m.captureLog(stderrPipe)
	go func() {
		cmd.Wait()
		time.Sleep(200 * time.Millisecond)
		m.mu.Lock()
		if m.status == StatusRunning {
			m.status = StatusError
			errDetail := "xray exited unexpectedly"
			if len(m.logLines) > 0 {
				var errLines []string
				for i := len(m.logLines) - 1; i >= 0 && len(errLines) < 3; i-- {
					if l := strings.TrimSpace(m.logLines[i]); l != "" {
						errLines = append([]string{l}, errLines...)
					}
				}
				if len(errLines) > 0 {
					errDetail = strings.Join(errLines, " | ")
				}
			}
			m.errMsg = errDetail
			m.st.Running = false
			_ = m.db.SaveSetting("state", &m.st)
			// xray 异常退出时也清理防火墙规则
			if mode != firewall.ModeNone {
				firewall.New(mode, m.settings.TProxyPort, m.settings.DNSPort,
					filepath.Join(m.dataDir, "xraya.nft"), gid, m.settings.IPv6).Cleanup()
			}
		}
		m.mu.Unlock()
	}()

	log.Printf("xraya: started [%s] node=%s gid=%d", xrayBin, n.Name, gid)
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	s := m.settings
	mode := firewallMode(s.ProxyMode)
	if mode != firewall.ModeNone {
		// GID 在 stop 时读取当前系统 group，找不到就传 0（cleanup 仍能删表）
		gid := uint32(0)
		if g, err := user.LookupGroup(xrayaGroup); err == nil {
			if v, err := strconv.ParseUint(g.Gid, 10, 32); err == nil {
				gid = uint32(v)
			}
		}
		firewall.New(mode, s.TProxyPort, s.DNSPort,
			filepath.Join(m.dataDir, "xraya.nft"), gid, s.IPv6).Cleanup()
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.proc != nil {
		_ = m.proc.Signal(os.Interrupt)
		m.proc = nil
	}
	m.status = StatusStopped
	m.st.Running = false
	_ = m.db.SaveSetting("state", &m.st)
}

func (m *Manager) AutoStart() {
	m.mu.Lock()
	nodeID := m.st.ActiveNodeID
	wasRunning := m.st.Running
	m.mu.Unlock()
	if wasRunning && nodeID != "" {
		if err := m.Start(nodeID); err != nil {
			log.Printf("xraya: autostart failed: %v", err)
		}
	}
}

func (m *Manager) Status() (Status, string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status, m.errMsg, m.st.ActiveNodeID
}

// ── Logs ───────────────────────────────────────────────────────────────────

func (m *Manager) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.logLines))
	copy(cp, m.logLines)
	return cp
}

func (m *Manager) captureLog(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		m.mu.Lock()
		m.logLines = append(m.logLines, line)
		if len(m.logLines) > 500 {
			m.logLines = m.logLines[len(m.logLines)-500:]
		}
		m.mu.Unlock()
	}
}

// ── Internal ───────────────────────────────────────────────────────────────

func (m *Manager) getNodeLocked(id string) (*node.Node, error) {
	e, err := m.db.GetEntity("node", id)
	if err != nil || e == nil {
		return nil, fmt.Errorf("node %s not found", id)
	}
	var n node.Node
	return &n, json.Unmarshal([]byte(e.Data), &n)
}

func (m *Manager) buildConfig(n *node.Node, gid uint32) ([]byte, error) {
	return builder.Build(n, m.settings, gid)
}

func (m *Manager) findXray() string {
	for _, p := range []string{
		filepath.Join(m.dataDir, "xray"),
		"/usr/bin/xray",
		"/usr/local/bin/xray",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("xray"); err == nil {
		return p
	}
	return "xray"
}

func firewallMode(p builder.ProxyMode) firewall.Mode {
	switch p {
	case builder.ProxyModeTProxy:
		return firewall.ModeTProxy
	case builder.ProxyModeRedir:
		return firewall.ModeRedir
	}
	return firewall.ModeNone
}
