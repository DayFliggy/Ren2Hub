package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// UpstreamCredential is only serialized inside the account's authenticated ciphertext.
type UpstreamCredential struct {
	AccessToken      string `json:"access_token,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	PAT              string `json:"pat,omitempty"`
	AccessExpiresAt  int64  `json:"access_expires_at,omitempty"`
	RefreshExpiresAt int64  `json:"refresh_expires_at,omitempty"`
	RemotePATID      int    `json:"remote_pat_id,omitempty"`
}
type UpstreamRemoteKey struct {
	UsedQuota       *int64   `json:"used_quota,omitempty"`
	Quota           *float64 `json:"quota,omitempty"`
	QuotaUsed       *float64 `json:"quota_used,omitempty"`
	ExpiresAt       *string  `json:"expires_at,omitempty"`
	RemainQuota     *int64   `json:"remain_quota,omitempty"`
	UnlimitedQuota  *bool    `json:"unlimited_quota,omitempty"`
	ExpiredTime     *int64   `json:"expired_time,omitempty"`
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Prefix          string   `json:"remote_key_prefix"`
	Status          string   `json:"status"`
	KeySHA256       string   `json:"-"`
	UpstreamGroup   string   `json:"upstream_group"`
	UpstreamGroupID int      `json:"upstream_group_id"`
}
type UpstreamKeyInput struct {
	Name            *string  `json:"name"`
	UpstreamGroup   *string  `json:"upstream_group"`
	UpstreamGroupID *int     `json:"upstream_group_id"`
	Quota           *float64 `json:"quota"`
	ExpiresInDays   *int     `json:"expires_in_days"`
	ExpiresAt       *string  `json:"expires_at"`
	ExpiredTime     *int64   `json:"expired_time"`
	RemainQuota     *int     `json:"remain_quota"`
	UnlimitedQuota  *bool    `json:"unlimited_quota"`
}
type UpstreamGroup struct {
	ID       int    `json:"id,omitempty"`
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
	Ratio    string `json:"ratio"`
}
type UpstreamSubscription struct {
	ID        int    `json:"id"`
	GroupID   int    `json:"upstream_group_id"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at"`
}
type UpstreamQuotaDisplay struct {
	QuotaPerUnit               float64 `json:"quota_per_unit,omitempty"`
	QuotaDisplayType           string  `json:"quota_display_type,omitempty"`
	USDExchangeRate            float64 `json:"usd_exchange_rate,omitempty"`
	CustomCurrencySymbol       string  `json:"custom_currency_symbol,omitempty"`
	CustomCurrencyExchangeRate float64 `json:"custom_currency_exchange_rate,omitempty"`
}

type UpstreamSiteMeta struct {
	Username  string `json:"username,omitempty"`
	UserGroup string `json:"upstream_group,omitempty"`
	Balance   string `json:"balance,omitempty"`
	UpstreamQuotaDisplay
}

type UpstreamSnapshot struct {
	UpstreamSiteMeta
	Groups        []UpstreamGroup        `json:"groups"`
	Subscriptions []UpstreamSubscription `json:"subscriptions"`
}
type UpstreamRemoteError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *UpstreamRemoteError) Error() string { return e.Code }
func remoteError(code string) error          { return &UpstreamRemoteError{Code: code} }

type upstreamAdapter struct {
	platform, baseURL, origin string
	cred                      UpstreamCredential
	client                    *http.Client
}

func newUpstreamAdapter(platform, baseURL, proxyURL string, cred UpstreamCredential) (*upstreamAdapter, error) {
	if err := validateUpstreamEndpoint(platform, baseURL, proxyURL); err != nil {
		return nil, err
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, remoteError("invalid_base_url")
	}
	client, err := newUpstreamRatioHTTPClient(proxyURL)
	if err != nil {
		return nil, remoteError("invalid_proxy_url")
	}
	client.Jar, _ = cookiejar.New(nil)
	return &upstreamAdapter{platform, strings.TrimRight(u.String(), "/"), u.Scheme + "://" + u.Host, cred, client}, nil
}
func (a *upstreamAdapter) call(ctx context.Context, method, path string, body, out any, headers http.Header) error {
	return a.callWithBudget(ctx, method, path, body, out, headers, nil)
}

