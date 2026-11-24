package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	upstreamRatioRequestTimeout   = 10 * time.Second
	upstreamRatioSmallBodyLimit   = 64 * 1024
	upstreamRatioPricingBodyLimit = 4 * 1024 * 1024
)

var (
	errUpstreamRatioBodyTooLarge = errors.New("response too large")
	errUpstreamRatioZonedHost    = errors.New("ipv6 zone host is not allowed")
	errUpstreamRatioSetting      = model.ErrUpstreamRatioSetting
)

// 官方 API 域名不会提供 /v1/sub2api/billing 或 /api/usage/token/，直接判为不支持，
// 避免拿渠道 Key 周期性请求官方域的不存在路径。前半部分与 Sub2API 列表一致，后半部分补国产厂商。
var upstreamRatioOfficialDomains = []string{
	"anthropic.com", "googleapis.com", "x.ai", "grok.com", "openai.com", "ollama.com",
	"moonshot.cn", "kimi.com", "bigmodel.cn", "deepseek.com",
	"moonshot.ai", "z.ai", "openrouter.ai", "aliyuncs.com", "baidubce.com", "xf-yun.com",
	"tencentcloudapi.com", "minimax.chat", "minimaxi.com", "minimax.io", "siliconflow.cn",
	"siliconflow.com", "volces.com", "volcengineapi.com", "lingyiwanwu.com",
}

func isOfficialUpstreamAPI(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, domain := range upstreamRatioOfficialDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

type upstreamRatioTarget = model.UpstreamRatioTarget

func newUpstreamRatioTarget(ch *model.Channel) (upstreamRatioTarget, error) {
	return model.NewUpstreamRatioTarget(ch)
}
func upstreamProbeBaseURL(raw string) (string, error)   { return model.UpstreamProbeBaseURL(raw) }
func upstreamRatioFingerprint(ch *model.Channel) string { return model.UpstreamRatioFingerprint(ch) }

// newUpstreamRatioHTTPClient 每次探测新建，不复用中转用的共享 client：拒绝重定向。
// 所有拨号（直连目标，或代理地址）都经过 SSRF 受保护拨号器，按解析后的 IP 检查。
// 代理目标按同一防护配置决定是否固定 IP，TLS 与 Host 保留原主机身份。
func newUpstreamRatioHTTPClient(proxy string) (*http.Client, error) {
	dialer := &protectedFetchDialer{
		resolver:      net.DefaultResolver,
		dialContext:   (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		getProtection: currentFetchProtection,
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		// 共享 SSRF 检查把 fe80::1%eth0 这类带 zone 的 IPv6 字面量当作域名，未开启域名 IP 过滤时
		// 会直接放行。探测目标和代理都不需要 zone，这里统一拒绝。
		if host, _, err := net.SplitHostPort(addr); err == nil && strings.Contains(host, "%") {
			return nil, errUpstreamRatioZonedHost
		}
		return dialer.DialContext(ctx, network, addr)
	}}
	var roundTripper http.RoundTripper = transport
	if proxy != "" {
		proxyURL, _, err := common.ParseProxyURLRuntime(proxy)
		if err != nil {
			return nil, err
		}
		roundTripper = &pinnedProxyRoundTripper{base: transport, proxy: proxyURL, resolver: net.DefaultResolver, getProtection: currentFetchProtection}
	}
	return &http.Client{
		Transport:     roundTripper,
		Timeout:       upstreamRatioRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

type upstreamRatioResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func upstreamRatioGet(ctx context.Context, client *http.Client, rawURL, key string, limit int64) (*upstreamRatioResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamRatioRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errUpstreamRatioBodyTooLarge
	}
	return &upstreamRatioResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// probeOutcome 是一次 HTTP 探测的分类结果。Reason 只用固定代码，不含上游原文或 Key。
type probeOutcome struct {
	Status     string
	Reason     string
	HTTPStatus int
	RetryAfter time.Duration
}

func (o probeOutcome) ok() bool { return o.Status == model.UpstreamRatioStatusOK }

func classifyUpstreamRatioResponse(resp *upstreamRatioResponse, err error, now time.Time) probeOutcome {
	if errors.Is(err, errUpstreamRatioBodyTooLarge) {
		return probeOutcome{Status: model.UpstreamRatioStatusFailed, Reason: "response_too_large"}
	}
	if err != nil {
		return probeOutcome{Status: model.UpstreamRatioStatusFailed, Reason: "request_failed"}
	}
	outcome := probeOutcome{HTTPStatus: resp.StatusCode, RetryAfter: upstreamRatioRetryAfter(resp.Header, now)}
	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		outcome.Status = model.UpstreamRatioStatusOK
	case code == http.StatusNotFound || code == http.StatusMethodNotAllowed:
		outcome.Status, outcome.Reason = model.UpstreamRatioStatusUnsupported, "unsupported"
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		outcome.Status, outcome.Reason = model.UpstreamRatioStatusAuthFailed, "auth_failed"
	case code == http.StatusTooManyRequests:
		outcome.Status, outcome.Reason = model.UpstreamRatioStatusRateLimited, "rate_limited"
	case code >= 300 && code < 400:
		outcome.Status, outcome.Reason = model.UpstreamRatioStatusFailed, "redirect"
	default:
		outcome.Status, outcome.Reason = model.UpstreamRatioStatusFailed, fmt.Sprintf("http_%d", code)
	}
	return outcome
}
