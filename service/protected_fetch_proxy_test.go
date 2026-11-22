package service

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProxyFetchTestClient(t *testing.T, proxyURL string, resolver ssrfResolver) *http.Client {
	t.Helper()
	u, err := url.Parse(proxyURL)
	require.NoError(t, err)
	client := newProtectedFetchHTTPClientWithProxy(resolver, nil, staticProtection(&common.SSRFProtection{ApplyIPFilterForDomain: true}), http.ProxyURL(u))
	client.Timeout = 3 * time.Second
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func TestProtectedFetchProxyPinsTargetAndVerifiesOriginalTLSName(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Host) }))
	t.Cleanup(upstream.Close)
	targets := make(chan string, 4)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targets <- r.Host
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
	client := newProxyFetchTestClient(t, proxy.URL, staticSSRFResolver{"example.com": {{IP: net.ParseIP("1.1.1.1")}}, "wrong.example": {{IP: net.ParseIP("1.1.1.1")}}})
	u, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	client.Transport.(*ssrfProtectedRoundTripper).transportFor(u).TLSClientConfig = &tls.Config{RootCAs: roots}
	resp, err := client.Get("https://example.com/probe")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, "example.com", string(body))
	assert.Equal(t, "1.1.1.1:443", <-targets)
	_, err = client.Get("https://wrong.example/probe")
	require.ErrorContains(t, err, "certificate")
	assert.Equal(t, "1.1.1.1:443", <-targets)
}

func TestProtectedFetchProxyRejectsAllResolvedIPsAndRedirects(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"))
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "http://private.example/blocked", 302)
	}))
	t.Cleanup(proxy.Close)
	resolver := staticSSRFResolver{"mixed.example": {{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP("127.0.0.1")}}, "public.example": {{IP: net.ParseIP("1.1.1.1")}}, "private.example": {{IP: net.ParseIP("10.0.0.1")}}}
	client := newProxyFetchTestClient(t, proxy.URL, resolver)
	_, err := client.Get("http://mixed.example/probe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private IP")
	assert.Zero(t, calls.Load(), "any blocked answer rejects before proxy contact")
	calls.Store(0)
	_, err = client.Get("http://public.example/start")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private IP")
	assert.EqualValues(t, 1, calls.Load(), "redirect target must be checked before a second proxy request")
}

func TestProtectedFetchHTTPSProxyPinsAbsoluteURIAndChecksProxyTLS(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"))
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.RequestURI) }))
	t.Cleanup(proxy.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	for _, valid := range []bool{true, false} {
		proxyURL := proxy.URL
		if !valid {
			proxyURL = strings.Replace(proxyURL, "127.0.0.1", "localhost", 1)
		}
		client := newProxyFetchTestClient(t, proxyURL, staticSSRFResolver{"example.com": {{IP: net.ParseIP("1.1.1.1")}}})
		u, _ := url.Parse(proxyURL)
		base := client.Transport.(*ssrfProtectedRoundTripper).transportFor(u)
		base.TLSClientConfig = &tls.Config{RootCAs: roots}
		base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(proxy.URL, "https://"))
		}
		resp, err := client.Get("http://example.com/a%2Fb?q=one%20two")
		if !valid {
			require.ErrorContains(t, err, "certificate")
			continue
		}
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, "http://1.1.1.1:80/a%2Fb?q=one%20two", string(body))
	}
}

func TestProtectedFetchSOCKSProxyReceivesIPLiteral(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	ratioProxyDNS(t, net.ParseIP("1.1.1.1"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	target := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(conn)
		head := make([]byte, 2)
		if _, err = io.ReadFull(reader, head); err != nil {
			return
		}
		methods := make([]byte, int(head[1]))
		if _, err = io.ReadFull(reader, methods); err != nil {
			return
		}
		conn.Write([]byte{5, 0})
		request := make([]byte, 4)
		if _, err = io.ReadFull(reader, request); err != nil {
			return
		}
		var host string
		switch request[3] {
		case 1:
			ip := make([]byte, 4)
			io.ReadFull(reader, ip)
			host = net.IP(ip).String()
		case 3:
			n, _ := reader.ReadByte()
			name := make([]byte, int(n))
			io.ReadFull(reader, name)
			host = string(name)
		default:
			return
		}
		port := make([]byte, 2)
		if _, err = io.ReadFull(reader, port); err != nil {
			return
		}
		target <- net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(port)))
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
		if _, err = http.ReadRequest(reader); err != nil {
			return
		}
		fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
	}()
	client := newProxyFetchTestClient(t, "socks5://"+listener.Addr().String(), staticSSRFResolver{"example.com": {{IP: net.ParseIP("1.1.1.1")}}})
	resp, err := client.Get("http://example.com/probe")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, "1.1.1.1:80", <-target)
}