func (a *upstreamAdapter) callWithBudget(ctx context.Context, method, path string, body, out any, headers http.Header, remaining *int64) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = common.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return remoteError("invalid_base_url")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Ren2Hub-Upstream/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token := a.cred.AccessToken
	if a.platform == "newapi" {
		token = a.cred.PAT
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	resp, err := a.client.Do(req)
	if err != nil {
		var n net.Error
		if errors.As(err, &n) && n.Timeout() {
			return remoteError("upstream_timeout")
		}
		return remoteError("upstream_unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return &UpstreamRemoteError{Code: "upstream_rate_limited", RetryAfter: upstreamRatioRetryAfter(resp.Header, time.Now())}
	}
	limit := int64(upstreamRatioPricingBodyLimit)
	if remaining != nil {
		limit = min(limit, *remaining)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if remaining != nil {
		*remaining -= int64(len(b))
		if *remaining < 0 {
			return remoteError("upstream_pagination_limit_exceeded")
		}
	}
	if err != nil || len(b) > upstreamRatioPricingBodyLimit {
		return remoteError("upstream_invalid_response")
	}
	var env struct {
		Code            common.RawMessage
		Success         *bool
		Reason, Message string
		Data            common.RawMessage
		Error           common.RawMessage
	}
	decodeErr := common.Unmarshal(b, &env)
	code := common.JsonRawMessageToString(env.Code)
	// ErrorFrom uses a numeric HTTP code plus reason, JWT middleware uses a
	// string code, and gateways may return an OpenAI-style nested error.
	var nested struct{ Code string }
	if len(env.Error) > 0 && env.Error[0] == '{' {
		_ = common.Unmarshal(env.Error, &nested)
	}
	codeIs := func(want string) bool {
		return code == want || env.Reason == want || nested.Code == want || common.JsonRawMessageToString(env.Error) == want
	}
	singleKeyGet := method == "GET" && lastNumericID(path) > 0 && path == a.keyPath(lastNumericID(path))
	if singleKeyGet {
		absent := a.platform == "sub2api" && resp.StatusCode == http.StatusNotFound && decodeErr == nil && codeIs("API_KEY_NOT_FOUND")
		if a.platform == "newapi" && (resp.StatusCode == 200 || resp.StatusCode == 404) && decodeErr == nil && env.Success != nil && !*env.Success {
			absent = codeIs("TOKEN_NOT_FOUND") || env.Message == "record not found"
		}
		if absent {
			return remoteError("remote_key_not_found")
		}
	}
	if resp.StatusCode == 401 && codeIs("TOKEN_EXPIRED") {
		return remoteError("token_expired")
	}
	if code == "ACCESS_TOKEN_LIMIT" {
		return remoteError("remote_pat_limit")
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return remoteError("upstream_auth_failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return remoteError(fmt.Sprintf("upstream_http_%d", resp.StatusCode))
	}
	if decodeErr != nil {
		return remoteError("upstream_invalid_response")
	}
	if (a.platform == "newapi" && (env.Success == nil || !*env.Success)) || (a.platform == "sub2api" && code != "0") {
		return remoteError("upstream_request_rejected")
	}
	if out != nil && (len(env.Data) == 0 || string(env.Data) == "null" || common.Unmarshal(env.Data, out) != nil) {
		return remoteError("upstream_invalid_response")
	}
	return nil
}
func lastNumericID(path string) int {
	i, _ := strconv.Atoi(path[strings.LastIndex(path, "/")+1:])
	return i
}
func (a *upstreamAdapter) keyPath(id int) string {
	if a.platform == "newapi" {
		return fmt.Sprintf("/api/token/%d", id)
	}
	return fmt.Sprintf("/api/v1/keys/%d", id)
}

// Only scalar numbers (and NewAPI's explicit automatic-group sentinel) may
// leave the adapter in pricing or balance snapshots.
func upstreamNumericValue(raw common.RawMessage, allowAuto bool) (string, error) {
	value := common.JsonRawMessageToString(raw)
	if value == "" || allowAuto && value == "auto" {
		return value, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return "", remoteError("upstream_invalid_response")
	}
	return value, nil
}

type UpstreamCapabilities struct {
	SiteName                       string `json:"site_name"`
	TOTPEnabled                    *bool  `json:"totp_enabled,omitempty"`
	BackendModeEnabled             *bool  `json:"backend_mode_enabled,omitempty"`
	PasswordLoginEncryptionEnabled *bool  `json:"password_login_encryption_enabled,omitempty"`
	UpstreamQuotaDisplay
	Platform               string `json:"platform"`
	Version                string `json:"version"`
	TurnstileEnabled       bool   `json:"turnstile_enabled"`
	CaptchaEnabled         bool   `json:"captcha_enabled"`
	PasswordLoginAvailable bool   `json:"password_login_available"`
}

type newAPIPublicStatus struct {
	SystemName                     string `json:"system_name"`
	PasswordLoginEncryptionEnabled *bool  `json:"password_login_encryption_enabled"`
	Version                        string `json:"version"`
	TurnstileEnabled               *bool  `json:"turnstile_check"`
	PasswordLoginEnabled           *bool  `json:"password_login_enabled"`
	UpstreamQuotaDisplay
}

func (a *upstreamAdapter) publicStatus(ctx context.Context) (newAPIPublicStatus, error) {
	var status newAPIPublicStatus
	public := *a
	public.cred = UpstreamCredential{}
	err := public.call(ctx, "GET", "/api/status", nil, &status, nil)
	return status, err
}
func (a *upstreamAdapter) Probe(ctx context.Context) (UpstreamCapabilities, error) {
	result := UpstreamCapabilities{Platform: a.platform}
	if a.platform == "newapi" {
		status, err := a.publicStatus(ctx)
		if err != nil {
			return result, err
		}
		if status.Version == "" || status.TurnstileEnabled == nil || status.PasswordLoginEnabled == nil {
			return result, remoteError("upstream_invalid_response")
		}
		result.Version = status.Version
		result.SiteName = status.SystemName
		result.PasswordLoginEncryptionEnabled = status.PasswordLoginEncryptionEnabled
		result.UpstreamQuotaDisplay = status.UpstreamQuotaDisplay
		result.TurnstileEnabled = *status.TurnstileEnabled
		result.CaptchaEnabled = result.TurnstileEnabled
		result.PasswordLoginAvailable = *status.PasswordLoginEnabled && !result.TurnstileEnabled
		return result, nil
	}
	var settings struct {
		TOTPEnabled        *bool  `json:"totp_enabled"`
		BackendModeEnabled *bool  `json:"backend_mode_enabled"`
		SiteName           string `json:"site_name"`
		Version            string `json:"version"`
		TurnstileEnabled   *bool  `json:"turnstile_enabled"`
		TencentEnabled     bool   `json:"tencent_captcha_enabled"`
		AliyunEnabled      bool   `json:"aliyun_captcha_enabled"`
	}
	if e := a.call(ctx, "GET", "/api/v1/settings/public", nil, &settings, nil); e != nil {
		return result, e
	}
	if settings.SiteName == "" || settings.Version == "" || settings.TurnstileEnabled == nil {
		return result, remoteError("upstream_invalid_response")
	}
	result.Version = settings.Version
	result.SiteName = settings.SiteName
	result.TOTPEnabled = settings.TOTPEnabled
	result.BackendModeEnabled = settings.BackendModeEnabled
	result.TurnstileEnabled = *settings.TurnstileEnabled
	result.CaptchaEnabled = result.TurnstileEnabled || settings.TencentEnabled || settings.AliyunEnabled
	result.PasswordLoginAvailable = !result.CaptchaEnabled
	return result, nil
}

func (a *upstreamAdapter) ListKeys(ctx context.Context) ([]UpstreamRemoteKey, error) {
	keys := []UpstreamRemoteKey{}
	seen := map[int]bool{}
	const maxKeys, maxPages = 5000, 50
	remaining := int64(16 << 20)
	expected := -1
	for page := 1; ; page++ {
		path := fmt.Sprintf("/api/v1/keys?page=%d&page_size=200", page)
		if a.platform == "newapi" {
			path = fmt.Sprintf("/api/token/?p=%d&page_size=100", page)
		}
		var data struct {
			Page     int
			PageSize int `json:"page_size"`
			Pages    int
			Total    *int
			Items    []common.RawMessage
		}
		if e := a.callWithBudget(ctx, "GET", path, nil, &data, nil, &remaining); e != nil {
			return nil, e
		}
		if data.Total == nil || *data.Total < 0 || data.PageSize < 1 || data.Page != page || (expected >= 0 && expected != *data.Total) {
			return nil, remoteError("upstream_pagination_incomplete")
		}
		expected = *data.Total
		if expected > maxKeys || data.Pages > maxPages || len(data.Items) > maxKeys {
			return nil, remoteError("upstream_pagination_limit_exceeded")
		}
		pages := 0
		if expected > 0 {
			pages = 1 + (expected-1)/data.PageSize
		}
		if pages > maxPages {
			return nil, remoteError("upstream_pagination_limit_exceeded")
		}
		before := len(keys)
		for _, item := range data.Items {
			k, e := a.decodeKey(item)
			if e != nil {
				return nil, e
			}
			if !seen[k.ID] {
				if len(keys) >= expected {
					return nil, remoteError("upstream_pagination_incomplete")
				}
				keys = append(keys, k)
				seen[k.ID] = true
			}
		}

		if data.Pages > 0 && data.Pages != pages && expected != 0 {
			return nil, remoteError("upstream_pagination_incomplete")
		}
		if page >= pages {
			break
		}
		if before == len(keys) {
			return nil, remoteError("upstream_pagination_incomplete")
		}
	}
	if len(keys) != expected {
		return nil, remoteError("upstream_pagination_incomplete")
	}
	return keys, nil
}
func (a *upstreamAdapter) decodeKey(b []byte) (UpstreamRemoteKey, error) {
	if a.platform == "sub2api" {
		return decodeSub2Key(b)
	}
	return decodeNewAPIKey(b)
}
func (a *upstreamAdapter) GetKey(ctx context.Context, id int) (UpstreamRemoteKey, error) {
	var b common.RawMessage
	if e := a.call(ctx, "GET", a.keyPath(id), nil, &b, nil); e != nil {
		return UpstreamRemoteKey{}, e
	}
	k, e := a.decodeKey(b)
	if e == nil && id != k.ID {
		return UpstreamRemoteKey{}, remoteError("upstream_invalid_response")
	}
	return k, e
}
func (a *upstreamAdapter) Reveal(ctx context.Context, id int) (string, error) {
	path := a.keyPath(id)
	method := "GET"
	if a.platform == "newapi" {
		method = "POST"
		path += "/key"
	}
	var k struct {
		Key string
		ID  int
	}
	if e := a.call(ctx, method, path, nil, &k, nil); e != nil {
		return "", e
	}
	if k.Key == "" || (a.platform == "sub2api" && k.ID != id) {
		return "", remoteError("upstream_invalid_response")
	}
	return k.Key, nil
}
func (a *upstreamAdapter) DeleteKey(ctx context.Context, id int) error {
	return a.call(ctx, "DELETE", a.keyPath(id), nil, nil, nil)
}
func (a *upstreamAdapter) Snapshot(ctx context.Context) (UpstreamSnapshot, error) {
	if a.platform == "sub2api" {
		return a.sub2Snapshot(ctx)
	}
	return a.newAPISnapshot(ctx)
}
func (a *upstreamAdapter) CreateKey(ctx context.Context, in UpstreamKeyInput) (UpstreamRemoteKey, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return UpstreamRemoteKey{}, remoteError("invalid_key_name")
	}
	if a.platform == "sub2api" {
		return a.createSub2Key(ctx, in)
	}
	return a.createNewAPIKey(ctx, in)
}
func (a *upstreamAdapter) UpdateKey(ctx context.Context, id int, in UpstreamKeyInput) error {
	if a.platform == "sub2api" {
		return a.updateSub2Key(ctx, id, in)
	}
	return a.updateNewAPIKey(ctx, id, in)
}
func (a *upstreamAdapter) SetKeyEnabled(ctx context.Context, id int, enabled bool) error {
	if a.platform == "sub2api" {
		status := "inactive"
		if enabled {
			status = "active"
		}
		return a.call(ctx, "PUT", a.keyPath(id), map[string]any{"status": status}, nil, nil)
	}
	status := 2
	if enabled {
		status = 1
	}
	return a.call(ctx, "PUT", "/api/token/?status_only=1", map[string]int{"id": id, "status": status}, nil, nil)
}
func (a *upstreamAdapter) Login(ctx context.Context, user, password, uid string, oldID int, save func(UpstreamCredential) error) (string, error) {
	if a.platform == "sub2api" {
		cred, flow, err := a.sub2Auth(ctx, "/api/v1/auth/login", map[string]string{"email": user, "password": password})
		if err != nil || flow != "" {
			return flow, err
		}
		return "", save(cred)
	}
	return a.newAPILogin(ctx, user, password, uid, oldID, save)
}
func (a *upstreamAdapter) Complete(ctx context.Context, flow, code, uid string, oldID int, save func(UpstreamCredential) error) error {
	if a.platform == "sub2api" {
		cred, _, err := a.sub2Auth(ctx, "/api/v1/auth/login/2fa", map[string]string{"temp_token": flow, "totp_code": code})
		if err != nil {
			return err
		}
		return save(cred)
	}
	var session newAPILoginSession
	if err := a.call(ctx, "POST", "/api/user/login/verify", map[string]string{"flow_token": flow, "code": code}, &session, nil); err != nil {
		return err
	}
	return a.issuePAT(ctx, session.AccessToken, uid, oldID, map[string]any{"method": "2fa", "code": code}, save)
}
