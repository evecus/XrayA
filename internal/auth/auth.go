package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xraya/xraya/internal/storage"
)

const (
	sessionCookie  = "xraya_session"
	sessionTTL     = 7 * 24 * time.Hour
	settingKey     = "auth"
)

type authConfig struct {
	PasswordHash string `json:"passwordHash"` // sha256 hex, empty = no auth
}

type Manager struct {
	mu       sync.RWMutex
	store    *storage.Store
	cfg      authConfig
	sessions map[string]time.Time // token → expiry
}

func New(db *storage.DB) (*Manager, error) {
	m := &Manager{
		store:    storage.NewStore(db, settingKey),
		sessions: make(map[string]time.Time),
	}
	_ = m.store.Load(&m.cfg)
	return m, nil
}

// HasPassword returns true if a password is configured.
func (m *Manager) HasPassword() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.PasswordHash != ""
}

// SetPassword hashes and stores a new password. Empty = disable auth.
func (m *Manager) SetPassword(password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if password == "" {
		m.cfg.PasswordHash = ""
	} else {
		m.cfg.PasswordHash = hashPassword(password)
	}
	m.sessions = make(map[string]time.Time) // invalidate all sessions
	return m.store.Save(&m.cfg)
}

// Login verifies password and returns a session token.
func (m *Manager) Login(password string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.PasswordHash == "" {
		return "", fmt.Errorf("auth not configured")
	}
	if hashPassword(password) != m.cfg.PasswordHash {
		return "", fmt.Errorf("invalid password")
	}
	token := newToken()
	m.sessions[token] = time.Now().Add(sessionTTL)
	return token, nil
}

// Logout removes the session token.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// Middleware returns a gin middleware that enforces authentication.
// If no password is set, all requests pass through.
func (m *Manager) Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !m.HasPassword() {
			ctx.Next()
			return
		}
		token := ctx.GetHeader("X-Session-Token")
		if token == "" {
			if c, err := ctx.Cookie(sessionCookie); err == nil {
				token = c
			}
		}
		if !m.validateToken(token) {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized,
				gin.H{"error": "unauthorized"})
			return
		}
		ctx.Next()
	}
}

func (m *Manager) validateToken(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok || time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	// sliding window: refresh expiry on each valid request
	m.sessions[token] = time.Now().Add(sessionTTL)
	return true
}

// SetCookie writes the session cookie to the response.
func SetCookie(ctx *gin.Context, token string) {
	ctx.SetCookie(sessionCookie, token,
		int(sessionTTL.Seconds()), "/", "", false, true)
}

// ClearCookie removes the session cookie.
func ClearCookie(ctx *gin.Context) {
	ctx.SetCookie(sessionCookie, "", -1, "/", "", false, true)
}

func hashPassword(p string) string {
	h := sha256.Sum256([]byte(p))
	return hex.EncodeToString(h[:])
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
