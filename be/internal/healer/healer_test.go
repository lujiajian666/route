package healer

import (
	"testing"
	"time"

	"be/internal/model"
	"be/internal/routecmd"
)

func TestBuildView(t *testing.T) {
	route := model.Route{Host: "5.5.2.13", Gateway: "192.168.100.254"}

	tests := []struct {
		name   string
		insp   routecmd.Inspection
		err    error
		status string
	}{
		{
			name:   "ready",
			insp:   routecmd.Inspection{Present: true, HostRoute: true, Gateway: "192.168.100.254", Interface: "en7"},
			status: model.StatusOK,
		},
		{
			name:   "missing",
			insp:   routecmd.Inspection{Present: false},
			status: model.StatusMissing,
		},
		{
			name:   "fallback route",
			insp:   routecmd.Inspection{Present: true, HostRoute: false, Gateway: "192.168.100.1"},
			status: model.StatusMissing,
		},
		{
			name:   "wrong gateway",
			insp:   routecmd.Inspection{Present: true, HostRoute: true, Gateway: "192.168.100.1"},
			status: model.StatusGatewayMismatch,
		},

		{
			name:   "inspect error",
			insp:   routecmd.Inspection{},
			err:    errString("boom"),
			status: model.StatusError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			view := buildView(route, tc.insp, tc.err, time.Now())
			if view.Status != tc.status {
				t.Fatalf("expected %s, got %s (%s)", tc.status, view.Status, view.Message)
			}
			if view.Flags == nil {
				t.Fatal("flags 不能是 nil")
			}
		})
	}
}

func TestBuildViewNetwork(t *testing.T) {
	route := model.Route{Host: "5.5.2.0/24", Gateway: "192.168.100.254"}

	ready := buildView(route, routecmd.Inspection{
		Present: true, NetworkRoute: true, Gateway: "192.168.100.254", Interface: "en7",
	}, nil, time.Now())
	if ready.Status != model.StatusOK {
		t.Fatalf("expected ok, got %s (%s)", ready.Status, ready.Message)
	}

	missing := buildView(route, routecmd.Inspection{
		Present: true, Gateway: "192.168.100.1", Interface: "en0",
	}, nil, time.Now())
	if missing.Status != model.StatusMissing {
		t.Fatalf("expected missing, got %s (%s)", missing.Status, missing.Message)
	}
}

func TestSummarize(t *testing.T) {
	views := []model.RouteView{
		{Status: model.StatusOK},
		{Status: model.StatusOK},
		{Status: model.StatusMissing},
		{Status: model.StatusGatewayMismatch},
		{Status: model.StatusError},
	}
	s := summarize(views)
	if s.Total != 5 || s.OK != 2 || s.Missing != 1 || s.GatewayMismatch != 1 || s.Error != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
