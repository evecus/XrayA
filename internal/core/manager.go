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
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraya/xraya/internal/auth"
	"github.com/xraya/xraya/internal/builder"
	"github.com/xraya/xraya/internal/firewall"
	"github.com/xraya/xraya/internal/node"
	"github.com/xraya/xraya/internal/storage"
	"github.com/xraya/xraya/internal/subscription"
)

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
	// Clear old nodes from this group
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

	cfg, err := m.buildConfig(n)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	cfgPath := filepath.Join(m.dataDir, "run", "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	xrayBin := m.findXray()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, xrayBin, "run", "-c", cfgPath)
	cmd.Env = append(os.Environ(),
		"XRAY_LOCATION_ASSET="+m.dataDir,
		"V2RAY_LOCATION_ASSET="+m.dataDir,
	)

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		cancel()
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
		m.mu.Lock()
		if m.status == StatusRunning {
			m.status = StatusError
			m.errMsg = "xray exited unexpectedly"
			m.st.Running = false
			_ = m.db.SaveSetting("state", &m.st)
		}
		m.mu.Unlock()
	}()

	go func() {
		if err := m.setupFirewall(); err != nil {
			log.Printf("xraya: firewall: %v", err)
		}
	}()

	log.Printf("xraya: started [%s] node=%s", xrayBin, n.Name)
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	m.cleanupFirewall()
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

func (m *Manager) buildConfig(n *node.Node) ([]byte, error) {
	return builder.Build(n, m.settings)
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

func (m *Manager) setupFirewall() error {
	s := m.settings
	mode := firewallMode(s.ProxyMode)
	if mode == firewall.ModeNone {
		return nil
	}
	return firewall.New(mode, s.TProxyPort,
		filepath.Join(m.dataDir, "xraya.nft")).Setup()
}

func (m *Manager) cleanupFirewall() {
	s := m.settings
	mode := firewallMode(s.ProxyMode)
	if mode == firewall.ModeNone {
		return
	}
	firewall.New(mode, s.TProxyPort,
		filepath.Join(m.dataDir, "xraya.nft")).Cleanup()
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
