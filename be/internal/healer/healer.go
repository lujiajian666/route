// Package healer 负责巡检托管路由，发现丢失或网关不对时自动恢复。
package healer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"be/internal/execx"
	"be/internal/model"
	"be/internal/routecmd"
	"be/internal/store"
)

type Healer struct {
	store    *store.Store
	ops      *routecmd.Ops
	interval time.Duration
	opMu     sync.Mutex

	errMu   sync.Mutex
	lastErr map[string]string
}

func New(st *store.Store, interval time.Duration) *Healer {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	runner := &execx.Runner{Timeout: 10 * time.Second}
	return &Healer{
		store:    st,
		ops:      routecmd.New(runner),
		interval: interval,
		lastErr:  make(map[string]string),
	}
}

// RouteTable 返回当前内核路由表，供页面展示。
func (h *Healer) RouteTable(ctx context.Context) (raw, command string, err error) {
	return h.ops.Table(ctx)
}

// Routes 返回托管路由列表（只读，不执行任何系统命令）。
func (h *Healer) Routes() []model.Route {
	return h.store.Routes()
}

// CheckAll 读取所有托管路由的实时状态。
func (h *Healer) CheckAll(ctx context.Context) ([]model.RouteView, model.Summary, time.Time) {
	routes := h.store.Routes()
	views := make([]model.RouteView, 0, len(routes))
	for _, r := range routes {
		views = append(views, h.view(ctx, r))
	}
	return views, summarize(views), time.Now()
}

// Add 保存一条路由并立即下发、核验。
func (h *Healer) Add(ctx context.Context, host, gateway string) (model.RouteView, bool, error) {
	route, created, err := h.store.Upsert(host, gateway)
	if err != nil {
		return model.RouteView{}, false, err
	}
	view, _, applyErr := h.reconcileOne(ctx, route)
	return view, created, applyErr
}

// Delete 删除内核主机路由并停止托管。
func (h *Healer) Delete(ctx context.Context, host string) error {
	h.opMu.Lock()
	defer h.opMu.Unlock()

	if _, ok := h.store.Get(host); !ok {
		return store.ErrNotFound
	}
	insp, err := h.ops.Inspect(ctx, host)
	if err != nil {
		return err
	}
	if insp.Present && (insp.HostRoute || insp.NetworkRoute) {
		if err := h.ops.DeleteHost(ctx, host); err != nil {
			return err
		}
	}
	return h.store.Delete(host)
}

// Run 启动后台巡检：启动时立即检查一次，之后按 interval 循环。
func (h *Healer) Run(ctx context.Context) {
	h.reconcile(ctx)

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.reconcile(ctx)
		}
	}
}

func (h *Healer) reconcile(ctx context.Context) {
	for _, route := range h.store.Routes() {
		view, changed, err := h.reconcileOne(ctx, route)
		if err != nil {
			h.logFailure(route, changed, err)
			continue
		}
		h.clearFailure(route.Host)
		if changed {
			log.Printf("已自动恢复路由 %s -> %s（%s）", route.Host, route.Gateway, view.Interface)
		}
	}
}

// reconcileOne 检查并按需重建一条路由。changed 表示执行过删除/添加。
func (h *Healer) reconcileOne(ctx context.Context, route model.Route) (model.RouteView, bool, error) {
	h.opMu.Lock()
	defer h.opMu.Unlock()

	view, insp, err := h.viewWithInspection(ctx, route)
	if err != nil {
		return view, false, err
	}
	if view.Status == model.StatusOK {
		return view, false, nil
	}

	if insp.Present && (insp.HostRoute || insp.NetworkRoute) {
		if err := h.ops.DeleteHost(ctx, route.Host); err != nil {
			return view, false, err
		}
	}
	if err := h.ops.AddHost(ctx, route.Host, route.Gateway); err != nil {
		return view, true, err
	}

	view, _, err = h.viewWithInspection(ctx, route)
	if err != nil {
		return view, true, err
	}
	if view.Status != model.StatusOK {
		return view, true, errors.New(view.Message)
	}
	return view, true, nil
}

