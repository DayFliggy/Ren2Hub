package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
)

var ErrProviderUsageUnsupported = errors.New("provider usage requires a single-credential Codex channel")

const providerUsageInterval = 5 * time.Minute
const providerUsageManualInterval = time.Minute

type ProviderUsageWindow struct {
	Name         string   `json:"name"`
	UsedPercent  *float64 `json:"used_percent"`
	LimitSeconds *int64   `json:"limit_seconds"`
	ResetAt      *int64   `json:"reset_at"`
}

type ProviderUsageView struct {
	ChannelID      int                   `json:"channel_id"`
	Provider       string                `json:"provider"`
	State          string                `json:"state"`
	PlanType       string                `json:"plan_type"`
	Windows        []ProviderUsageWindow `json:"windows"`
	FetchedAt      int64                 `json:"fetched_at"`
	ExpiresAt      int64                 `json:"expires_at"`
	LastAttemptAt  int64                 `json:"last_attempt_at"`
	NextRefreshAt  int64                 `json:"next_refresh_at"`
	UpstreamStatus int                   `json:"upstream_status"`
	ErrorCode      string                `json:"error_code"`
	AutoRefresh    bool                  `json:"auto_refresh"`
}

type providerUsageCollector struct {
	flight            singleflight.Group
	workers           *common.WorkerGroup
	now               func() time.Time
	client            func(string) (*http.Client, error)
	refreshCredential func(context.Context, int, CodexCredentialRefreshOptions) (*CodexOAuthKey, *model.Channel, error)
}

var defaultProviderUsageCollector = &providerUsageCollector{now: time.Now, client: GetHttpClientWithProxy, refreshCredential: RefreshCodexChannelCredential}

func providerUsageChannel(channelID int) (*model.Channel, *CodexOAuthKey, error) {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, nil, err
	}
	if channel == nil || channel.Type != constant.ChannelTypeCodex || channel.ChannelInfo.IsMultiKey {
		return nil, nil, ErrProviderUsageUnsupported
	}
	key, err := parseCodexOAuthKey(channel.Key)
	if err != nil || strings.TrimSpace(key.AccessToken) == "" || strings.TrimSpace(key.AccountID) == "" {
		return nil, nil, ErrProviderUsageUnsupported
	}
	return channel, key, nil
}

func providerUsageView(channelID int, snapshot *model.ProviderUsageSnapshot, enabled bool, now time.Time) (ProviderUsageView, error) {
	view := ProviderUsageView{ChannelID: channelID, Provider: "codex", State: "unknown", Windows: []ProviderUsageWindow{}, AutoRefresh: enabled}
	if snapshot == nil {
		return view, nil
	}
	view.State, view.PlanType = snapshot.State, snapshot.PlanType
	view.FetchedAt, view.ExpiresAt = snapshot.FetchedAt, snapshot.ExpiresAt
	view.LastAttemptAt, view.NextRefreshAt = snapshot.LastAttemptAt, snapshot.NextRefreshAt
	view.UpstreamStatus, view.ErrorCode = snapshot.UpstreamStatus, snapshot.ErrorCode
	if snapshot.Windows != "" {
		if err := common.UnmarshalJsonStr(string(snapshot.Windows), &view.Windows); err != nil {
			return view, errors.New("provider usage cache is invalid")
		}
	}
	if view.State == "known" && view.ExpiresAt <= now.Unix() {
		view.State = "stale"
	}
	return view, nil
}

func GetProviderUsage(ctx context.Context, channelID int) (ProviderUsageView, error) {
	channel, _, err := providerUsageChannel(channelID)
	if err != nil {
		return ProviderUsageView{}, err
	}
	setting, err := model.ReadProviderUsageSetting(ctx, channelID)
	if err != nil {
		return ProviderUsageView{}, err
	}
	snapshot, err := model.ReadProviderUsageSnapshot(ctx, channel)
	if err != nil {
		return ProviderUsageView{}, err
	}
	return providerUsageView(channelID, snapshot, setting.Enabled, time.Now())
}

func SetProviderUsageAutoRefresh(ctx context.Context, channelID int, enabled bool) (ProviderUsageView, error) {
	if _, _, err := providerUsageChannel(channelID); err != nil {
		return ProviderUsageView{}, err
	}
	if err := model.SaveProviderUsageSetting(ctx, channelID, enabled); err != nil {
		return ProviderUsageView{}, err
	}
	return GetProviderUsage(ctx, channelID)
}

func RefreshProviderUsage(ctx context.Context, channelID int) (ProviderUsageView, error) {
	return defaultProviderUsageCollector.refresh(ctx, channelID, providerUsageManualInterval)
}

