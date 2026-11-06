package healer

import (
	"testing"
	"time"

	"be/internal/model"
	"be/internal/routecmd"
)

func TestEvaluate(t *testing.T) {
	route := model.Route{Host: "5.5.2.13", Gateway: "192.168.100.254"}

	tests := []struct {
		name   string
		insp   routecmd.Inspection
		err    error
		status string
	}{
		{
			name:   "ready host route",
			insp:   routecmd.Inspection{Present: true, HostRoute: true, Gateway: "192.168.100.254"},
			status: model.StatusOK,
		},
		{
			name:   "missing route",
			insp:   routecmd.Inspection{Present: false},
			status: model.StatusMissing,
		},
		{
			name:   "wrong gateway",
			insp:   routecmd.Inspection{Present: true, HostRoute: true, Gateway: "192.168.100.1"},
			status: model.StatusGatewayMismatch,
		},
		{
			name:   "default route fallback",
			insp:   routecmd.Inspection{Present: true, HostRoute: false, Gateway: "192.168.100.1"},
			status: model.StatusMissing,
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
			state := evaluate(route, tc.insp, tc.err, time.Now())
			if state.Status != tc.status {
				t.Fatalf("expected %s, got %s (%s)", tc.status, state.Status, state.Message)
			}
			if state.CheckedAt.IsZero() {
				t.Fatal("CheckedAt should be set")
			}
			if state.Flags == nil {
				t.Fatal("Flags should never be nil (JSON should be [] not null)")
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	views := []model.RouteView{
		{State: model.RouteState{Status: model.StatusOK}},
		{State: model.RouteState{Status: model.StatusMissing}},
		{State: model.RouteState{Status: model.StatusMissing}},
		{State: model.RouteState{Status: model.StatusGatewayMismatch}},
		{State: model.RouteState{Status: model.StatusError}},
	}
	s := summarize(views)
	if s.Total != 5 || s.OK != 1 || s.Missing != 2 || s.GatewayMismatch != 1 || s.Error != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}
}

func TestFriendlyApplyError(t *testing.T) {
	if got := friendlyApplyError(errString("sudo: a password is required")); got == "sudo: a password is required" {
		t.Fatal("expected an actionable hint for sudo password errors")
	}
	if got := friendlyApplyError(errString("route: operation not permitted")); got == "route: operation not permitted" {
		t.Fatal("expected an actionable hint for permission errors")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
