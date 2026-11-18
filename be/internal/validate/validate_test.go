package validate

import "testing"

func TestTarget(t *testing.T) {
	cases := []struct {
		raw       string
		target    string
		isNetwork bool
	}{
		{"5.5.2.13", "5.5.2.13", false},
		{" 5.5.2.13 ", "5.5.2.13", false},
		{"5.5.2.13/32", "5.5.2.13", false},
		{"5.5.2.0/24", "5.5.2.0/24", true},
		{"5.5.2.11/24", "5.5.2.0/24", true},
		{"10.0.0.0/8", "10.0.0.0/8", true},
	}
	for _, tc := range cases {
		target, isNetwork, err := Target(tc.raw)
		if err != nil {
			t.Fatalf("Target(%q) error: %v", tc.raw, err)
		}
		if target != tc.target || isNetwork != tc.isNetwork {
			t.Fatalf("Target(%q) = %q,%v; want %q,%v", tc.raw, target, isNetwork, tc.target, tc.isNetwork)
		}
	}
}

func TestTargetInvalid(t *testing.T) {
	for _, raw := range []string{
		"", "5.5.2.999", "example.com", "0.0.0.0", "224.0.0.1",
		"5.5.2.0/0", "5.5.2.0/33", "5.5.2.0/abc", "2001:db8::1",
	} {
		if _, _, err := Target(raw); err == nil {
			t.Fatalf("Target(%q) expected error", raw)
		}
	}
}

func TestTargetGateway(t *testing.T) {
	cases := []struct{ target, gateway string }{
		{"5.5.2.13", "192.168.100.254"},
		{"5.5.2.0/24", "192.168.100.254"},
		{" 5.5.2.11/24 ", " 192.168.100.254 "},
	}
	for _, tc := range cases {
		target, gateway, _, err := TargetGateway(tc.target, tc.gateway)
		if err != nil {
			t.Fatalf("TargetGateway(%q, %q) error: %v", tc.target, tc.gateway, err)
		}
		if target == "" || gateway == "" {
			t.Fatalf("TargetGateway(%q, %q) returned empty values", tc.target, tc.gateway)
		}
	}
}

func TestTargetGatewayInvalid(t *testing.T) {
	cases := []struct{ name, target, gateway string }{
		{"empty target", "", "192.168.100.254"},
		{"bad target", "5.5.2.999", "192.168.100.254"},
		{"hostname", "example.com", "192.168.100.254"},
		{"empty gateway", "5.5.2.13", ""},
		{"bad gateway", "5.5.2.13", "gateway.local"},
		{"ipv6", "2001:db8::1", "192.168.100.254"},
		{"unspecified target", "0.0.0.0", "192.168.100.254"},
		{"multicast target", "224.0.0.1", "192.168.100.254"},
		{"unspecified gateway", "5.5.2.13", "0.0.0.0"},
		{"same host gateway", "5.5.2.13", "5.5.2.13"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := TargetGateway(tc.target, tc.gateway); err == nil {
				t.Fatalf("TargetGateway(%q, %q) expected error", tc.target, tc.gateway)
			}
		})
	}
}
