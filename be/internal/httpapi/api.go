// Package httpapi 提供前端调用的 HTTP 接口。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"be/internal/healer"
	"be/internal/routecmd"
	"be/internal/store"
	"be/internal/validate"
)

func New(h *healer.Healer) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		api.GET("/routes", func(c *gin.Context) {
			views, summary, checkedAt := h.CheckAll(c.Request.Context())
			c.JSON(http.StatusOK, gin.H{
				"routes":    views,
				"summary":   summary,
				"checkedAt": checkedAt,
			})
		})
		api.POST("/routes", func(c *gin.Context) {
			addRoute(c, h)
		})
		api.DELETE("/routes/:host", func(c *gin.Context) {
			deleteRoute(c, h)
		})
		api.GET("/route-table", func(c *gin.Context) {
			routeTable(c, h)
		})
	}
	return r
}

func addRoute(c *gin.Context, h *healer.Healer) {
	var req struct {
		Host    string `json:"host"`
		Gateway string `json:"gateway"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	host, gateway, err := validate.HostGateway(req.Host, req.Gateway)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	view, created, err := h.Add(c.Request.Context(), host, gateway)
	if err != nil {
		status := http.StatusBadGateway
		if view.Host == "" {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": err.Error(), "route": view})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"route": view, "created": created})
}

func deleteRoute(c *gin.Context, h *healer.Healer) {
	host, err := validate.IPv4(c.Param("host"))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Delete(c.Request.Context(), host); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(c, http.StatusNotFound, host+" 不在托管列表中")
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": true})
}

type tableEntry struct {
	routecmd.TableEntry
	Managed bool `json:"managed"`
}

func routeTable(c *gin.Context, h *healer.Healer) {
	routes := h.Routes()
	managed := make(map[string]bool, len(routes))
	for _, r := range routes {
		managed[r.Host] = true
	}

	raw, command, err := h.RouteTable(c.Request.Context())
	entries := routecmd.ParseNetstat(raw)
	if strings.Contains(command, " ip route ") {
		entries = routecmd.ParseIPRoute(raw)
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

func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}
