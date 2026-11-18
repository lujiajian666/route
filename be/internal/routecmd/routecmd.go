// Package routecmd 封装 macOS / Linux 的路由表命令与输出解析。
//
// macOS: route -n get <host> / route add -host <host> <gateway> / route delete -host <host>
// Linux: ip route get <host> / ip route replace <host>/32 via <gateway> / ip route del <host>/32
package routecmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"be/internal/execx"
)

type Inspection struct {
	Present      bool
	HostRoute    bool
	NetworkRoute bool
	Gateway      string
	Interface    string
	Flags        []string
	Detail       string
}

type TableEntry struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Flags       string `json:"flags"`
	Netif       string `json:"netif"`
	Line        string `json:"line"`
}

type Ops struct {
	platform    string
	runner      *execx.Runner
	routeBin    string
	netstatBin  string
	ipBin       string
	ipconfigBin string
}

func New(runner *execx.Runner) *Ops {
	return &Ops{
		platform:    runtime.GOOS,
		runner:      runner,
		routeBin:    findBin("route", "/sbin/route", "/usr/sbin/route", "/usr/bin/route"),
		netstatBin:  findBin("netstat", "/usr/sbin/netstat", "/sbin/netstat", "/usr/bin/netstat", "/bin/netstat"),
		ipBin:       findBin("ip", "/sbin/ip", "/usr/sbin/ip", "/usr/bin/ip", "/bin/ip"),
		ipconfigBin: findBin("ipconfig", "/usr/sbin/ipconfig", "/sbin/ipconfig"),
	}
}

func (o *Ops) Platform() string { return o.platform }

func (o *Ops) Inspect(ctx context.Context, host string) (Inspection, error) {
	if o.platform == "linux" {
		return o.inspectLinux(ctx, host)
	}
	return o.inspectDarwin(ctx, host)
}

func (o *Ops) inspectDarwin(ctx context.Context, target string) (Inspection, error) {
	query, isNetwork := networkQuery(target)
	res, err := o.runner.Run(ctx, o.routeBin, "-n", "get", query)
	if err != nil {
		if isNotFound(res.Output) {
			return Inspection{Present: false, Detail: res.Output}, nil
		}
		return Inspection{}, err
	}
	destination, mask, gateway, iface, flags, ok := parseDarwinRoute(res.Output)
	if !ok {
		return Inspection{}, fmt.Errorf("无法解析 route -n get %s 的输出", target)
	}

	networkRoute := false
	if isNetwork {
		networkRoute = matchesNetwork(destination, mask, target)
	}

	return Inspection{
		Present:      true,
		HostRoute:    hasFlag(flags, "HOST"),
		NetworkRoute: networkRoute,
		Gateway:      gateway,
		Interface:    iface,
		Flags:        flags,
		Detail:       res.Output,
	}, nil
}

func (o *Ops) inspectLinux(ctx context.Context, target string) (Inspection, error) {
	query, isNetwork := networkQuery(target)
	res, err := o.runner.Run(ctx, o.ipBin, "route", "get", query)
	if err != nil {
		if isNotFound(res.Output) {
			return Inspection{Present: false, Detail: res.Output}, nil
		}
		return Inspection{}, err
	}
	gateway, iface, ok := ParseLinuxGet(res.Output)
	if !ok {
		return Inspection{}, fmt.Errorf("无法解析 ip route get %s 的输出", target)
	}

	hostRoute := false
	networkRoute := false
	flags := []string{}
	if isNetwork {
		if exact, err := o.runner.Run(ctx, o.ipBin, "route", "show", target); err == nil {
			networkRoute = hasExactNetwork(exact.Output, target)
		}
	} else if exact, err := o.runner.Run(ctx, o.ipBin, "route", "show", target+"/32"); err == nil {
		hostRoute = hasExactHostRoute(exact.Output, target)
	}
	if hostRoute {
		flags = append(flags, "HOST")
	}
	if networkRoute {
		flags = append(flags, "NET")
	}
	if gateway != "" {
		flags = append(flags, "GATEWAY")
	}
	return Inspection{
		Present:      true,
		HostRoute:    hostRoute,
		NetworkRoute: networkRoute,
		Gateway:      gateway,
		Interface:    iface,
		Flags:        flags,
		Detail:       res.Output,
	}, nil
}

