package common

import "testing"

func TestSSRFProtectionRejectsZonedIPv6BeforeDomainRules(t *testing.T) {
	p := &SSRFProtection{AllowPrivateIp: true, ApplyIPFilterForDomain: false}
	for _, host := range []string{"fe80::1%eth0", "::1%lo", "2001:4860::1%eth0"} {
		if err := p.ValidateNetworkTarget(host, 80); err == nil {
			t.Errorf("zone address %q must be rejected", host)
		}
	}
	if err := p.ValidateURL("http://[fe80::1%25eth0]/"); err == nil {
		t.Error("URL zone must use the same rejection rule")
	}
}