// view 读取路由状态，并用接口的 DHCP 网关做二次校验。
func (h *Healer) view(ctx context.Context, route model.Route) model.RouteView {
	view, _, _ := h.viewWithInspection(ctx, route)
	return view
}

func (h *Healer) viewWithInspection(ctx context.Context, route model.Route) (model.RouteView, routecmd.Inspection, error) {
	insp, err := h.ops.Inspect(ctx, route.Host)
	view := buildView(route, insp, err, time.Now())
	if err != nil {
		return view, insp, err
	}
	if view.Status != model.StatusOK || insp.Interface == "" {
		return view, insp, nil
	}

	gateway, ok, dhcpErr := h.ops.InterfaceGateway(ctx, insp.Interface)
	if dhcpErr != nil || !ok {
		// 静态地址或读取 DHCP 失败时，只按 route -n get 的结果判断。
		return view, insp, nil
	}
	if gateway != route.Gateway {
		view.Status = model.StatusGatewayMismatch
		view.Message = fmt.Sprintf("接口 %s 的 DHCP 网关是 %s，期望 %s", insp.Interface, gateway, route.Gateway)
	}
	return view, insp, nil
}

func (h *Healer) logFailure(route model.Route, changed bool, err error) {
	h.errMu.Lock()
	defer h.errMu.Unlock()

	msg := err.Error()
	if h.lastErr[route.Host] == msg {
		return
	}
	h.lastErr[route.Host] = msg
	if changed {
		log.Printf("自动恢复失败 %s -> %s: %v", route.Host, route.Gateway, err)
		return
	}
	log.Printf("巡检跳过 %s: %v", route.Host, err)
}

func (h *Healer) clearFailure(host string) {
	h.errMu.Lock()
	delete(h.lastErr, host)
	h.errMu.Unlock()
}

func buildView(route model.Route, insp routecmd.Inspection, inspectErr error, at time.Time) model.RouteView {
	view := model.RouteView{
		Host:           route.Host,
		Gateway:        route.Gateway,
		Present:        insp.Present,
		HostRoute:      insp.HostRoute,
		CurrentGateway: insp.Gateway,
		Interface:      insp.Interface,
		Flags:          insp.Flags,
		CheckedAt:      at,
	}
	if view.Flags == nil {
		view.Flags = []string{}
	}

	isNetwork := strings.Contains(route.Host, "/")
	switch {
	case inspectErr != nil:
		view.Status = model.StatusError
		view.Message = inspectErr.Error()
	case !insp.Present:
		view.Status = model.StatusMissing
		view.Message = "路由表中未找到该路由"
	case isNetwork && !insp.NetworkRoute:
		view.Status = model.StatusMissing
		if insp.Gateway != "" {
			view.Message = fmt.Sprintf("未建立网段路由，当前由其它路由（网关 %s）转发", insp.Gateway)
		} else {
			view.Message = "未建立网段路由"
		}
	case !isNetwork && !insp.HostRoute:
		view.Status = model.StatusMissing
		if insp.Gateway != "" {
			view.Message = fmt.Sprintf("未建立主机路由，当前由其它路由（网关 %s）转发", insp.Gateway)
		} else {
			view.Message = "未建立主机路由"
		}
	case insp.Gateway != "" && insp.Gateway != route.Gateway:
		view.Status = model.StatusGatewayMismatch
		view.Message = fmt.Sprintf("当前走网关 %s，期望 %s", insp.Gateway, route.Gateway)
	default:
		view.Status = model.StatusOK
		view.Message = "路由已就绪"
	}
	return view
}

func summarize(views []model.RouteView) model.Summary {
	s := model.Summary{Total: len(views)}
	for _, v := range views {
		switch v.Status {
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
