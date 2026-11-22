package service

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

// 每个查询均由本地 DNS 桩回答，不访问外部解析器。
func ratioProxyDNS(t *testing.T, ips ...net.IP) {
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			var size [2]byte
			if _, err := io.ReadFull(b, size[:]); err != nil {
				return
			}
			data := make([]byte, int(size[0])<<8|int(size[1]))
			if _, err := io.ReadFull(b, data); err != nil {
				return
			}
			var msg dnsmessage.Message
			if msg.Unpack(data) != nil {
				return
			}
			msg.Header.Response = true
			msg.Header.RecursionAvailable = true
			for _, q := range msg.Questions {
				for _, ip := range ips {
					h := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 1}
					if q.Type == dnsmessage.TypeA && ip.To4() != nil {
						var v [4]byte
						copy(v[:], ip.To4())
						msg.Answers = append(msg.Answers, dnsmessage.Resource{Header: h, Body: &dnsmessage.AResource{A: v}})
					}
				}
			}
			reply, err := msg.Pack()
			if err != nil {
				return
			}
			b.Write(append([]byte{byte(len(reply) >> 8), byte(len(reply))}, reply...))
		}()
		return a, nil
	}}
	t.Cleanup(func() { net.DefaultResolver = old })
}

func (t *pinnedProxyRoundTripper) transportBase() *http.Transport { return t.base }

func ratioProxyBase(client *http.Client) *http.Transport {
	if wrapper, ok := client.Transport.(interface{ transportBase() *http.Transport }); ok {
		return wrapper.transportBase()
	}
	return client.Transport.(*http.Transport)
}

func TestUpstreamRatioProxyPinsResolvedTarget(t *testing.T) {
	setupUpstreamRatioTest(t)
	fetch := system_setting.GetFetchSetting()
	fetch.EnableSSRFProtection = true
	fetch.AllowPrivateIp = false
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Host) }))
	defer upstream.Close()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	targets := make(chan string, 4)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targets <- r.Host
		if r.Method != http.MethodConnect {
			fmt.Fprintf(w, "%s|%s", r.URL.Host, r.Host)
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
		fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { defer remote.Close(); io.Copy(remote, conn) }()
		defer conn.Close()
		io.Copy(conn, remote)
	}))
	defer proxy.Close()
	client, err := newUpstreamRatioHTTPClient(proxy.URL)
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	ratioProxyBase(client).TLSClientConfig = &tls.Config{RootCAs: roots}
	// 仅代理端使用本地桩；目标仍经过开启的 SSRF 校验。
	ratioProxyBase(client).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(proxy.URL, "http://"))
	}
	resp, err := upstreamRatioGet(context.Background(), client, "https://example.com/probe", "", 1024)
	require.NoError(t, err, "证书仅包含 example.com，必须按原主机校验")
	require.Equal(t, "example.com", string(resp.Body))
	require.Equal(t, "1.1.1.1:443", <-targets)
	_, err = upstreamRatioGet(context.Background(), client, "https://wrong.example/probe", "", 1024)
	require.ErrorContains(t, err, "certificate")
	require.Equal(t, "1.1.1.1:443", <-targets)
	ratioProxyBase(client).DialContext = func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			reader := bufio.NewReader(b)
			var lines []string
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == "\r\n" {
					break
				}
				lines = append(lines, line)
			}
			body := strings.Join(lines, "")
			fmt.Fprintf(b, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		}()
		return a, nil
	}
	resp, err = upstreamRatioGet(context.Background(), client, "http://example.com:8080/a%2Fb?q=one%20two", "", 4096)
	require.NoError(t, err)
	require.Contains(t, string(resp.Body), "GET http://1.1.1.1:8080/a%2Fb?q=one%20two HTTP/1.1\r\n")
	require.Contains(t, string(resp.Body), "Host: example.com:8080\r\n")
}

func TestUpstreamRatioProxyRejectsMixedPrivateResolution(t *testing.T) {
	setupUpstreamRatioTest(t)
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"), net.ParseIP("127.0.0.1"))
	fetch := system_setting.GetFetchSetting()
	fetch.EnableSSRFProtection = true
	fetch.AllowPrivateIp = false
	// 即使关闭域名 IP 过滤，固定解析也不能把私网地址交给代理。
	fetch.ApplyIPFilterForDomain = false
	fetch.DomainFilterMode = false
	fetch.IpFilterMode = false
	client, err := newUpstreamRatioHTTPClient("http://8.8.8.8:8080")
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	ratioProxyBase(client).DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("unexpected proxy dial")
	}
	_, err = upstreamRatioGet(context.Background(), client, "http://example.com/probe", "", 1024)
	require.ErrorContains(t, err, "private IP")
}

func TestUpstreamRatioHTTPSProxyVerifiesProxyIdentity(t *testing.T) {
	setupUpstreamRatioTest(t)
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"))
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.URL.Host) }))
	defer proxy.Close()
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	for _, valid := range []bool{true, false} {
		proxyURL := proxy.URL
		if !valid {
			proxyURL = strings.Replace(proxyURL, "127.0.0.1", "localhost", 1)
		}
		client, err := newUpstreamRatioHTTPClient(proxyURL)
		require.NoError(t, err)
		base := ratioProxyBase(client)
		base.TLSClientConfig = &tls.Config{RootCAs: roots}
		base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(proxy.URL, "https://"))
		}
		resp, err := upstreamRatioGet(context.Background(), client, "http://example.com/probe", "", 1024)
		client.CloseIdleConnections()
		if valid {
			require.NoError(t, err)
			require.Equal(t, "1.1.1.1:80", string(resp.Body))
		} else {
			require.ErrorContains(t, err, "certificate")
		}
	}
}