// InterfaceGateway 读取接口通过 DHCP 拿到的网关，例如 en7 -> 192.168.100.254。
func (o *Ops) InterfaceGateway(ctx context.Context, iface string) (string, bool, error) {
	if o.platform != "darwin" || iface == "" {
		return "", false, nil
	}
	res, err := o.runner.Run(ctx, o.ipconfigBin, "getpacket", iface)
	if err != nil {
		return "", false, err
	}
	gateway, ok := parseIPConfigRouter(res.Output)
	return gateway, ok, nil
}

func parseIPConfigRouter(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "router (") {
			continue
		}
		start := strings.Index(line, "{")
		end := strings.LastIndex(line, "}")
		if start < 0 || end <= start {
			continue
		}
		gateway := strings.TrimSpace(line[start+1 : end])
		if gateway != "" {
			return gateway, true
		}
	}
	return "", false
}

// AddHost 添加路由；target 是主机地址或 CIDR 网段，接口由内核按路由表选择。
func (o *Ops) AddHost(ctx context.Context, target, gateway string) error {
	isNetwork := isNetworkTarget(target)
	if o.platform == "linux" {
		route := target
		if !isNetwork {
			route = target + "/32"
		}
		_, err := o.runner.Run(ctx, o.ipBin, "route", "replace", route, "via", gateway)
		return err
	}
	if isNetwork {
		_, err := o.runner.Run(ctx, o.routeBin, "add", "-net", target, gateway)
		return err
	}
	_, err := o.runner.Run(ctx, o.routeBin, "add", "-host", target, gateway)
	return err
}

// DeleteHost 删除该目标的所有路由；重复路由会循环删除到 not in table。
func (o *Ops) DeleteHost(ctx context.Context, target string) error {
	isNetwork := isNetworkTarget(target)
	for attempt := 0; attempt < 4; attempt++ {
		var err error
		if o.platform == "linux" {
			route := target
			if !isNetwork {
				route = target + "/32"
			}
			_, err = o.runner.Run(ctx, o.ipBin, "route", "del", route)
		} else if isNetwork {
			_, err = o.runner.Run(ctx, o.routeBin, "delete", "-net", target)
		} else {
			_, err = o.runner.Run(ctx, o.routeBin, "delete", "-host", target)
		}
		if err == nil {
			continue
		}
		if isNotFound(err.Error()) {
			return nil
		}
		return err
	}
	return nil
}

func (o *Ops) Table(ctx context.Context) (raw, command string, err error) {
	if o.platform == "linux" && o.netstatBin == "" {
		res, err := o.runner.Run(ctx, o.ipBin, "route", "show")
		return res.Output, res.Command, err
	}
	res, err := o.runner.Run(ctx, o.netstatBin, "-rn")
	return res.Output, res.Command, err
}

func isNetworkTarget(target string) bool {
	return strings.Contains(target, "/")
}

func networkQuery(target string) (string, bool) {
	if !isNetworkTarget(target) {
		return target, false
	}
	_, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return target, true
	}
	return ipnet.IP.String(), true
}

func matchesNetwork(destination, mask, target string) bool {
	if destination == "" || destination == "default" {
		return false
	}
	_, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return false
	}
	destIP := parseDestinationIP(destination)
	if destIP == nil || !bytes.Equal(destIP, ipnet.IP.To4()) {
		return false
	}
	maskIP := net.ParseIP(mask)
	if maskIP == nil {
		return true
	}
	return bytes.Equal(maskIP.To4(), net.IP(ipnet.Mask).To4())
}

