package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

type proxyPolicyResolver struct {
	calls int
	ips   []net.IPAddr
}

func (r *proxyPolicyResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.calls++
	if len(r.ips) == 0 {
		return nil, fmt.Errorf("target only resolvable by proxy")
	}
	return r.ips, nil
}

func TestProtectedFetchProxyHonorsProtectionMode(t *testing.T) { testProxyProtectionModes(t, false) }
func TestUpstreamRatioProxyHonorsProtectionMode(t *testing.T)  { testProxyProtectionModes(t, true) }

func testProxyProtectionModes(t *testing.T, ratio bool) {
	for _, mode := range []string{"disabled", "domain-filter-off", "full"} {
		for _, target := range []string{"proxy-only", "private-resolution", "public-resolution", "mixed-resolution", "private-literal", "public-literal", "blocked-domain", "blocked-port", "tls"} {
			t.Run(mode+"/"+target, func(t *testing.T) {
				configureSSRFTestFetchSetting(t)
				setting := system_setting.GetFetchSetting()
				setting.EnableSSRFProtection = mode != "disabled"
				setting.ApplyIPFilterForDomain = mode == "full"
				setting.DomainList = []string{"blocked.example"}
				resolver := &proxyPolicyResolver{}
				rawURL := "http://example.com/a%2Fb?q=one%20two"
				expectedURI := rawURL
				expectedDNS := 0
				reject := false
				switch target {
				case "private-resolution":
					resolver.ips = []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}
				case "public-resolution", "tls":
					resolver.ips = []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}
				case "mixed-resolution":
					resolver.ips = []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP("10.0.0.5")}}
				case "private-literal":
					rawURL = "http://10.0.0.5/a%2Fb?q=one%20two"
					expectedURI = rawURL
					reject = mode != "disabled"
				case "public-literal":
					rawURL = "http://1.1.1.1/a%2Fb?q=one%20two"
					expectedURI = rawURL
					if mode != "disabled" {
						expectedURI = "http://1.1.1.1:80/a%2Fb?q=one%20two"
					}
				case "blocked-domain":
					rawURL = "http://blocked.example/a%2Fb?q=one%20two"
					expectedURI = rawURL
					reject = mode != "disabled"
				case "blocked-port":
					rawURL = "http://example.com:81/a%2Fb?q=one%20two"
					expectedURI = rawURL
					reject = mode != "disabled"
				}
				if mode == "full" && (target == "proxy-only" || target == "private-resolution" || target == "public-resolution" || target == "mixed-resolution" || target == "tls") {
					expectedDNS = 1
					reject = target != "public-resolution" && target != "tls"
					expectedURI = "http://1.1.1.1:80/a%2Fb?q=one%20two"
				}
				upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Host) }))
				t.Cleanup(upstream.Close)
				var calls atomic.Int32
				seen := make(chan string, 1)
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					seen <- r.RequestURI
					if r.Method != http.MethodConnect {
						fmt.Fprint(w, "ok")
						return
					}
					remote, err := net.Dial("tcp", strings.TrimPrefix(upstream.URL, "https://"))
					if err != nil {
						http.Error(w, err.Error(), 502)
						return
					}
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						remote.Close()
						return
					}
					defer conn.Close()
					defer remote.Close()
					fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					go func() { io.Copy(remote, conn); remote.Close() }()
					io.Copy(conn, remote)
				}))
				t.Cleanup(proxy.Close)
				proxyURL, err := url.Parse(proxy.URL)
				require.NoError(t, err)
				// 目标策略照常执行，只有管理员配置的代理地址映射到本地桩。
				dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, proxyURL.Host)
				}
				var client *http.Client
				var base *http.Transport
				if ratio {
					client, err = newUpstreamRatioHTTPClient(proxy.URL)
					require.NoError(t, err)
					client.Transport.(*pinnedProxyRoundTripper).resolver = resolver
					base = ratioProxyBase(client)
					base.DialContext = dial
				} else {
					client = newProtectedFetchHTTPClientWithProxy(resolver, dial, currentFetchProtection, http.ProxyURL(proxyURL))
					base = client.Transport.(*ssrfProtectedRoundTripper).transportFor(proxyURL)
				}
				t.Cleanup(client.CloseIdleConnections)
				roots := x509.NewCertPool()
				roots.AddCert(upstream.Certificate())
				base.TLSClientConfig = &tls.Config{RootCAs: roots}
				if target == "tls" {
					rawURL = "https://example.com/probe"
					expectedURI = "example.com:443"
					if mode == "full" {
						expectedURI = "1.1.1.1:443"
					}
				}
				resp, err := client.Get(rawURL)
				if reject {
					require.Error(t, err)
					require.Zero(t, calls.Load())
				} else {
					require.NoError(t, err)
					body, readErr := io.ReadAll(resp.Body)
					resp.Body.Close()
					require.NoError(t, readErr)
					require.EqualValues(t, 1, calls.Load())
					require.Equal(t, expectedURI, <-seen)
					if target == "tls" {
						require.Equal(t, "example.com", string(body), "TLS certificate and Host retain the original hostname")
					}
				}
				require.Equal(t, expectedDNS, resolver.calls, "only locally checked domain IPs require local DNS")
			})
		}
	}
}
