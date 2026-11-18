// Package validate contains input validation shared by the API layer.
package validate

import (
	"fmt"
	"net"
	"strconv"
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

// Target accepts "5.5.2.13" or "5.5.2.0/24".
// A host target is returned without a prefix; a network target is normalized
// to its network address (5.5.2.11/24 -> 5.5.2.0/24).
func Target(raw string) (target string, isNetwork bool, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false, fmt.Errorf("目标地址不能为空")
	}

	ipPart := s
	prefix := 32
	if strings.Contains(s, "/") {
		var prefixPart string
		ipPart, prefixPart, _ = strings.Cut(s, "/")
		prefix, err = strconv.Atoi(prefixPart)
		if err != nil || prefix < 1 || prefix > 32 {
			return "", false, fmt.Errorf("掩码必须是 1-32，例如 5.5.2.0/24")
		}
	}

	host, err := IPv4(ipPart)
	if err != nil {
		return "", false, fmt.Errorf("目标地址无效：%w", err)
	}
	ip := net.ParseIP(host)
	if ip.IsUnspecified() {
		return "", false, fmt.Errorf("目标地址不能是 0.0.0.0")
	}
	if ip.IsMulticast() {
		return "", false, fmt.Errorf("目标地址不能是组播地址")
	}

	if prefix == 32 {
		return host, false, nil
	}
	_, network, err := net.ParseCIDR(fmt.Sprintf("%s/%d", host, prefix))
	if err != nil {
		return "", false, fmt.Errorf("目标网段无效：%w", err)
	}
	return network.String(), true, nil
}

// TargetGateway validates a target (IP or CIDR) and its next-hop gateway.
func TargetGateway(rawTarget, rawGateway string) (target, gateway string, isNetwork bool, err error) {
	target, isNetwork, err = Target(rawTarget)
	if err != nil {
		return "", "", false, err
	}
	gateway, err = IPv4(rawGateway)
	if err != nil {
		return "", "", false, fmt.Errorf("网关地址无效：%w", err)
	}

	gwIP := net.ParseIP(gateway)
	if gateway == "255.255.255.255" || gwIP.IsUnspecified() || gwIP.IsMulticast() {
		return "", "", false, fmt.Errorf("网关必须是可用的单播 IPv4 地址")
	}
	if !isNetwork && target == gateway {
		return "", "", false, fmt.Errorf("网关不能与目标地址相同")
	}
	return target, gateway, isNetwork, nil
}
