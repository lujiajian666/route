package routecmd

import (
	"strings"
	"testing"
)

const darwinGetHost = `   route to: 5.5.2.13
destination: 5.5.2.13
       mask: 255.255.255.255
    gateway: 192.168.100.254
  interface: en7
      flags: <UP,GATEWAY,HOST,DONE,STATIC>
`

const darwinGetDefault = `   route to: 5.5.2.13
destination: default
       mask: default
    gateway: 192.168.100.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
`

func TestParseDarwinGet(t *testing.T) {
	gw, iface, flags, ok := ParseDarwinGet(darwinGetHost)
	if !ok || gw != "192.168.100.254" || iface != "en7" {
		t.Fatalf("unexpected parse: %q %q %v %v", gw, iface, flags, ok)
	}
	if !hasFlag(flags, "HOST") || !hasFlag(flags, "GATEWAY") {
		t.Fatalf("expected HOST/GATEWAY flags, got %v", flags)
	}
}

func TestParseDarwinGetDefault(t *testing.T) {
	gw, _, flags, ok := ParseDarwinGet(darwinGetDefault)
	if !ok || gw != "192.168.100.1" {
		t.Fatalf("unexpected parse: %q %v %v", gw, flags, ok)
	}
	if hasFlag(flags, "HOST") {
		t.Fatal("default 路由不应被当作主机路由")
	}
}

func TestParseDarwinGetGarbage(t *testing.T) {
	if _, _, _, ok := ParseDarwinGet("route: writing to routing socket: not in table"); ok {
		t.Fatal("expected parse failure")
	}
}

func TestParseLinuxGet(t *testing.T) {
	gw, iface, ok := ParseLinuxGet("5.5.2.13 via 192.168.100.254 dev eth0 src 192.168.100.10")
	if !ok || gw != "192.168.100.254" || iface != "eth0" {
		t.Fatalf("unexpected parse: %q %q %v", gw, iface, ok)
	}
}

func TestParseNetstatDarwin(t *testing.T) {
	raw := `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
default            192.168.100.1      UGScg                 en0
5.5.2.13           192.168.100.254    UGHS                  en7
127                127.0.0.1          UCS                   lo0

Internet6:
Destination                             Gateway                                 Flags               Netif Expire
default                                 fe80::1%en0                             UGcg                en0
`
	entries := ParseNetstat(raw)
	if len(entries) != 3 {
		t.Fatalf("expected 3 IPv4 entries, got %d: %+v", len(entries), entries)
	}
	host := entries[1]
	if host.Destination != "5.5.2.13" || host.Gateway != "192.168.100.254" ||
		!strings.Contains(host.Flags, "H") || host.Netif != "en7" {
		t.Fatalf("unexpected host entry: %+v", host)
	}
}

func TestParseNetstatLinux(t *testing.T) {
	raw := `Kernel IP routing table
Destination     Gateway         Genmask         Flags   MSS Window  irtt Iface
0.0.0.0         192.168.1.1     0.0.0.0         UG        0 0          0 eth0
5.5.2.13        192.168.100.254 255.255.255.255 UGH       0 0          0 eth0
`
	entries := ParseNetstat(raw)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[1].Gateway != "192.168.100.254" || entries[1].Netif != "eth0" {
		t.Fatalf("unexpected entry: %+v", entries[1])
	}
}

func TestParseIPRoute(t *testing.T) {
	entries := ParseIPRoute("default via 192.168.100.1 dev en0\n5.5.2.13 via 192.168.100.254 dev en7\n")
	if len(entries) != 2 || entries[1].Gateway != "192.168.100.254" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestHasExactHostRoute(t *testing.T) {
	if !hasExactHostRoute("5.5.2.13 via 192.168.100.254 dev eth0\n", "5.5.2.13") {
		t.Fatal("expected exact host route")
	}
	if hasExactHostRoute("5.5.2.0/24 via 192.168.100.254 dev eth0\n", "5.5.2.13") {
		t.Fatal("network route must not count")
	}
}
