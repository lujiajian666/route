// Package healer keeps managed host routes in sync with the kernel.
package healer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"be/internal/events"
	"be/internal/execx"
	"be/internal/model"
	"be/internal/routecmd"
	"be/internal/store"
)

// ErrRouteNotManaged is returned when a host is not in the desired set.
var ErrRouteNotManaged = errors.New("route not managed")

// ApplyResult describes one add / repair attempt.
type ApplyResult struct {
	Host      string           `json:"host"`
	Gateway   string           `json:"gateway"`
	Trigger   string           `json:"trigger"`
	Applied   bool             `json:"applied"`
	Changed   bool             `json:"changed"`
	Simulated bool             `json:"simulated"`
	State     model.RouteState `json:"state"`
	Message   string           `json:"message"`
	Warnings  []string         `json:"warnings,omitempty"`
	Error     string           `json:"error,omitempty"`
	At        time.Time        `json:"at"`
}

// DeleteResult describes a removal attempt.
type DeleteResult struct {
	Host      string    `json:"host"`
	Gateway   string    `json:"gateway"`
	Deleted   bool      `json:"deleted"`
	Simulated bool      `json:"simulated"`
	Warnings  []string  `json:"warnings,omitempty"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

// Healer owns the reconciliation loop and all mutating operations.
type Healer struct {
	store   *store.Store
	ops     *routecmd.Ops
	runner  *execx.Runner
	log     *events.Log
	dryRun  bool
	version string

	startedAt time.Time

	opMu sync.Mutex

	stateMu                sync.Mutex
	lastReconcileAt        time.Time
	lastReconcileSummary   model.Summary
	lastReconcileErr       string
	lastReconcileSignature string
	lastApplied            map[string]time.Time
	lastError              map[string]string
}

// New builds a healer.
func New(st *store.Store, ops *routecmd.Ops, runner *execx.Runner, log *events.Log, dryRun bool, version string) *Healer {
	return &Healer{
		store:       st,
		ops:         ops,
		runner:      runner,
		log:         log,
		dryRun:      dryRun,
		version:     version,
		startedAt:   time.Now(),
		lastApplied: make(map[string]time.Time),
		lastError:   make(map[string]string),
	}
}

// Version returns the build version.
func (h *Healer) Version() string { return h.version }

// StartedAt returns the process start time.
func (h *Healer) StartedAt() time.Time { return h.startedAt }

// DryRun reports whether mutations are simulated.
func (h *Healer) DryRun() bool { return h.dryRun }

// Log exposes the audit log.
func (h *Healer) Log() *events.Log { return h.log }

// Store exposes the persisted desired state.
func (h *Healer) Store() *store.Store { return h.store }

// Ops exposes platform routing operations.
func (h *Healer) Ops() *routecmd.Ops { return h.ops }

// Runner exposes the command runner.
func (h *Healer) Runner() *execx.Runner { return h.runner }

// LastReconcile returns the last background reconciliation snapshot.
func (h *Healer) LastReconcile() (at time.Time, summary model.Summary, errMsg string) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	return h.lastReconcileAt, h.lastReconcileSummary, h.lastReconcileErr
}

// View returns a live view of one route.
func (h *Healer) View(ctx context.Context, route model.Route) model.RouteView {
	insp, err := h.ops.Inspect(ctx, route.Host)
	state := evaluate(route, insp, err, time.Now())
	view := model.RouteView{
		Host:      route.Host,
		Gateway:   route.Gateway,
		CreatedAt: route.CreatedAt,
		UpdatedAt: route.UpdatedAt,
		State:     state,
	}
	h.stateMu.Lock()
	if at, ok := h.lastApplied[route.Host]; ok {
		view.LastAppliedAt = &at
	}
	view.LastError = h.lastError[route.Host]
	h.stateMu.Unlock()
	return view
}

// CheckAll inspects every managed route without mutating anything.
func (h *Healer) CheckAll(ctx context.Context) ([]model.RouteView, model.Summary, time.Time) {
	routes := h.store.Routes()
	views := make([]model.RouteView, 0, len(routes))
	for _, r := range routes {
		views = append(views, h.View(ctx, r))
	}
	return views, summarize(views), time.Now()
}

// ApplyHost repairs one managed route.
func (h *Healer) ApplyHost(ctx context.Context, host, trigger string) (ApplyResult, error) {
	route, ok := h.store.Get(host)
	if !ok {
		return ApplyResult{}, ErrRouteNotManaged
	}
	return h.applyRoute(ctx, route, trigger)
}

// ReconcileAll inspects and repairs every managed route.
func (h *Healer) ReconcileAll(ctx context.Context, trigger string) ([]ApplyResult, model.Summary, time.Time) {
	routes := h.store.Routes()
	results := make([]ApplyResult, 0, len(routes))
	views := make([]model.RouteView, 0, len(routes))

	var repaired, failed, abnormal int
	for _, r := range routes {
		res, err := h.applyRoute(ctx, r, trigger)
		results = append(results, res)
		views = append(views, model.RouteView{
			Host:      res.Host,
			Gateway:   res.Gateway,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			State:     res.State,
			LastError: res.Error,
		})
		if res.Changed {
			repaired++
		}
		if err != nil {
			failed++
		}
		if res.State.Status != model.StatusOK {
			abnormal++
		}
	}

	summary := summarize(views)
	at := time.Now()

	signature := reconcileSignature(views)
	h.stateMu.Lock()
	changedState := signature != h.lastReconcileSignature
	h.lastReconcileAt = at
	h.lastReconcileSummary = summary
	h.lastReconcileErr = ""
	h.lastReconcileSignature = signature
	if failed > 0 {
		h.lastReconcileErr = fmt.Sprintf("%d 条路由修复失败", failed)
	}
	h.stateMu.Unlock()

	// Only log when the picture changed or a repair happened, otherwise a
	// persistently broken route would spam the audit log every interval.
	if abnormal > 0 && (changedState || repaired > 0) {
		if h.dryRun {
			h.log.Add("warn", "reconcile", "", fmt.Sprintf("巡检发现 %d 条路由异常（dry-run 未修改路由表）", abnormal), "")
		} else if repaired > 0 {
			h.log.Add("warn", "reconcile", "", fmt.Sprintf("检测到 %d 条路由异常（疑似路由表被重置），已恢复 %d 条", abnormal, repaired), "")
		} else {
			h.log.Add("warn", "reconcile", "", fmt.Sprintf("巡检发现 %d 条路由异常且未能恢复", abnormal), "")
		}
	}
	return results, summary, at
}

// DeleteRoute stops managing host and removes the kernel host route if present.
func (h *Healer) DeleteRoute(ctx context.Context, host string) (DeleteResult, error) {
	h.opMu.Lock()
	defer h.opMu.Unlock()

	route, err := h.store.Delete(host)
	if err != nil {
		return DeleteResult{}, err
	}
	res := DeleteResult{Host: host, Gateway: route.Gateway, At: time.Now()}

	insp, inspectErr := h.ops.Inspect(ctx, host)
	switch {
	case inspectErr != nil:
		res.Warnings = append(res.Warnings, "检查路由状态失败: "+inspectErr.Error())
	case h.dryRun:
		res.Simulated = true
		res.Message = "dry-run：未实际删除内核路由"
		h.log.Add("info", "delete", host, "dry-run：模拟删除主机路由", h.ops.CommandPreviews(host, route.Gateway)["delete"])
	case !insp.Present || !insp.HostRoute:
		res.Message = "内核中不存在该主机路由，已从托管列表移除"
	default:
		delRes, delErr := h.ops.DeleteHost(ctx, host)
		if delErr != nil {
			res.Warnings = append(res.Warnings, "删除内核路由失败: "+delErr.Error())
			h.log.Add("warn", "delete", host, "删除内核路由失败: "+delErr.Error(), delRes.Command)
		} else {
			res.Deleted = true
			res.Message = "已删除主机路由并停止托管"
			h.log.Add("success", "delete", host, fmt.Sprintf("已删除主机路由 %s", host), delRes.Command)
		}
	}

	h.forget(host)
	if res.Message == "" {
		res.Message = "已从托管列表移除"
	}
	return res, nil
}

// Forget removes runtime metadata for a host.
func (h *Healer) Forget(host string) { h.forget(host) }

func (h *Healer) forget(host string) {
	h.stateMu.Lock()
	delete(h.lastApplied, host)
	delete(h.lastError, host)
	h.stateMu.Unlock()
}

// Run is the background reconciliation loop. It wakes every second and repairs
// routes when the configured interval has elapsed.
func (h *Healer) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var lastRun time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings := h.store.Settings()
			if !settings.AutoHeal {
				continue
			}
			interval := time.Duration(settings.IntervalSeconds) * time.Second
			if !lastRun.IsZero() && time.Since(lastRun) < interval {
				continue
			}
			lastRun = time.Now()
			if _, _, _ = h.ReconcileAll(ctx, "auto"); ctx.Err() != nil {
				return
			}
		}
	}
}

func (h *Healer) applyRoute(ctx context.Context, route model.Route, trigger string) (ApplyResult, error) {
	h.opMu.Lock()
	defer h.opMu.Unlock()

	res := ApplyResult{
		Host:    route.Host,
		Gateway: route.Gateway,
		Trigger: trigger,
		At:      time.Now(),
	}

	insp, err := h.ops.Inspect(ctx, route.Host)
	state := evaluate(route, insp, err, time.Now())
	res.State = state

	if err != nil {
		msg := "检查路由状态失败: " + err.Error()
		res.Error = msg
		res.Message = msg
		if h.recordError(route.Host, msg) {
			h.log.Add("error", "inspect", route.Host, msg, "")
		}
		return res, err
	}
	if state.Status == model.StatusOK {
		res.Message = "路由已就绪，无需修改"
		return res, nil
	}

	previews := h.ops.CommandPreviews(route.Host, route.Gateway)

	if h.dryRun {
		if insp.Present && insp.HostRoute {
			if _, derr := h.ops.DeleteHost(ctx, route.Host); derr == nil && trigger != "auto" {
				h.log.Add("info", "delete", route.Host, "dry-run：模拟删除旧主机路由", previews["delete"])
			}
		}
		addRes, aerr := h.ops.AddHost(ctx, route.Host, route.Gateway)
		if aerr != nil {
			msg := "模拟添加路由失败: " + aerr.Error()
			res.Error = msg
			res.Message = msg
			if h.recordError(route.Host, msg) {
				h.log.Add("error", "add", route.Host, msg, addRes.Command)
			}
			return res, aerr
		}
		res.Simulated = true
		res.Message = "dry-run 模式：已模拟执行删除/添加命令，未实际修改路由表"
		if trigger != "auto" {
			h.log.Add("info", "add", route.Host, res.Message, addRes.Command)
		}
		return res, nil
	}

	if insp.Present && insp.HostRoute {
		delRes, delErr := h.ops.DeleteHost(ctx, route.Host)
		if delErr != nil {
			res.Warnings = append(res.Warnings, "删除旧路由失败: "+delErr.Error())
			h.log.Add("warn", "delete", route.Host, "删除旧路由失败: "+delErr.Error(), delRes.Command)
		} else {
			h.log.Add("info", "delete", route.Host, "已删除旧主机路由", delRes.Command)
		}
	}

	addRes, addErr := h.ops.AddHost(ctx, route.Host, route.Gateway)
	if addErr != nil {
		msg := friendlyApplyError(addErr)
		res.Error = msg
		res.Message = msg
		if h.recordError(route.Host, msg) {
			h.log.Add("error", "add", route.Host, msg, addRes.Command)
		}
		return res, addErr
	}
	res.Applied = true
	res.Changed = true

	// Verify the kernel really accepted the route.
	verifyInsp, verifyErr := h.ops.Inspect(ctx, route.Host)
	verified := evaluate(route, verifyInsp, verifyErr, time.Now())
	res.State = verified
	if verified.Status != model.StatusOK {
		msg := "命令已执行但核验未通过：" + verified.Message
		if verifyErr != nil {
			msg = "命令已执行但核验失败：" + verifyErr.Error()
		}
		res.Error = msg
		res.Message = msg
		if h.recordError(route.Host, msg) {
			h.log.Add("warn", "verify", route.Host, msg, addRes.Command)
		}
		return res, errors.New(msg)
	}

	h.recordApplied(route.Host)
	res.Message = "已恢复主机路由并核验通过"
	h.log.Add("success", "add", route.Host,
		fmt.Sprintf("已添加主机路由 %s → %s 并核验通过", route.Host, route.Gateway), addRes.Command)
	return res, nil
}

func (h *Healer) recordApplied(host string) {
	h.stateMu.Lock()
	h.lastApplied[host] = time.Now()
	delete(h.lastError, host)
	h.stateMu.Unlock()
}

// recordError stores the latest error and reports whether it changed.
func (h *Healer) recordError(host, msg string) bool {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	changed := h.lastError[host] != msg
	h.lastError[host] = msg
	return changed
}

func evaluate(route model.Route, insp routecmd.Inspection, inspectErr error, at time.Time) model.RouteState {
	state := model.RouteState{
		Present:   insp.Present,
		HostRoute: insp.HostRoute,
		Gateway:   insp.Gateway,
		Interface: insp.Interface,
		Flags:     insp.Flags,
		CheckedAt: at,
	}
	if state.Flags == nil {
		state.Flags = []string{}
	}
	if inspectErr != nil {
		state.Status = model.StatusError
		state.Message = inspectErr.Error()
		return state
	}
	switch {
	case !insp.Present:
		state.Status = model.StatusMissing
		state.Message = "路由表中未找到该主机路由"
	case !insp.HostRoute:
		state.Status = model.StatusMissing
		if insp.Gateway != "" {
			state.Message = fmt.Sprintf("未建立主机路由，当前由其它路由（网关 %s）转发", insp.Gateway)
		} else {
			state.Message = "未建立主机路由"
		}
	case insp.Gateway != "" && insp.Gateway != route.Gateway:
		state.Status = model.StatusGatewayMismatch
		state.Message = fmt.Sprintf("当前走网关 %s，期望 %s", insp.Gateway, route.Gateway)
	default:
		state.Status = model.StatusOK
		state.Message = "主机路由已就绪"
	}
	return state
}

func reconcileSignature(views []model.RouteView) string {
	var b strings.Builder
	for _, v := range views {
		b.WriteString(v.Host)
		b.WriteByte('=')
		b.WriteString(v.State.Status)
		b.WriteByte(';')
	}
	return b.String()
}

func summarize(views []model.RouteView) model.Summary {
	s := model.Summary{Total: len(views)}
	for _, v := range views {
		switch v.State.Status {
		case model.StatusOK:
			s.OK++
		case model.StatusMissing:
			s.Missing++
		case model.StatusGatewayMismatch:
			s.GatewayMismatch++
		default:
			s.Error++
		}
	}
	return s
}

func friendlyApplyError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case containsAny(lower, "password is required", "a terminal is required", "not allowed", "no tty present"):
		return msg + "；请为 route 命令配置免密 sudo，或使用 root 运行后端（见 README）"
	case containsAny(lower, "operation not permitted", "must be root", "permission denied"):
		return msg + "；修改路由表需要 root 权限，请使用 sudo 运行后端或配置 sudoers"
	}
	return msg
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
