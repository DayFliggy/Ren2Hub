package service

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sub2apiBody(group, resolved, effective string, extra string) string {
	return `{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token",` +
		`"group_rate_multiplier":` + group + `,"resolved_rate_multiplier":` + resolved +
		`,"effective_rate_multiplier":` + effective + `,"observed_at":"2026-10-29T03:00:00Z"` + extra + `}`
}

func TestParseSub2APIBillingResponse(t *testing.T) {
	peak := `,"peak_rate_enabled":true,"peak_start":"10:00","peak_end":"12:00","timezone":"Asia/Shanghai","peak_rate_multiplier":2,"applied_peak_multiplier":2`
	billing, err := parseSub2APIBillingResponse([]byte(sub2apiBody("1.5", "0.8", "1.6", `,"user_rate_multiplier":0.8`+peak)))
	require.NoError(t, err)
	assert.Equal(t, 0.8, billing.ResolvedRate)
	// 03:00Z 是上海 11:00，在高峰内；13:00 上海时间不在高峰内。
	rate, ok := billing.effectiveRateAt(time.Date(2026, 10, 29, 5, 0, 0, 0, time.UTC))
	require.True(t, ok)
	assert.Equal(t, 0.8, rate)

	_, err = parseSub2APIBillingResponse([]byte(sub2apiBody("0", "0", "0", `,"peak_rate_enabled":false`)))
	assert.NoError(t, err, "upstream may declare a free group")

	for name, body := range map[string]string{
		"effective mismatch":         sub2apiBody("1", "1", "1.2", `,"peak_rate_enabled":false`),
		"resolved ignores user rate": sub2apiBody("1", "1", "1", `,"user_rate_multiplier":2,"peak_rate_enabled":false`),
		"peak start equals end":      sub2apiBody("1", "1", "1", `,"peak_rate_enabled":true,"peak_start":"10:00","peak_end":"10:00","timezone":"UTC","peak_rate_multiplier":2,"applied_peak_multiplier":1`),
		"negative group":             sub2apiBody("-1", "-1", "-1", `,"peak_rate_enabled":false`),
		"wrong object":               strings.Replace(sub2apiBody("1", "1", "1", `,"peak_rate_enabled":false`), "sub2api.key_billing", "other", 1),
	} {
		_, err := parseSub2APIBillingResponse([]byte(body))
		assert.Error(t, err, name)
	}
}

func TestUpstreamRatioProbeDelays(t *testing.T) {
	for range 50 {
		delay := nextUpstreamRatioProbeDelay(30, 0)
		assert.GreaterOrEqual(t, delay, 25*time.Minute)
		assert.LessOrEqual(t, delay, 35*time.Minute)
		assert.Equal(t, upstreamRatioMaxDelay, unsupportedUpstreamRatioProbeDelay(24*60, 0))
		unsupported := unsupportedUpstreamRatioProbeDelay(30, 0)
		assert.GreaterOrEqual(t, unsupported, 200*time.Minute)
		assert.LessOrEqual(t, unsupported, 280*time.Minute)
	}
	assert.Equal(t, 2*time.Hour, nextUpstreamRatioProbeDelay(30, 2*time.Hour), "Retry-After overrides the local interval")
	assert.Equal(t, upstreamRatioMaxDelay, nextUpstreamRatioProbeDelay(30, 48*time.Hour))
}

func TestUpstreamProbeBaseURLKeepsDeploymentPrefix(t *testing.T) {
	for raw, want := range map[string]string{
		"https://relay.example.com/v1":            "https://relay.example.com",
		"https://relay.example.com/proxy/v1/":     "https://relay.example.com/proxy",
		"https://relay.example.com/gemini/v1beta": "https://relay.example.com/gemini",
		"https://relay.example.com/api":           "https://relay.example.com/api",
	} {
		got, err := upstreamProbeBaseURL(raw)
		require.NoError(t, err)
		assert.Equal(t, want, got, raw)
	}
	_, err := upstreamProbeBaseURL("ftp://relay.example.com")
	assert.Error(t, err)
}

func setupUpstreamRatioTest(t *testing.T) *gorm.DB {
	t.Helper()
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelUpstreamRatio{}))
	model.DB = db
	fetch := system_setting.GetFetchSetting()
	original := *fetch
	fetch.EnableSSRFProtection = false
	t.Cleanup(func() {
		*fetch = original
		model.DB = previous
		require.NoError(t, sqlDB.Close())
	})
	return db
}