func (collector *providerUsageCollector) refresh(ctx context.Context, channelID int, interval time.Duration) (ProviderUsageView, error) {
	workers := collector.workers
	if workers == nil {
		workers = common.BackgroundWorkers
	}
	if !workers.Accepting() {
		return ProviderUsageView{}, context.Canceled
	}
	channel, _, err := providerUsageChannel(channelID)
	if err != nil {
		return ProviderUsageView{}, err
	}
	credential, err := model.EnsureGatewayCredential(ctx, channelID, channel.Key)
	if err != nil {
		return ProviderUsageView{}, err
	}
	endpoint := channel.GatewayEndpointID()
	result := collector.flight.DoChan(credential.ID+":"+endpoint, func() (any, error) {
		// Shared refreshes outlive individual browsers, but shutdown owns their
		// cancellation and joins both provider I/O and the final database write.
		finished := make(chan struct{})
		var view ProviderUsageView
		var collectErr error
		if !workers.Go(func(workerCtx context.Context) {
			defer close(finished)
			fetchCtx, cancel := context.WithTimeout(workerCtx, 40*time.Second)
			defer cancel()
			view, collectErr = collector.collect(fetchCtx, channelID, credential.ID, endpoint, interval)
		}) {
			return nil, context.Canceled
		}
		<-finished
		return view, collectErr
	})
	select {
	case <-ctx.Done():
		return ProviderUsageView{}, ctx.Err()
	case result := <-result:
		if result.Val == nil {
			return ProviderUsageView{}, result.Err
		}
		return result.Val.(ProviderUsageView), result.Err
	}
}

func (collector *providerUsageCollector) collect(ctx context.Context, channelID int, credentialID, endpointID string, interval time.Duration) (ProviderUsageView, error) {
	now := collector.now()
	snapshot, err := model.BeginProviderUsageRefresh(ctx, channelID, credentialID, endpointID, now.Unix(), int64(interval/time.Second))
	if errors.Is(err, model.ErrProviderUsageCooldown) {
		view, readErr := GetProviderUsage(ctx, channelID)
		if readErr != nil {
			return view, readErr
		}
		return view, err
	}
	if err != nil {
		return ProviderUsageView{}, err
	}
	channel, key, err := providerUsageChannel(channelID)
	if err != nil {
		return ProviderUsageView{}, err
	}
	if channel.GatewayEndpointID() != endpointID {
		return ProviderUsageView{}, model.ErrProviderUsageIdentityChanged
	}
	if _, _, err := model.ResolveGatewayCredential(ctx, channel, credentialID); err != nil {
		return ProviderUsageView{}, model.ErrProviderUsageIdentityChanged
	}
	client, err := collector.client(channel.GetSetting().Proxy)
	status, body, retryAfter, fetchErr := 0, []byte(nil), time.Duration(0), err
	if fetchErr == nil {
		status, body, retryAfter, fetchErr = fetchProviderUsage(ctx, client, channel.GetBaseURL(), key.AccessToken, key.AccountID)
	}
	if fetchErr == nil && (status == http.StatusUnauthorized || status == http.StatusForbidden) && strings.TrimSpace(key.RefreshToken) != "" {
		key, channel, err = collector.refreshCredential(ctx, channelID, CodexCredentialRefreshOptions{ResetCaches: true})
		if err != nil {
			// The trusted refresh persists credentials atomically. Never issue a
			// second request after its write failed, or turn that failure into success.
			snapshot.State, snapshot.ErrorCode, snapshot.UpstreamStatus = "error", "credential_refresh_failed", status
			snapshot.FailureCount++
			snapshot.NextRefreshAt = collector.now().Add(providerUsageErrorBackoff(snapshot.FailureCount, 0)).Unix()
			if persistErr := model.FinishProviderUsageRefresh(ctx, snapshot); persistErr != nil {
				return ProviderUsageView{}, errors.Join(err, persistErr)
			}
			return ProviderUsageView{}, fmt.Errorf("provider usage credential refresh failed: %w", err)
		}
		status, body, retryAfter, fetchErr = fetchProviderUsage(ctx, client, channel.GetBaseURL(), key.AccessToken, key.AccountID)
	}
	now = collector.now()
	var windows []ProviderUsageWindow
	var plan string
	if fetchErr == nil && status >= 200 && status < 300 {
		plan, windows, fetchErr = parseCodexProviderUsage(body, now)
	}
	snapshot.UpstreamStatus = status
	if fetchErr != nil || status < 200 || status >= 300 {
		snapshot.State, snapshot.ErrorCode = "error", "upstream_unavailable"
		if fetchErr == nil {
			snapshot.ErrorCode = "upstream_rejected"
		}
		if status == http.StatusTooManyRequests {
			snapshot.ErrorCode = "rate_limited"
		}
		snapshot.FailureCount++
		snapshot.NextRefreshAt = now.Add(providerUsageErrorBackoff(snapshot.FailureCount, retryAfter)).Unix()
	} else {
		encoded, err := common.Marshal(windows)
		if err != nil {
			return ProviderUsageView{}, err
		}
		snapshot.Windows, snapshot.PlanType = model.LongText(encoded), plan
		snapshot.State, snapshot.ErrorCode, snapshot.FailureCount = "unknown", "", 0
		for _, window := range windows {
			if window.UsedPercent != nil {
				snapshot.State = "known"
				break
			}
		}
		snapshot.FetchedAt, snapshot.ExpiresAt = now.Unix(), now.Add(providerUsageInterval).Unix()
		snapshot.NextRefreshAt = now.Add(providerUsageManualInterval).Unix()
	}
	if err := model.FinishProviderUsageRefresh(ctx, snapshot); err != nil {
		return ProviderUsageView{}, err
	}
	setting, err := model.ReadProviderUsageSetting(ctx, channelID)
	if err != nil {
		return ProviderUsageView{}, err
	}
	return providerUsageView(channelID, &snapshot, setting.Enabled, now)
}

