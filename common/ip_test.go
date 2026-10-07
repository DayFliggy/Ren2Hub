package common

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Token allowlists call IsIpInCIDRList with net.ParseIP(c.ClientIP()). A
// dual-stack listener may report an IPv4 client as ::ffff:a.b.c.d, so both
// spellings must match the same IPv4 and IPv4-mapped rules (AxonHub 8dfb609).
func TestIsIpInCIDRListNormalizesIPv4MappedAddresses(t *testing.T) {
	for _, test := range []struct {
		name   string
		client string
		rules  []string
		want   bool
	}{
		{"mapped client, IPv4 single rule", "::ffff:192.0.2.10", []string{"192.0.2.10"}, true},
		{"mapped client, IPv4 CIDR rule", "::ffff:192.0.2.10", []string{"192.0.2.0/24"}, true},
		{"IPv4 client, mapped single rule", "192.0.2.10", []string{"::ffff:192.0.2.10"}, true},
		{"IPv4 client, whole mapped space", "192.0.2.10", []string{"::ffff:0:0/96"}, true},
		{"mapped client, whole mapped space", "::ffff:192.0.2.10", []string{"::ffff:0:0/96"}, true},
		{"IPv4 client, mapped /120 rule", "192.0.2.10", []string{"::ffff:192.0.2.0/120"}, true},
		{"mapped client outside every rule", "::ffff:198.51.100.1", []string{"192.0.2.10", "192.0.2.0/24", "::ffff:192.0.2.0/120"}, false},
		{"IPv4 client outside mapped /120 rule", "198.51.100.1", []string{"::ffff:192.0.2.0/120"}, false},
		{"native IPv6 client, mapped space", "2001:db8::1", []string{"::ffff:0:0/96"}, false},
		{"IPv4 client, native IPv6 rule", "192.0.2.10", []string{"2001:db8::/32"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := net.ParseIP(test.client)
			require.NotNil(t, client)
			assert.Equal(t, test.want, IsIpInCIDRList(client, test.rules))
		})
	}
}
