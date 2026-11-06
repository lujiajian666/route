package routecmd

import (
	"strings"
	"testing"

	"be/internal/execx"
)

const darwinGetHost = `   route to: 5.5.2.13
destination: 5.5.2.13
       mask: 255.255.255.255
    gateway: 192.168.100.254
  interface: en7
      flags: <UP,GATEWAY,HOST,DONE,STATIC>
 recvpipe  sendpipe  ssthresh  rtt,msec    rttvar  hopcount      mtu     expire
       0         0         0         0         0         0      1500         0
`

const darwinGetDefault = `   route to: 5.5.2.13
destination: default
       mask: default
    gateway: 192.168.100.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
`

func TestParseDarwinGetHostRoute(t *testing.T) {
	gw, iface, flags, ok := ParseDarwinGet(darwinGetHost)
	if !ok {
		t.Fatal("expected parse success")
	}
	if gw != "192.168.100.254" || iface != "en7" {
		t.Fatalf("unexpected gateway/iface: %q %q", gw, iface)
	}
	if !hasFlag(flags, "HOST") || !hasFlag(flags, "GATEWAY") {
		t.Fatalf("expected HOST and GATEWAY flags, got %v", flags)
	}
}

func TestParseDarwinGetFallsBackToDefault(t *testing.T) {
	gw, _, flags, ok := ParseDarwinGet(darwinGetDefault)
	if !ok || gw != "192.168.100.1" {
		t.Fatalf("unexpected parse: %q %v %v", gw, flags, ok)
	}
	if hasFlag(flags, "HOST") {
		t.Fatal("default route must not be treated as a host route")
	}
}

func TestParseDarwinGetGarbage(t *testing.T) {
	if _, _, _, ok := ParseDarwinGet("route: writing to routing socket: not in table"); ok {
		t.Fatal("expected parse failure")
	}
}

func TestParseLinuxGet(t *testing.T) {
	gw, iface, ok := ParseLinuxGet("5.5.2.13 via 192.168.100.254 dev eth0 src 192.168.100.10 uid 1000")
	if !ok || gw != "192.168.100.254" || iface != "eth0" {
		t.Fatalf("unexpected parse: %q %q %v", gw, iface, ok)
	}
}

func TestParseNetstatDarwin(t *testing.T) {
	raw := `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
default            192.168.100.1      UGScg                 en0
default            192.168.100.254    UGScIg                en7
5.5.2.13           192.168.100.254    UGHS                  en7
127                127.0.0.1          UCS                   lo0
127.0.0.1          127.0.0.1          UH                    lo0
192.168.100        link#14            UCS                   en0      !

Internet6:
Destination                             Gateway                                 Flags               Netif Expire
default                                 fe80::1%en0                             UGcg                en0
`
	entries := ParseNetstat(raw)
	if len(entries) != 6 {
		t.Fatalf("expected 6 IPv4 entries, got %d: %+v", len(entries), entries)
	}
	first := entries[0]
	if first.Destination != "default" || first.Gateway != "192.168.100.1" || first.Flags != "UGScg" || first.Netif != "en0" {
		t.Fatalf("unexpected first entry: %+v", first)
	}
	host := entries[2]
	if host.Destination != "5.5.2.13" || host.Gateway != "192.168.100.254" || !strings.Contains(host.Flags, "H") {
		t.Fatalf("unexpected host entry: %+v", host)
	}
}

func TestParseNetstatLinux(t *testing.T) {
	raw := `Kernel IP routing table
Destination     Gateway         Genmask         Flags   MSS Window  irtt Iface
0.0.0.0         192.168.1.1     0.0.0.0         UG        0 0          0 eth0
5.5.2.13        192.168.100.254 255.255.255.255 UGH       0 0          0 eth0
192.168.1.0     0.0.0.0         255.255.255.0   U         0 0          0 eth0
`
	entries := ParseNetstat(raw)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[1].Destination != "5.5.2.13" || entries[1].Gateway != "192.168.100.254" ||
		entries[1].Flags != "UGH" || entries[1].Netif != "eth0" {
		t.Fatalf("unexpected host entry: %+v", entries[1])
	}
}

func TestParseIPRoute(t *testing.T) {
	raw := `default via 192.168.100.1 dev en0
5.5.2.13 via 192.168.100.254 dev en7
192.168.100.0/24 dev en0 scope link`
	entries := ParseIPRoute(raw)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[1].Gateway != "192.168.100.254" || entries[1].Netif != "en7" {
		t.Fatalf("unexpected entry: %+v", entries[1])
	}
}

func TestHasExactHostRoute(t *testing.T) {
	out := "5.5.2.13 via 192.168.100.254 dev eth0\n"
	if !hasExactHostRoute(out, "5.5.2.13") {
		t.Fatal("expected exact host route")
	}
	if hasExactHostRoute("5.5.2.0/24 via 192.168.100.254 dev eth0\n", "5.5.2.13") {
		t.Fatal("network route must not count as a host route")
	}
}

func dummyRunner() *execx.Runner { return &execx.Runner{} }

func TestCommandPreviews(t *testing.T) {
	ops := New(dummyRunner())
	preview := ops.CommandPreviews("5.5.2.13", "192.168.100.254")
	for _, key := range []string{"get", "add", "delete", "table"} {
		if preview[key] == "" {
			t.Fatalf("missing preview for %s", key)
		}
	}
	if ops.Platform() == "linux" {
		if !strings.Contains(preview["add"], "replace 5.5.2.13/32 via 192.168.100.254") {
			t.Fatalf("unexpected linux add command: %s", preview["add"])
		}
		return
	}
	if !strings.Contains(preview["add"], "add -host 5.5.2.13 192.168.100.254") {
		t.Fatalf("unexpected darwin add command: %s", preview["add"])
	}
	if !strings.Contains(preview["delete"], "delete -host 5.5.2.13") {
		t.Fatalf("unexpected darwin delete command: %s", preview["delete"])
	}
}
