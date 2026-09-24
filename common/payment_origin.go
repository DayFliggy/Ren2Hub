package common

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// NormalizePaymentOrigin keeps callback and return addresses on an exact HTTP
// origin. Payment configuration errors must never echo a credential-bearing URL.
func NormalizePaymentOrigin(raw string) (string, error) {
	invalid := errors.New("invalid payment origin")
	if strings.ContainsAny(raw, "?#\r\n\x00") {
		return "", invalid
	}
	origin, err := NormalizeOrigin(raw)
	if err != nil {
		return "", invalid
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.HasSuffix(parsed.Host, ":") {
		return "", invalid
	}
	if (strings.Contains(parsed.Hostname(), ":") || strings.ContainsAny(parsed.Host, "[]")) && net.ParseIP(parsed.Hostname()) == nil {
		return "", invalid
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", invalid
		}
	}
	return origin, nil
}
