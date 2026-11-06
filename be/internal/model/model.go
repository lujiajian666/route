package model

import "time"

const (
	StatusOK              = "ok"
	StatusMissing         = "missing"
	StatusGatewayMismatch = "gateway_mismatch"
	StatusError           = "error"
)

// Route 是托管的路由：目标地址走指定网关。
type Route struct {
	Host      string    `json:"host"`
	Gateway   string    `json:"gateway"`
	CreatedAt time.Time `json:"createdAt"`
}

// RouteView 是返回给前端的一条路由及其当前内核状态。
type RouteView struct {
	Host           string    `json:"host"`
	Gateway        string    `json:"gateway"`
	CreatedAt      time.Time `json:"createdAt"`
	Status         string    `json:"status"`
	Present        bool      `json:"present"`
	HostRoute      bool      `json:"hostRoute"`
	CurrentGateway string    `json:"currentGateway,omitempty"`
	Interface      string    `json:"interface,omitempty"`
	Flags          []string  `json:"flags"`
	Message        string    `json:"message"`
	CheckedAt      time.Time `json:"checkedAt"`
}

// Summary 汇总所有托管路由的状态。
type Summary struct {
	Total           int `json:"total"`
	OK              int `json:"ok"`
	Missing         int `json:"missing"`
	GatewayMismatch int `json:"gatewayMismatch"`
	Error           int `json:"error"`
}
