package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/xraya/xraya/internal/auth"
	"github.com/xraya/xraya/internal/builder"
	"github.com/xraya/xraya/internal/core"
	"github.com/xraya/xraya/internal/node"
	"github.com/xraya/xraya/internal/subscription"
)

type Server struct {
	mgr    *core.Manager
	engine *gin.Engine
}

func NewServer(mgr *core.Manager, webFS embed.FS) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// CORS — allow the dev frontend to call the API
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "X-Session-Token"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	s := &Server{mgr: mgr, engine: r}

	// ── Static web UI ──────────────────────────────────────────────────────
	webSub, err := fs.Sub(webFS, "web")
	if err == nil {
		r.NoRoute(func(ctx *gin.Context) {
			http.FileServer(http.FS(webSub)).ServeHTTP(ctx.Writer, ctx.Request)
		})
	}

	// ── Auth routes (no middleware) ────────────────────────────────────────
	r.POST("/api/login", s.handleLogin)
	r.POST("/api/logout", s.handleLogout)
	r.GET("/api/auth/status", s.handleAuthStatus)

	// ── Protected API ──────────────────────────────────────────────────────
	api := r.Group("/api", mgr.Auth.Middleware())
	{
		// Nodes
		api.GET("/nodes", s.listNodes)
		api.POST("/nodes", s.addNode)
		api.DELETE("/nodes/:id", s.deleteNode)
		api.POST("/nodes/import", s.importLinks)

		// Subscriptions
		api.GET("/subscriptions", s.listSubscriptions)
		api.POST("/subscriptions", s.addSubscription)
		api.DELETE("/subscriptions/:id", s.deleteSubscription)
		api.POST("/subscriptions/:id/update", s.updateSubscription)

		// Core control
		api.POST("/connect", s.connect)
		api.POST("/disconnect", s.disconnect)
		api.GET("/status", s.getStatus)
		api.GET("/logs", s.getLogs)

		// Settings
		api.GET("/settings", s.getSettings)
		api.PUT("/settings", s.putSettings)

		// Auth management
		api.POST("/auth/password", s.setPassword)
	}

	return s
}

func (s *Server) Run(addr string) error { return s.engine.Run(addr) }

// ── Auth ───────────────────────────────────────────────────────────────────

func (s *Server) handleAuthStatus(ctx *gin.Context) {
	ok(ctx, gin.H{
		"hasPassword": s.mgr.Auth.HasPassword(),
	})
}

func (s *Server) handleLogin(ctx *gin.Context) {
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		bad(ctx, err.Error())
		return
	}
	token, err := s.mgr.Auth.Login(req.Password)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	auth.SetCookie(ctx, token)
	ok(ctx, gin.H{"token": token})
}

func (s *Server) handleLogout(ctx *gin.Context) {
	token := ctx.GetHeader("X-Session-Token")
	if token == "" {
		token, _ = ctx.Cookie("xraya_session")
	}
	s.mgr.Auth.Logout(token)
	auth.ClearCookie(ctx)
	ok(ctx, gin.H{})
}

func (s *Server) setPassword(ctx *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		bad(ctx, err.Error())
		return
	}
	if err := s.mgr.Auth.SetPassword(req.Password); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{})
}

// ── Nodes ──────────────────────────────────────────────────────────────────

func (s *Server) listNodes(ctx *gin.Context) {
	nodes, err := s.mgr.ListNodes()
	if err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{"data": nodes})
}

func (s *Server) addNode(ctx *gin.Context) {
	var req struct {
		Link string `json:"link" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		bad(ctx, err.Error())
		return
	}
	n, err := node.ParseLink(req.Link)
	if err != nil {
		bad(ctx, err.Error())
		return
	}
	if err := s.mgr.AddNode(n); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{"data": n})
}

func (s *Server) deleteNode(ctx *gin.Context) {
	if err := s.mgr.DeleteNode(ctx.Param("id")); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{})
}

func (s *Server) importLinks(ctx *gin.Context) {
	var req struct {
		Links []string `json:"links" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		bad(ctx, err.Error())
		return
	}
	var added []*node.Node
	var failed []string
	for _, link := range req.Links {
		n, err := node.ParseLink(link)
		if err != nil {
			failed = append(failed, link)
			continue
		}
		if err := s.mgr.AddNode(n); err != nil {
			failed = append(failed, link)
			continue
		}
		added = append(added, n)
	}
	ok(ctx, gin.H{"added": len(added), "failed": failed})
}

// ── Subscriptions ──────────────────────────────────────────────────────────

func (s *Server) listSubscriptions(ctx *gin.Context) {
	subs, err := s.mgr.ListSubscriptions()
	if err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{"data": subs})
}

func (s *Server) addSubscription(ctx *gin.Context) {
	var g subscription.Group
	if err := ctx.ShouldBindJSON(&g); err != nil {
		bad(ctx, err.Error())
		return
	}
	if err := s.mgr.AddSubscription(&g); err != nil {
		fail(ctx, err)
		return
	}
	// Fetch immediately
	go func() { _ = s.mgr.UpdateSubscription(g.ID) }()
	ok(ctx, gin.H{"data": g})
}

func (s *Server) deleteSubscription(ctx *gin.Context) {
	if err := s.mgr.DeleteSubscription(ctx.Param("id")); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{})
}

func (s *Server) updateSubscription(ctx *gin.Context) {
	if err := s.mgr.UpdateSubscription(ctx.Param("id")); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{})
}

// ── Core control ───────────────────────────────────────────────────────────

func (s *Server) connect(ctx *gin.Context) {
	var req struct {
		NodeID string `json:"nodeId" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		bad(ctx, err.Error())
		return
	}
	if err := s.mgr.Start(req.NodeID); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{})
}

func (s *Server) disconnect(ctx *gin.Context) {
	s.mgr.Stop()
	ok(ctx, gin.H{})
}

func (s *Server) getStatus(ctx *gin.Context) {
	status, errMsg, activeNode := s.mgr.Status()
	ok(ctx, gin.H{
		"status":     status,
		"error":      errMsg,
		"activeNode": activeNode,
	})
}

func (s *Server) getLogs(ctx *gin.Context) {
	ok(ctx, gin.H{"logs": s.mgr.Logs()})
}

// ── Settings ───────────────────────────────────────────────────────────────

func (s *Server) getSettings(ctx *gin.Context) {
	ok(ctx, gin.H{"data": s.mgr.GetSettings()})
}

func (s *Server) putSettings(ctx *gin.Context) {
	var settings builder.Settings
	if err := ctx.ShouldBindJSON(&settings); err != nil {
		bad(ctx, err.Error())
		return
	}
	if err := s.mgr.SetSettings(settings); err != nil {
		fail(ctx, err)
		return
	}
	ok(ctx, gin.H{})
}

// ── Helpers ────────────────────────────────────────────────────────────────

func ok(ctx *gin.Context, data gin.H) {
	data["ok"] = true
	ctx.JSON(http.StatusOK, data)
}

func bad(ctx *gin.Context, msg string) {
	ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": msg})
}

func fail(ctx *gin.Context, err error) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
}
