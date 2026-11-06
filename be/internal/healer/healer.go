// Package healer 负责巡检托管路由，发现丢失或网关错误时自动恢复。
package healer

import (
	"context"
	"fmt"
	"log"
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
}

func New(st *store.Store, interval time.Duration) *Healer {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	runner := &execx.Runner{Timeout: 10 * time.Second}
	return &Healer{store: st, ops: routecmd.New(runner), interval: interval}
}

// RouteTable 返回当前内核路由表，供页面展示。
func (h *Healer) RouteTable(ctx context.Context) (raw, command string, err error) {
	return h.ops.Table(ctx)
}

// Routes 返回托管路由列表（只读，不执行任何系统命令）。
func (h *Healer) Routes() []model.Route {
	return h.store.Routes()
}

// View 读取一条路由的实时状态，不做任何修改。
func (h *Healer) View(ctx context.Context, route model.Route) model.RouteView {
	insp, err := h.ops.Inspect(ctx, route.Host)
	return buildView(route, insp, err, time.Now())
}

// CheckAll 读取所有托管路由的实时状态。
func (h *Healer) CheckAll(ctx context.Context) ([]model.RouteView, model.Summary, time.Time) {
	routes := h.store.Routes()
	views := make([]model.RouteView, 0, len(routes))
	for _, r := range routes {
		views = append(views, h.View(ctx, r))
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
	if insp.Present && insp.HostRoute {
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
		if !changed {
			if err != nil {
				log.Printf("巡检失败 %s: %v", route.Host, err)
			}
			continue
		}
		if err != nil {
			log.Printf("自动恢复失败 %s -> %s: %v", route.Host, route.Gateway, err)
			continue
		}
		log.Printf("已自动恢复路由 %s -> %s（%s）", route.Host, route.Gateway, view.Interface)
	}
}

// reconcileOne 检查并按需重建一条路由。changed 表示执行过删除/添加。
func (h *Healer) reconcileOne(ctx context.Context, route model.Route) (model.RouteView, bool, error) {
	h.opMu.Lock()
	defer h.opMu.Unlock()

	insp, err := h.ops.Inspect(ctx, route.Host)
	view := buildView(route, insp, err, time.Now())
	if err != nil {
		return view, false, err
	}
	if view.Status == model.StatusOK {
		return view, false, nil
	}

	if insp.Present && insp.HostRoute {
		if err := h.ops.DeleteHost(ctx, route.Host); err != nil {
			return view, false, err
		}
	}

	if err := h.ops.AddHost(ctx, route.Host, route.Gateway); err != nil {
		return view, true, err
	}

	verified, verifyErr := h.ops.Inspect(ctx, route.Host)
	verifiedView := buildView(route, verified, verifyErr, time.Now())
	if verifyErr != nil {
		return verifiedView, true, verifyErr
	}
	if verifiedView.Status != model.StatusOK {
		return verifiedView, true, fmt.Errorf("%s", verifiedView.Message)
	}
	return verifiedView, true, nil
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

	switch {
	case inspectErr != nil:
		view.Status = model.StatusError
		view.Message = inspectErr.Error()
	case !insp.Present:
		view.Status = model.StatusMissing
		view.Message = "路由表中未找到该主机路由"
	case !insp.HostRoute:
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
		view.Message = "主机路由已就绪"
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