type fakeUpstream struct {
	tokenName string
	pricing   string
	billing   string
	hits      atomic.Int32
}

const fakeUpstreamKey = "sk-upstream-ratio-test"

func (f *fakeUpstream) serve(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		authorized := r.Header.Get("Authorization") == "Bearer "+fakeUpstreamKey
		switch {
		case r.URL.Path == "/v1/sub2api/billing" && f.billing != "":
			if !authorized {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(f.billing))
		case r.URL.Path == "/api/usage/token/" && f.tokenName != "":
			if !authorized {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"code":true,"message":"ok","data":{"object":"token_usage","name":"` + f.tokenName + `"}}`))
		case r.URL.Path == "/api/pricing" && f.pricing != "":
			_, _ = w.Write([]byte(f.pricing))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

const fakePricing = `{"success":true,"group_ratio":{"default":1,"vip":1.5,"auto":1},"usable_group":{"default":"默认","vip":"VIP","auto":"自动"},"auto_groups":["default","vip"]}`

func createUpstreamRatioChannel(t *testing.T, db *gorm.DB, id, channelType int, baseURL string) *model.Channel {
	t.Helper()
	channel := &model.Channel{Id: id, Type: channelType, Key: fakeUpstreamKey, Name: "upstream", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(channel).Error)
	return channel
}

func TestNewAPIUpstreamRatioNameMatchAndManualMapping(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	upstream := &fakeUpstream{tokenName: "vip", pricing: fakePricing}
	server := upstream.serve(t)
	createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeNewAPI, server.URL+"/v1")

	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusOK, view.Status)
	assert.Equal(t, "1.5", view.Ratio)
	assert.Equal(t, model.UpstreamRatioMappingNameMatch, view.Mapping)
	assert.False(t, view.Stale)
	assert.Len(t, view.Groups, 3)

	// 名称只在严格相等时匹配；大小写不同进入待关联。
	upstream.tokenName = "VIP"
	view, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusNeedsMapping, view.Status)

	group := "default"
	view, err = UpdateChannelUpstreamRatio(1, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeAuto, GroupName: &group, Version: view.Version})
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusOK, view.Status)
	assert.Equal(t, "1", view.Ratio)
	assert.Equal(t, model.UpstreamRatioMappingManual, view.Mapping)

	// 手动关联在后续探测中保留。
	view, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "default", view.GroupName)
	assert.Equal(t, model.UpstreamRatioMappingManual, view.Mapping)

	// 取消手动关联后，旧分组的倍率不能继续作为有效值显示。
	empty := ""
	view, err = UpdateChannelUpstreamRatio(1, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeAuto, GroupName: &empty, Version: view.Version})
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusNeedsMapping, view.Status)
	assert.Empty(t, view.Ratio)
	assert.False(t, view.Stale)

	auto := "auto"
	_, err = UpdateChannelUpstreamRatio(1, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeAuto, GroupName: &auto, Version: view.Version})
	assert.ErrorIs(t, err, ErrUpstreamRatioInvalid, "auto group has no fixed ratio")
	_, err = UpdateChannelUpstreamRatio(1, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeManual, Ratio: "2", Version: view.Version - 1})
	assert.ErrorIs(t, err, model.ErrUpstreamRatioConflict)
}

func TestNewAPIPricingRequiresLoginFallsBackToFailure(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	server := (&fakeUpstream{tokenName: "vip"}).serve(t)
	createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeNewAPI, server.URL)
	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusFailed, view.Status)
	assert.Equal(t, model.UpstreamRatioSourceNewAPI, view.Source, "a failed probe never changes the fixed source")
	assert.False(t, view.Stale, "a first failure has no old value to mark stale")
}

func TestSub2APIUpstreamRatioAndFingerprint(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	server := (&fakeUpstream{billing: sub2apiBody("1.2", "1.2", "1.2", `,"peak_rate_enabled":false`)}).serve(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeSub2API, server.URL)

	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusOK, view.Status)
	assert.Equal(t, "1.2", view.EffectiveRatio)
	require.NotNil(t, view.Sub2API)

	// 渠道 Key 改变后旧记录显示过期，且不改动 channel_ratio。
	require.NoError(t, db.Model(channel).Update("key", "sk-other").Error)
	views, err := GetChannelUpstreamRatioViews([]int{1}, false)
	require.NoError(t, err)
	assert.True(t, views[0].Stale)
	var stored model.Channel
	require.NoError(t, db.First(&stored, 1).Error)
	assert.Equal(t, "1", stored.ChannelRatio)

	view, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusAuthFailed, view.Status)
	assert.Equal(t, "1.2", view.Ratio, "failures keep the last declared value")
	assert.True(t, view.Stale, "the kept value belongs to the old key")
}

func TestDetectMainstreamChannelUpstream(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	upstream := &fakeUpstream{tokenName: "vip", pricing: fakePricing}
	server := upstream.serve(t)
	createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeAnthropic, server.URL)
	createUpstreamRatioChannel(t, db, 2, constant.ChannelTypeNewAPI, server.URL)

	due, err := model.GetDueUpstreamRatioChannels(time.Now().Unix(), 20)
	require.NoError(t, err)
	require.Len(t, due, 1, "mainstream channels are only probed after manual detection")
	assert.Equal(t, 2, due[0].Id)

	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, UpstreamRatioKindDetect, view.Kind)
	assert.Equal(t, model.UpstreamRatioSourceNewAPI, view.Source)
	assert.Equal(t, "1.5", view.Ratio)

	official := createUpstreamRatioChannel(t, db, 3, constant.ChannelTypeOpenAI, "https://api.openai.com/v1")
	hits := upstream.hits.Load()
	view, err = RefreshChannelUpstreamRatio(context.Background(), official.Id)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusUnsupported, view.Status)
	assert.Equal(t, "official_api", view.LastError)
	assert.Equal(t, hits, upstream.hits.Load())

	plain := createUpstreamRatioChannel(t, db, 4, constant.ChannelTypeOpenAI, server.URL+"/none")
	view, err = RefreshChannelUpstreamRatio(context.Background(), plain.Id)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusUnsupported, view.Status)
	assert.Empty(t, view.Source)
}

func TestUpstreamRatioRejectsRedirectAndLargeBody(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/big") {
			_, _ = w.Write([]byte(strings.Repeat("x", upstreamRatioSmallBodyLimit+1)))
			return
		}
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeSub2API, server.URL)
	createUpstreamRatioChannel(t, db, 2, constant.ChannelTypeSub2API, server.URL+"/big")

	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "redirect", view.LastError)
	view, err = RefreshChannelUpstreamRatio(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, "response_too_large", view.LastError)
	assert.NotContains(t, view.LastError, fakeUpstreamKey)
}

func TestUpstreamRatioCycleRemovesOrphans(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	server := (&fakeUpstream{billing: sub2apiBody("1", "1", "1", `,"peak_rate_enabled":false`)}).serve(t)
	createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeSub2API, server.URL)
	require.NoError(t, db.Create(&model.ChannelUpstreamRatio{ChannelId: 99, Mode: model.UpstreamRatioModeAuto, Version: 1}).Error)

	summary, err := RunChannelUpstreamRatioCycle(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Orphans)
	assert.Equal(t, 1, summary.Statuses[model.UpstreamRatioStatusOK])
	due, err := model.HasDueUpstreamRatioChannels(time.Now().Unix())
	require.NoError(t, err)
	assert.False(t, due)
}

func TestManualUpstreamRatioOnUnsupportedChannel(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeAdvancedCustom, "https://relay.example.com")
	view, err := UpdateChannelUpstreamRatio(channel.Id, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeManual, Ratio: "0.85"})
	require.NoError(t, err)
	assert.Equal(t, UpstreamRatioKindUnsupported, view.Kind)
	assert.Equal(t, "0.85", view.Ratio)
	assert.False(t, view.Stale)
	for _, ratio := range []string{"1001", "0e100000"} {
		_, err = UpdateChannelUpstreamRatio(channel.Id, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeManual, Ratio: ratio, Version: view.Version})
		assert.ErrorIs(t, err, ErrUpstreamRatioInvalid, ratio)
	}
}

// 代理地址是拨号目标，必须经过 SSRF 检查：回环代理在开启防护时不能被连接。
func TestUpstreamRatioProxyDialIsProtected(t *testing.T) {
	setupUpstreamRatioTest(t)
	fetch := system_setting.GetFetchSetting()
	fetch.EnableSSRFProtection = true
	fetch.AllowPrivateIp = false
	client, err := newUpstreamRatioHTTPClient("http://127.0.0.1:1")
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://upstream.invalid/v1/sub2api/billing")
	if resp != nil {
		_ = resp.Body.Close()
	}
	assert.Error(t, err, "loopback proxy must be refused by the protected dialer")
}

// 走代理时拨号器只检查代理地址，目标 URL 也必须按 SSRF 规则校验，被禁止的目标不能经代理发出。
func TestUpstreamRatioProxyTargetIsProtected(t *testing.T) {
	setupUpstreamRatioTest(t)
	fetch := system_setting.GetFetchSetting()
	fetch.EnableSSRFProtection = true
	fetch.AllowPrivateIp = false
	fetch.AllowedPorts = []string{"80", "8080"}
	client, err := newUpstreamRatioHTTPClient("http://8.8.8.8:8080")
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	// 保留受保护拨号器，只把最终 TCP 连接换成内存里的代理，不访问网络。
	var proxied atomic.Int32
	dialer := &protectedFetchDialer{resolver: net.DefaultResolver, getProtection: currentFetchProtection,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				req, err := http.ReadRequest(bufio.NewReader(serverConn))
				if err != nil {
					return
				}
				proxied.Add(1)
				_ = req.Body.Close()
				fmt.Fprint(serverConn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
			}()
			return clientConn, nil
		}}
	client.Transport.(*http.Transport).DialContext = dialer.DialContext

	_, err = upstreamRatioGet(context.Background(), client, "http://169.254.169.254/v1/sub2api/billing", "sk-test", 1024)
	require.Error(t, err)
	assert.Zero(t, proxied.Load(), "a forbidden target must not reach the proxy")

	resp, err := upstreamRatioGet(context.Background(), client, "http://1.1.1.1/v1/sub2api/billing", "sk-test", 1024)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 1, proxied.Load())
}

// 直连探测只在拨号时、按探测的 context 解析目标；已取消的探测不发起 DNS 查询。
func TestUpstreamRatioDirectProbeResolvesWithinItsContext(t *testing.T) {
	setupUpstreamRatioTest(t)
	fetch := system_setting.GetFetchSetting()
	fetch.EnableSSRFProtection = true
	fetch.ApplyIPFilterForDomain = true
	fetch.AllowPrivateIp = false
	fetch.AllowedPorts = []string{"80"}
	previous := net.DefaultResolver
	var lookups atomic.Int32
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		lookups.Add(1)
		return nil, fmt.Errorf("offline resolver")
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	client, err := newUpstreamRatioHTTPClient("")
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = upstreamRatioGet(ctx, client, "http://relay.example.com/v1/sub2api/billing", "", 1024)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, lookups.Load())
}

func TestUpstreamRatioZonedIPv6HostIsRefused(t *testing.T) {
	setupUpstreamRatioTest(t)
	fetch := system_setting.GetFetchSetting()
	fetch.EnableSSRFProtection = true
	fetch.AllowPrivateIp = false
	for _, proxy := range []string{"", "http://[fe80::1%25eth0]:8080"} {
		client, err := newUpstreamRatioHTTPClient(proxy)
		require.NoError(t, err)
		resp, err := client.Get("http://[fe80::1%25eth0]:8080/v1/sub2api/billing")
		if resp != nil {
			_ = resp.Body.Close()
		}
		client.CloseIdleConnections()
		assert.ErrorIs(t, err, errUpstreamRatioZonedHost, "proxy %q", proxy)
	}
}

// 普通代理下目标主机不经过本地拨号，带 zone 的 IPv6 目标必须在拼地址时就被拒绝。
func TestUpstreamRatioZonedTargetIsRefusedBehindProxy(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeSub2API, "http://[fe80::1%25eth0]:8080/v1")
	setting := `{"proxy":"http://127.0.0.1:9"}`
	require.NoError(t, db.Model(channel).Update("setting", setting).Error)
	view, err := RefreshChannelUpstreamRatio(context.Background(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusFailed, view.Status)
	assert.Equal(t, "invalid_base_url", view.LastError)
}

// 读取和探测倍率只解析渠道设置，坏 JSON 不能被原生 GetSetting 清空并写回。
func TestUpstreamRatioKeepsMalformedChannelSetting(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeSub2API, "https://relay.example.com")
	require.NoError(t, db.Model(channel).Update("setting", "{broken").Error)
	view, err := RefreshChannelUpstreamRatio(context.Background(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "invalid_proxy", view.LastError)
	_, err = GetChannelUpstreamRatioViews([]int{channel.Id}, false)
	require.NoError(t, err)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.NotNil(t, stored.Setting)
	assert.Equal(t, "{broken", *stored.Setting)
}

// 高峰倍率为 0 是合法声明，序列化后必须保留，否则弹窗会把缺失值当成 1 显示。
func TestSub2APIZeroPeakMultiplierSurvivesStorage(t *testing.T) {
	encoded := marshalUpstreamRatioDetail(Sub2APIBilling{PeakEnabled: true, PeakRate: 0, ResolvedRate: 1})
	assert.Contains(t, encoded, `"peak_rate_multiplier":0`)
}

// 连续失败的重探间隔逐次翻倍，封顶 24 小时；Retry-After 更长时以它为准。
func TestFailedUpstreamRatioProbeDelayBacksOff(t *testing.T) {
	for range 50 {
		first := failedUpstreamRatioProbeDelay(30, 1, 0)
		assert.GreaterOrEqual(t, first, 25*time.Minute)
		assert.LessOrEqual(t, first, 35*time.Minute)
		assert.Greater(t, failedUpstreamRatioProbeDelay(30, 3, 0), failedUpstreamRatioProbeDelay(30, 1, 0)-time.Minute)
	}
	assert.Equal(t, upstreamRatioMaxDelay, failedUpstreamRatioProbeDelay(30, 50, 0))
	assert.Equal(t, 2*time.Hour, failedUpstreamRatioProbeDelay(30, 1, 2*time.Hour))
}

// 秒数过大时先限幅再换算，否则乘以 time.Second 溢出成负数，被当作没有 Retry-After。
func TestUpstreamRatioRetryAfterCapsHugeSeconds(t *testing.T) {
	header := http.Header{"Retry-After": []string{"9223372037"}}
	assert.Equal(t, upstreamRatioMaxDelay, upstreamRatioRetryAfter(header, time.Now()))
	header.Set("Retry-After", "99999999999999999999999")
	assert.Equal(t, upstreamRatioMaxDelay, upstreamRatioRetryAfter(header, time.Now()), "digits beyond int range still mean wait")
	header.Set("Retry-After", "120")
	assert.Equal(t, 2*time.Minute, upstreamRatioRetryAfter(header, time.Now()))
}

// 换成另一个凭据后，旧凭据按名称匹配到的倍率不能以新指纹继续显示为有效值。
func TestNewAPIRatioClearedWhenNewCredentialHasNoGroup(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	first := (&fakeUpstream{tokenName: "vip", pricing: fakePricing}).serve(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeNewAPI, first.URL)
	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "1.5", view.Ratio)

	second := (&fakeUpstream{tokenName: "partner", pricing: fakePricing}).serve(t)
	require.NoError(t, db.Model(channel).Update("base_url", second.URL).Error)
	view, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.UpstreamRatioStatusNeedsMapping, view.Status)
	assert.Empty(t, view.Ratio, "the old ratio belongs to the previous credential")
	assert.Empty(t, view.GroupName)
	assert.False(t, view.Stale)
}

// 固定类型互换后探测失败：旧来源的明细不能按新来源解码，否则有效倍率显示为 0。
func TestUpstreamRatioTypeSwitchDropsOldSourceDetail(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	server := (&fakeUpstream{tokenName: "vip", pricing: fakePricing}).serve(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeNewAPI, server.URL)
	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "1.5", view.Ratio)

	require.NoError(t, db.Model(channel).Update("type", constant.ChannelTypeSub2API).Error)
	view, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.NotEqual(t, model.UpstreamRatioStatusOK, view.Status)
	assert.Equal(t, model.UpstreamRatioSourceSub2API, view.Source)
	assert.Nil(t, view.Sub2API)
	assert.Empty(t, view.Ratio)
	assert.Empty(t, view.EffectiveRatio)
	assert.Empty(t, view.GroupName)
}

// 经过不支持类型（来源被清空）再改成固定类型：旧明细没有来源标记，同样不能按新来源解码。
func TestUpstreamRatioSwitchThroughUnsupportedDropsOldDetail(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	server := (&fakeUpstream{tokenName: "vip", pricing: fakePricing}).serve(t)
	channel := createUpstreamRatioChannel(t, db, 1, constant.ChannelTypeNewAPI, server.URL)
	view, err := RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "1.5", view.Ratio)

	require.NoError(t, db.Model(channel).Update("type", constant.ChannelTypeAdvancedCustom).Error)
	_, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, db.Model(channel).Update("type", constant.ChannelTypeSub2API).Error)
	view, err = RefreshChannelUpstreamRatio(context.Background(), 1)
	require.NoError(t, err)
	assert.Nil(t, view.Sub2API)
	assert.Empty(t, view.Ratio)
	assert.Empty(t, view.EffectiveRatio)
}

// 删除后用同一 ID 重建的渠道是新一代：旧一代的记录（即使配置相同）不能作为新渠道的数据。
func TestUpstreamRatioRecreatedChannelDoesNotInheritRecord(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	baseURL := "https://relay.example.com"
	old := &model.Channel{Id: 1, Type: constant.ChannelTypeAdvancedCustom, Key: fakeUpstreamKey, Name: "old", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, CreatedTime: 1000}
	require.NoError(t, db.Create(old).Error)
	view, err := UpdateChannelUpstreamRatio(1, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeManual, Ratio: "0.85"})
	require.NoError(t, err)
	require.Equal(t, "0.85", view.Ratio)

	require.NoError(t, db.Delete(&model.Channel{}, 1).Error)
	recreated := &model.Channel{Id: 1, Type: constant.ChannelTypeAdvancedCustom, Key: fakeUpstreamKey, Name: "new", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, CreatedTime: 2000}
	require.NoError(t, db.Create(recreated).Error)
	views, err := GetChannelUpstreamRatioViews([]int{1}, false)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Empty(t, views[0].Ratio, "the manual ratio belongs to the deleted channel")
	assert.Equal(t, model.UpstreamRatioModeAuto, views[0].Mode)
	assert.Zero(t, views[0].Version)

	view, err = UpdateChannelUpstreamRatio(1, UpstreamRatioUpdate{Mode: model.UpstreamRatioModeManual, Ratio: "1.1"})
	require.NoError(t, err, "a version-0 save replaces the previous generation's row")
	assert.Equal(t, "1.1", view.Ratio)
}

// Detection sends the channel key as a Bearer token, so it is offered only for
// types whose relay already sends the whole stored key to the channel's base
// URL. Types that derive a signature or token from a composite key (Zhipu v3,
// Xunfei, Tencent, Baidu v2) are unsupported and send nothing; a kept type
// still detects.
func TestUpstreamRatioDetectionSkipsDerivedCredentialTypes(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	upstream := &fakeUpstream{tokenName: "vip", pricing: fakePricing}
	server := upstream.serve(t)
	for id, channelType := range map[int]int{1: constant.ChannelTypeZhipu, 2: constant.ChannelTypeXunfei, 3: constant.ChannelTypeTencent, 4: constant.ChannelTypeBaiduV2} {
		createUpstreamRatioChannel(t, db, id, channelType, server.URL)
		view, err := RefreshChannelUpstreamRatio(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, UpstreamRatioKindUnsupported, view.Kind, channelType)
		assert.Equal(t, model.UpstreamRatioStatusUnsupported, view.Status, channelType)
		assert.Equal(t, "unsupported_channel", view.LastError, channelType)
	}
	assert.Zero(t, upstream.hits.Load(), "no request reaches the upstream for these types")

	createUpstreamRatioChannel(t, db, 5, constant.ChannelTypeBaidu, server.URL)
	view, err := RefreshChannelUpstreamRatio(context.Background(), 5)
	require.NoError(t, err)
	assert.Equal(t, UpstreamRatioKindDetect, view.Kind)
	assert.Equal(t, model.UpstreamRatioSourceNewAPI, view.Source)
	assert.Equal(t, "1.5", view.Ratio)
}
