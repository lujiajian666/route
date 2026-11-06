package validate

import "testing"

func TestHostGatewayValid(t *testing.T) {
	cases := []struct{ host, gateway string }{
		{"5.5.2.13", "192.168.100.254"},
		{" 5.5.2.13 ", " 192.168.100.254 "},
		{"10.0.0.1", "10.0.0.254"},
	}
	for _, tc := range cases {
		host, gw, err := HostGateway(tc.host, tc.gateway)
		if err != nil {
			t.Fatalf("HostGateway(%q, %q) unexpected error: %v", tc.host, tc.gateway, err)
		}
		if host == "" || gw == "" {
			t.Fatalf("HostGateway(%q, %q) returned empty values", tc.host, tc.gateway)
		}
	}
}

func TestHostGatewayInvalid(t *testing.T) {
	cases := []struct{ name, host, gateway string }{
		{"empty host", "", "192.168.100.254"},
		{"bad host", "5.5.2.999", "192.168.100.254"},
		{"hostname", "example.com", "192.168.100.254"},
		{"empty gateway", "5.5.2.13", ""},
		{"bad gateway", "5.5.2.13", "gateway.local"},
		{"ipv6", "2001:db8::1", "192.168.100.254"},
		{"unspecified host", "0.0.0.0", "192.168.100.254"},
		{"multicast host", "224.0.0.1", "192.168.100.254"},
		{"unspecified gateway", "5.5.2.13", "0.0.0.0"},
		{"same host gateway", "5.5.2.13", "5.5.2.13"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := HostGateway(tc.host, tc.gateway); err == nil {
				t.Fatalf("HostGateway(%q, %q) expected error", tc.host, tc.gateway)
			}
		})
	}
}