func parseDestinationIP(destination string) net.IP {
	if host, _, found := strings.Cut(destination, "/"); found {
		destination = host
	}
	parts := strings.Split(destination, ".")
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	return net.ParseIP(strings.Join(parts, ".")).To4()
}

func hasExactNetwork(out, target string) bool {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && fields[0] == target {
			return true
		}
	}
	return false
}

func parseDarwinRoute(out string) (destination, mask, gateway, iface string, flags []string, ok bool) {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "destination":
			destination = strings.TrimSpace(value)
		case "mask":
			mask = strings.TrimSpace(value)
		case "gateway":
			gateway = strings.TrimSpace(value)
		case "interface":
			iface = strings.TrimSpace(value)
		case "flags":
			flags = parseFlags(value)
		}
	}
	return destination, mask, gateway, iface, flags, len(flags) > 0
}

func ParseDarwinGet(out string) (gateway, iface string, flags []string, ok bool) {
	_, _, gateway, iface, flags, ok = parseDarwinRoute(out)
	return gateway, iface, flags, ok
}

func ParseLinuxGet(out string) (gateway, iface string, ok bool) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", "", false
	}
	for i, field := range fields {
		switch field {
		case "via":
			if i+1 < len(fields) {
				gateway = fields[i+1]
			}
		case "dev":
			if i+1 < len(fields) {
				iface = fields[i+1]
			}
		}
	}
	return gateway, iface, true
}

// ParseNetstat 解析 netstat -rn 的 IPv4 部分（兼容 BSD 与 Linux 列布局）。
func ParseNetstat(raw string) []TableEntry {
	entries := make([]TableEntry, 0, 32)
	inV4 := !strings.Contains(raw, "Internet:")
	var header []string
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Routing tables"), strings.HasPrefix(line, "Kernel IP routing table"):
			continue
		case line == "Internet:":
			inV4 = true
			header = nil
			continue
		case strings.HasPrefix(line, "Internet6:"):
			inV4 = false
			header = nil
			continue
		case strings.HasPrefix(line, "Destination"):
			header = strings.Fields(line)
			continue
		}
		if !inV4 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.Contains(fields[0], ":") {
			continue
		}
		entry := TableEntry{
			Destination: fields[0],
			Gateway:     fieldByHeader(header, fields, "Gateway", 1),
			Flags:       fieldByHeader(header, fields, "Flags", 2),
			Netif:       fieldByHeader(header, fields, "Iface", 3),
			Line:        line,
		}
		if entry.Netif == "" {
			entry.Netif = fieldByHeader(header, fields, "Netif", 3)
		}
		entries = append(entries, entry)
	}
	return entries
}

func ParseIPRoute(raw string) []TableEntry {
	entries := make([]TableEntry, 0, 32)
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		entry := TableEntry{Destination: fields[0], Line: line}
		for i, f := range fields {
			switch f {
			case "via":
				if i+1 < len(fields) {
					entry.Gateway = fields[i+1]
					entry.Flags = "UG"
				}
			case "dev":
				if i+1 < len(fields) {
					entry.Netif = fields[i+1]
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func fieldByHeader(header, fields []string, name string, fallback int) string {
	for i, h := range header {
		if strings.EqualFold(h, name) && i < len(fields) {
			return fields[i]
		}
	}
	if fallback < len(fields) {
		return fields[fallback]
	}
	return ""
}

func parseFlags(value string) []string {
	value = strings.Trim(strings.TrimSpace(value), "<>")
	parts := strings.Split(value, ",")
	flags := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			flags = append(flags, p)
		}
	}
	return flags
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

func hasExactHostRoute(out, host string) bool {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if strings.TrimSuffix(fields[0], "/32") == host {
			return true
		}
	}
	return false
}

func isNotFound(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range []string{
		"not in table",
		"no such process",
		"network is unreachable",
		"cannot assign requested address",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func findBin(name string, candidates ...string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return name
}
