package model

import "time"

// Route status values returned by the API.
const (
	StatusOK              = "ok"
	StatusMissing         = "missing"
	StatusGatewayMismatch = "gateway_mismatch"
	StatusError           = "error"
)

// Settings is the persisted daemon configuration.
type Settings struct {
	AutoHeal        bool `json:"autoHeal"`
	IntervalSeconds int  `json:"intervalSeconds"`
}

// Normalized clamps settings into the supported range.
func (s Settings) Normalized() Settings {
	if s.IntervalSeconds < 1 {
		s.IntervalSeconds = 5
	}
	if s.IntervalSeconds > 3600 {
		s.IntervalSeconds = 3600
	}
	return s
}

// Route is a desired host route: host goes through gateway.
type Route struct {
	Host      string    `json:"host"`
	Gateway   string    `json:"gateway"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RouteState is the live kernel state of a desired route.
type RouteState struct {
	Status    string    `json:"status"`
	Present   bool      `json:"present"`
	HostRoute bool      `json:"hostRoute"`
	Gateway   string    `json:"gateway,omitempty"`
	Interface string    `json:"interface,omitempty"`
	Flags     []string  `json:"flags"`
	CheckedAt time.Time `json:"checkedAt"`
	Message   string    `json:"message"`
}

// RouteView is what the UI renders for one managed route.
type RouteView struct {
	Host          string     `json:"host"`
	Gateway       string     `json:"gateway"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	State         RouteState `json:"state"`
	LastAppliedAt *time.Time `json:"lastAppliedAt,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
}

// Summary aggregates route states.
type Summary struct {
	Total           int `json:"total"`
	OK              int `json:"ok"`
	Missing         int `json:"missing"`
	GatewayMismatch int `json:"gatewayMismatch"`
	Error           int `json:"error"`
}

// Event is a single audit/log entry.
type Event struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Level   string    `json:"level"` // info | success | warn | error
	Action  string    `json:"action"`
	Host    string    `json:"host,omitempty"`
	Message string    `json:"message"`
	Command string    `json:"command,omitempty"`
}
