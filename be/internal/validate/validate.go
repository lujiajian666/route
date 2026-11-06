// Package validate contains input validation shared by the API layer.
package validate

import (
	"fmt"
	"net"
	"strings"
)

// IPv4 returns the canonical dotted-quad form of an IPv4 address.
func IPv4(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("IP 地址不能为空")
	}
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("%q 不是合法的 IPv4 地址", raw)
	}
	return ip.To4().String(), nil
}

// HostGateway validates a target host and its next-hop gateway.
func HostGateway(rawHost, rawGateway string) (host, gateway string, err error) {
	host, err = IPv4(rawHost)
	if err != nil {
		return "", "", fmt.Errorf("目标地址无效：%w", err)
	}
	gateway, err = IPv4(rawGateway)
	if err != nil {
		return "", "", fmt.Errorf("网关地址无效：%w", err)
	}

	hostIP := net.ParseIP(host)
	gwIP := net.ParseIP(gateway)

	if hostIP.IsUnspecified() {
		return "", "", fmt.Errorf("目标地址不能是 0.0.0.0")
	}
	if hostIP.IsMulticast() {
		return "", "", fmt.Errorf("目标地址不能是组播地址")
	}
	if gateway == "255.255.255.255" || gwIP.IsUnspecified() || gwIP.IsMulticast() {
		return "", "", fmt.Errorf("网关必须是可用的单播 IPv4 地址")
	}
	if host == gateway {
		return "", "", fmt.Errorf("网关不能与目标地址相同")
	}
	return host, gateway, nil
}