func providerUsageErrorBackoff(failures int, retryAfter time.Duration) time.Duration {
	if failures > 7 {
		failures = 7
	}
	if failures < 1 {
		failures = 1
	}
	backoff := time.Minute * time.Duration(1<<(failures-1))
	if backoff > time.Hour {
		backoff = time.Hour
	}
	if retryAfter > backoff {
		backoff = retryAfter
	}
	return backoff
}

func fetchProviderUsage(ctx context.Context, client *http.Client, baseURL, accessToken, accountID string) (int, []byte, time.Duration, error) {
	if client == nil {
		return 0, nil, 0, errors.New("provider usage client unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/backend-api/wham/usage", nil)
	if err != nil {
		return 0, nil, 0, err
	}
	setCodexWhamRequestHeaders(req, accessToken, accountID)
	copyClient := *client
	copyClient.Timeout = 15 * time.Second
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return 0, nil, 0, err
	}
	defer resp.Body.Close()
	retryAfter, _ := ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err == nil && len(body) > 64*1024 {
		err = errors.New("provider usage response too large")
	}
	return resp.StatusCode, body, retryAfter, err
}

var providerUsagePlanPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Optional window fields remain null. Absent usage is never reported as 0%.
func parseCodexProviderUsage(body []byte, now time.Time) (string, []ProviderUsageWindow, error) {
	type wireWindow struct {
		UsedPercent  *float64 `json:"used_percent"`
		LimitSeconds *int64   `json:"limit_window_seconds"`
		ResetAt      *int64   `json:"reset_at"`
		ResetAfter   *int64   `json:"reset_after_seconds"`
	}
	type wireLimit struct {
		Primary   *wireWindow `json:"primary_window"`
		Secondary *wireWindow `json:"secondary_window"`
	}
	var payload struct {
		PlanType  string     `json:"plan_type"`
		RateLimit *wireLimit `json:"rate_limit"`
		Review    *wireLimit `json:"code_review_rate_limit"`
	}
	if strings.TrimSpace(string(body)) == "null" {
		return "", nil, errors.New("provider usage response is not an object")
	}
	if err := common.Unmarshal(body, &payload); err != nil {
		return "", nil, errors.New("provider usage response is invalid")
	}
	if payload.PlanType != "" && !providerUsagePlanPattern.MatchString(payload.PlanType) {
		return "", nil, errors.New("provider usage plan is invalid")
	}
	windows := []ProviderUsageWindow{}
	for _, limit := range []struct {
		name  string
		value *wireLimit
	}{{"rate_limit", payload.RateLimit}, {"code_review", payload.Review}} {
		if limit.value == nil {
			continue
		}
		for _, item := range []struct {
			name  string
			value *wireWindow
		}{{"primary", limit.value.Primary}, {"secondary", limit.value.Secondary}} {
			if item.value == nil {
				continue
			}
			value := item.value
			if value.UsedPercent != nil && (math.IsNaN(*value.UsedPercent) || math.IsInf(*value.UsedPercent, 0) || *value.UsedPercent < 0 || *value.UsedPercent > 100) {
				return "", nil, errors.New("provider usage percentage is invalid")
			}
			if value.LimitSeconds != nil && (*value.LimitSeconds <= 0 || *value.LimitSeconds > 366*86400) {
				return "", nil, errors.New("provider usage window is invalid")
			}
			if value.ResetAt != nil && (*value.ResetAt < 0 || *value.ResetAt > 253402300799) {
				return "", nil, errors.New("provider usage reset is invalid")
			}
			if value.ResetAt == nil && value.ResetAfter != nil {
				if *value.ResetAfter < 0 || *value.ResetAfter > 366*86400 {
					return "", nil, errors.New("provider usage reset is invalid")
				}
				reset := now.Unix() + *value.ResetAfter
				value.ResetAt = &reset
			}
			windows = append(windows, ProviderUsageWindow{Name: limit.name + "_" + item.name, UsedPercent: value.UsedPercent, LimitSeconds: value.LimitSeconds, ResetAt: value.ResetAt})
		}
	}
	return payload.PlanType, windows, nil
}
