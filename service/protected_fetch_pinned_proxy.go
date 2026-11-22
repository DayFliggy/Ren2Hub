package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// pinProtectedFetchTarget 固定本地校验过的解析结果，避免代理二次解析。
// 仅在策略要求检查目标 IP 时解析；其余情况保留代理原生解析。
func pinProtectedFetchTarget(req *http.Request, resolver ssrfResolver, getProtection func() (*common.SSRFProtection, bool, error)) (*http.Request, error) {
	host := req.URL.Hostname()
	if strings.Contains(host, "%") {
		return nil, errUpstreamRatioZonedHost
	}
	if host == "" || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return nil, fmt.Errorf("invalid fetch URL")
	}
	portText := req.URL.Port()
	if portText == "" {
		portText = "80"
		if req.URL.Scheme == "https" {
			portText = "443"
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %s", portText)
	}
	protection, enabled, err := getProtection()
	if err != nil {
		return nil, err
	}
	if enabled {
		if err := protection.ValidateNetworkTarget(host, port); err != nil {
			return nil, err
		}
	}
	if !enabled || (net.ParseIP(host) == nil && !protection.ApplyIPFilterForDomain) {
		return req, nil
	}
	ips := []net.IPAddr{{IP: net.ParseIP(host)}}
	if ips[0].IP == nil {
		ips, err = resolver.LookupIPAddr(req.Context(), host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("DNS resolution for %s returned no IP addresses", host)
	}
	for _, addr := range ips {
		if addr.Zone != "" || addr.IP == nil {
			return nil, fmt.Errorf("invalid resolved IP address")
		}
		if err := protection.ValidateResolvedIP(host, addr.IP); err != nil {
			return nil, err
		}
	}
	pinned := req.Clone(req.Context())
	if pinned.Host == "" {
		pinned.Host = req.URL.Host
	}
	pinned.URL.Host = net.JoinHostPort(ips[0].IP.String(), portText)
	return pinned, nil
}

// 每个请求独立的 transport 使不同目标的证书名与连接不会交叉复用。
type pinnedProxyRoundTripper struct {
	base          *http.Transport
	proxy         *url.URL
	resolver      ssrfResolver
	getProtection func() (*common.SSRFProtection, bool, error)
}

func (t *pinnedProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	pinned, err := pinProtectedFetchTarget(req, t.resolver, t.getProtection)
	if err != nil {
		return nil, err
	}
	transport := t.base.Clone()
	transport.DisableKeepAlives = true
	transport.Proxy = http.ProxyURL(t.proxy)
	if pinned == req {
		return transport.RoundTrip(req)
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.ServerName = req.URL.Hostname()
	// HTTPS 代理的外层 TLS 必须校验代理自身，隧道内 TLS 校验目标。
	if t.proxy.Scheme == "https" {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := transport.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			config := transport.TLSClientConfig.Clone()
			config.ServerName = t.proxy.Hostname()
			secured := tls.Client(conn, config)
			if err := secured.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			return secured, nil
		}
	}
	if pinned.URL.Scheme == "http" && (t.proxy.Scheme == "http" || t.proxy.Scheme == "https") {
		path := pinned.URL.EscapedPath()
		if path == "" {
			path = "/"
		}
		// Request.WriteProxy 否则会把 Host 头写进绝对 URI，重新引入代理端解析。
		pinned.URL.Opaque = "//" + pinned.URL.Host + path
	}
	return transport.RoundTrip(pinned)
}

func (t *pinnedProxyRoundTripper) CloseIdleConnections() { t.base.CloseIdleConnections() }
