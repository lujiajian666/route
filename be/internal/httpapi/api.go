// Package httpapi exposes the route daemon over HTTP for the Next.js UI.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"be/internal/healer"
	"be/internal/routecmd"
	"be/internal/store"
	"be/internal/validate"
)

// Config holds server-level configuration.
type Config struct {
	Version        string
	Listen         string
	StorePath      string
	DryRun         bool
	AllowedOrigins []string
}

type api struct {
	h   *healer.Healer
	cfg Config

	sudoMu  sync.Mutex
	sudoOK  bool
	sudoMsg string
	sudoAt  time.Time
}

// New builds the Gin engine.
func New(h *healer.Healer, cfg Config) *gin.Engine {
	a := &api{h: h, cfg: cfg}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery(), requestLogger(), maxBody(1<<20), cors(cfg.AllowedOrigins))

	engine.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":    "route-daemon",
			"version": cfg.Version,
			"api":     "/api",
		})
	})

	apiGroup := engine.Group("/api")
	{
		apiGroup.GET("/health", a.health)
		apiGroup.GET("/system", a.system)
		apiGroup.GET("/routes", a.listRoutes)
		apiGroup.POST("/routes", a.addRoute)
		apiGroup.DELETE("/routes/:host", a.deleteRoute)
		apiGroup.POST("/routes/:host/apply", a.applyRoute)
		apiGroup.POST("/reconcile", a.reconcile)
		apiGroup.GET("/route-table", a.routeTable)
		apiGroup.GET("/logs", a.logs)
		apiGroup.GET("/settings", a.getSettings)
		apiGroup.PATCH("/settings", a.updateSettings)
	}
	return engine
}

func (a *api) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now()})
}

func (a *api) listRoutes(c *gin.Context) {
	views, summary, checkedAt := a.h.CheckAll(c.Request.Context())
	settings := a.h.Store().Settings()
	lastAt, lastSummary, lastErr := a.h.LastReconcile()

	payload := gin.H{
		"routes":             views,
		"summary":            summary,
		"settings":           settings,
		"checkedAt":          checkedAt,
		"dryRun":             a.h.DryRun(),
		"lastReconcileError": lastErr,
	}
	if !lastAt.IsZero() {
		payload["lastReconcileAt"] = lastAt
		payload["lastReconcileSummary"] = lastSummary
	}
	c.JSON(http.StatusOK, payload)
}

type addRouteRequest struct {
	Host    string `json:"host"`
	Gateway string `json:"gateway"`
}

func (a *api) addRoute(c *gin.Context) {
	var req addRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	host, gateway, err := validate.HostGateway(req.Host, req.Gateway)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	_, created, err := a.h.Store().Upsert(host, gateway)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	action := "update"
	if created {
		action = "add"
	}
	a.h.Log().Add("info", action, host, fmt.Sprintf("已保存托管路由 %s → %s", host, gateway), "")

	result, applyErr := a.h.ApplyHost(c.Request.Context(), host, "manual")
	route, _ := a.h.Store().Get(host)
	view := a.h.View(c.Request.Context(), route)

	status := http.StatusCreated
	payload := gin.H{
		"route":   view,
		"apply":   result,
		"created": created,
	}
	if applyErr != nil {
		status = http.StatusBadGateway
		payload["error"] = applyDetailMessage(result, applyErr)
	}
	c.JSON(status, payload)
}

func (a *api) deleteRoute(c *gin.Context) {
	host, err := validate.IPv4(c.Param("host"))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.h.DeleteRoute(c.Request.Context(), host)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, fmt.Sprintf("%s 不在托管列表中", host))
		return
	case err != nil:
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": result, "removed": true})
}

func (a *api) applyRoute(c *gin.Context) {
	host, err := validate.IPv4(c.Param("host"))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.h.ApplyHost(c.Request.Context(), host, "manual")
	route, _ := a.h.Store().Get(host)
	view := a.h.View(c.Request.Context(), route)
	switch {
	case errors.Is(err, healer.ErrRouteNotManaged):
		fail(c, http.StatusNotFound, fmt.Sprintf("%s 不在托管列表中", host))
		return
	case err != nil:
		c.JSON(http.StatusBadGateway, gin.H{"error": applyDetailMessage(result, err), "apply": result, "route": view})
		return
	}
	c.JSON(http.StatusOK, gin.H{"apply": result, "route": view})
}

func (a *api) reconcile(c *gin.Context) {
	results, summary, at := a.h.ReconcileAll(c.Request.Context(), "manual")
	c.JSON(http.StatusOK, gin.H{
		"results":   results,
		"summary":   summary,
		"checkedAt": at,
	})
}

type tableEntry struct {
	routecmd.TableEntry
	Managed bool `json:"managed"`
}

func (a *api) routeTable(c *gin.Context) {
	raw, command, err := a.h.Ops().Table(c.Request.Context())
	entries := routecmd.ParseNetstat(raw)
	if strings.Contains(command, " ip route ") {
		entries = routecmd.ParseIPRoute(raw)
	}
	managed := make(map[string]bool)
	for _, r := range a.h.Store().Routes() {
		managed[r.Host] = true
	}
	table := make([]tableEntry, 0, len(entries))
	for _, e := range entries {
		table = append(table, tableEntry{TableEntry: e, Managed: managed[e.Destination]})
	}
	payload := gin.H{
		"raw":       raw,
		"command":   command,
		"entries":   table,
		"fetchedAt": time.Now(),
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	c.JSON(http.StatusOK, payload)
}

func (a *api) logs(c *gin.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 500 {
		limit = 500
	}
	c.JSON(http.StatusOK, gin.H{"logs": a.h.Log().List(limit)})
}

func (a *api) getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"settings": a.h.Store().Settings()})
}

func (a *api) updateSettings(c *gin.Context) {
	var req struct {
		AutoHeal        *bool `json:"autoHeal"`
		IntervalSeconds *int  `json:"intervalSeconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	current := a.h.Store().Settings()
	if req.AutoHeal != nil {
		current.AutoHeal = *req.AutoHeal
	}
	if req.IntervalSeconds != nil {
		if *req.IntervalSeconds < 1 || *req.IntervalSeconds > 3600 {
			fail(c, http.StatusBadRequest, "巡检间隔必须在 1 - 3600 秒之间")
			return
		}
		current.IntervalSeconds = *req.IntervalSeconds
	}
	current = current.Normalized()
	if err := a.h.Store().SetSettings(current); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	a.h.Log().Add("info", "settings", "",
		fmt.Sprintf("守护设置已更新：自动修复=%s，间隔=%ds", onOff(current.AutoHeal), current.IntervalSeconds), "")
	c.JSON(http.StatusOK, gin.H{"settings": current})
}

func (a *api) system(c *gin.Context) {
	hostname, _ := os.Hostname()
	isRoot := os.Geteuid() == 0
	sudoReady, sudoMsg := a.sudoStatus(c.Request.Context())
	lastAt, lastSummary, lastErr := a.h.LastReconcile()
	settings := a.h.Store().Settings()
	previews := a.h.Ops().CommandPreviews("", "")

	payload := gin.H{
		"version":     a.cfg.Version,
		"platform":    a.h.Ops().Platform(),
		"arch":        runtime.GOARCH,
		"goVersion":   runtime.Version(),
		"hostname":    hostname,
		"pid":         os.Getpid(),
		"isRoot":      isRoot,
		"useSudo":     a.h.Runner().Sudo,
		"sudoReady":   sudoReady,
		"sudoMessage": sudoMsg,
		"dryRun":      a.h.DryRun(),
		"listen":      a.cfg.Listen,
		"storePath":   a.h.Store().Path(),
		"settings":    settings,
		"startedAt":   a.h.StartedAt(),
		"serverTime":  time.Now(),
		"routeBinary": a.h.Ops().RouteBinary(),
		"commands":    previews,
	}
	if !lastAt.IsZero() {
		payload["lastReconcileAt"] = lastAt
		payload["lastReconcileSummary"] = lastSummary
	}
	if lastErr != "" {
		payload["lastReconcileError"] = lastErr
	}
	c.JSON(http.StatusOK, payload)
}

func (a *api) sudoStatus(ctx context.Context) (bool, string) {
	a.sudoMu.Lock()
	defer a.sudoMu.Unlock()
	if !a.sudoAt.IsZero() && time.Since(a.sudoAt) < 10*time.Second {
		return a.sudoOK, a.sudoMsg
	}
	ok, msg := a.h.Ops().ProbeSudo(ctx)
	if ok {
		msg = ""
	}
	a.sudoOK, a.sudoMsg, a.sudoAt = ok, msg, time.Now()
	return ok, msg
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s -> %d (%s)", c.Request.Method, c.Request.URL.Path,
			c.Writer.Status(), time.Since(start).Round(time.Millisecond))
	}
}

func maxBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func cors(origins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(origins))
	allowAll := false
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
		}
		if o != "" {
			allowed[o] = true
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && (allowAll || allowed[origin]) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// applyDetailMessage prefers the healer's actionable message (which may include
// sudo / permission hints) over the raw command error.
func applyDetailMessage(result healer.ApplyResult, err error) string {
	if result.Error != "" {
		return result.Error
	}
	return err.Error()
}

func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

func onOff(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}
