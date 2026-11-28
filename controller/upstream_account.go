package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func upstreamFailure(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "code": code, "message": code})
}

func writeUpstreamResult(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		upstreamFailure(c, 404, "not_found")
		return
	}
	// Errors from transports and databases can contain URLs, credentials or SQL.
	// Expose only codes in the local contract.
	code := err.Error()
	status := http.StatusInternalServerError
	switch code {
	case "remote_key_already_bound", "remote_key_locate_ambiguous", "upstream_account_changed", "channel_changed", "credential_undecryptable", "reauth_required", "account_inactive", "remote_pat_limit":
		status = http.StatusConflict
	case "remote_key_not_found":
		status = http.StatusNotFound
	case "channel_key_mismatch", "invalid_base_url", "invalid_proxy_url", "invalid_upstream_request", "unsupported_platform", "challenge_invalid", "invalid_ratio_sync_factor", "invalid_key_name", "invalid_account_name", "invalid_credentials", "invalid_expires_at", "invalid_expired_time", "invalid_verification_code", "upstream_channel_base_url_required":
		status = http.StatusBadRequest
	case "challenge_limit", "upstream_rate_limited":
		status = http.StatusTooManyRequests
		var remote *service.UpstreamRemoteError
		if errors.As(err, &remote) && remote.RetryAfter > 0 {
			c.Header("Retry-After", strconv.FormatInt(int64((remote.RetryAfter+time.Second-1)/time.Second), 10))
		}
	case "upstream_sync_deadline":
		status = http.StatusGatewayTimeout
	case "upstream_pagination_limit_exceeded", "upstream_invalid_response", "upstream_pagination_incomplete", "upstream_timeout", "upstream_unreachable", "upstream_auth_failed", "upstream_request_rejected", "upstream_refresh_unavailable", "token_expired":
		status = http.StatusBadGateway
	case "persistent_secret_required":
		status = http.StatusServiceUnavailable
	default:
		var remote *service.UpstreamRemoteError
		if errors.As(err, &remote) {
			code = "upstream_remote_error"
			status = http.StatusBadGateway
		} else {
			code = "upstream_operation_failed"
		}
	}
	upstreamFailure(c, status, code)
}

func upstreamBody(c *gin.Context, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	dec.DisallowUnknownFields()
	// Decode through a pointer so JSON null cannot pass as an empty object.
	if err := dec.Decode(&dst); err != nil || dst == nil || dec.Decode(&struct{}{}) != io.EOF {
		upstreamFailure(c, 400, "invalid_upstream_request")
		return false
	}
	return true
}

func upstreamIDs(c *gin.Context, extra string) (int, int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		upstreamFailure(c, 400, "invalid_id")
		return 0, 0, false
	}
	if extra == "" {
		return id, 0, true
	}
	child, err := strconv.Atoi(c.Param(extra))
	if err != nil || child <= 0 {
		upstreamFailure(c, 400, "invalid_id")
		return 0, 0, false
	}
	return id, child, true
}

func RequireUpstreamChannelWrite(c *gin.Context) {
	if !middleware.CanWithAccessToken(c, authz.ChannelSensitiveWrite) {
		upstreamFailure(c, 403, "channel_sensitive_write_required")
		return
	}
	c.Next()
}

func upstreamAccountView(a *model.UpstreamAccount) (gin.H, error) {
	groups := []service.UpstreamGroup{}
	subscriptions := []service.UpstreamSubscription{}
	var meta service.UpstreamSiteMeta
	for _, snapshot := range []struct {
		raw  string
		dest any
	}{{a.GroupsJSON, &groups}, {a.SubscriptionsJSON, &subscriptions}, {a.SiteMetaJSON, &meta}} {
		if snapshot.raw != "" {
			if err := common.UnmarshalJsonStr(snapshot.raw, snapshot.dest); err != nil {
				return nil, err
			}
		}
	}
	if groups == nil {
		groups = []service.UpstreamGroup{}
	}
	if subscriptions == nil {
		subscriptions = []service.UpstreamSubscription{}
	}
	return gin.H{"id": a.ID, "name": a.Name, "platform": a.Platform, "base_url": a.BaseURL, "has_proxy": a.ProxyURL != "", "status": a.Status, "last_error": a.LastError, "last_sync_at": a.LastSyncAt, "next_sync_at": a.NextSyncAt, "upstream_groups": groups, "subscriptions": subscriptions, "site_meta": meta}, nil
}

func ListManagedUpstreams(c *gin.Context) {
	rows, err := model.ListUpstreamAccounts()
	views := make([]gin.H, 0, len(rows))
	if err == nil {
		for i := range rows {
			var v gin.H
			v, err = upstreamAccountView(&rows[i])
			if err != nil {
				break
			}
			views = append(views, v)
		}
	}
	writeUpstreamResult(c, views, err)
}
func GetManagedUpstream(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if !ok {
		return
	}
	row, err := model.GetUpstreamAccount(id)
	if err != nil {
		writeUpstreamResult(c, nil, err)
		return
	}
	v, err := upstreamAccountView(row)
	writeUpstreamResult(c, v, err)
}
func ListUpstreamBindingsForChannels(c *gin.Context) {
	parts := strings.Split(c.Query("channel_ids"), ",")
	if len(parts) > 500 {
		upstreamFailure(c, 400, "invalid_id")
		return
	}
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id < 1 {
			upstreamFailure(c, 400, "invalid_id")
			return
		}
		ids = append(ids, id)
	}
	bindings, err := model.ListUpstreamBindingsForChannels(ids)
	writeUpstreamResult(c, bindings, err)
}
func ProbeUpstream(c *gin.Context) {
	var body struct {
		Platform string `json:"platform"`
		BaseURL  string `json:"base_url"`
		ProxyURL string `json:"proxy_url"`
	}
	if upstreamBody(c, &body) {
		caps, err := service.ProbeUpstream(c.Request.Context(), body.Platform, body.BaseURL, body.ProxyURL)
		writeUpstreamResult(c, caps, err)
	}
}
func StartUpstreamAuth(c *gin.Context) {
	var body service.UpstreamAuthRequest
	if !upstreamBody(c, &body) {
		return
	}
	result, err := service.StartUpstreamAuth(c.Request.Context(), c.GetInt("id"), body)
	writeUpstreamResult(c, result, err)
}
func CompleteUpstreamAuth(c *gin.Context) {
	var body struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	result, err := service.CompleteUpstreamAuth(c.Request.Context(), c.GetInt("id"), body.Challenge, body.Code)
	writeUpstreamResult(c, result, err)
}
func EditManagedUpstream(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if !ok {
		return
	}
	var body struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	if body.Name == nil && body.Enabled == nil {
		upstreamFailure(c, 400, "invalid_upstream_request")
		return
	}
	writeUpstreamResult(c, nil, service.UpdateManagedUpstream(id, body.Name, nil, nil, body.Enabled))
}
func EditManagedEndpoint(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if !ok {
		return
	}
	var body struct {
		BaseURL  *string `json:"base_url"`
		ProxyURL *string `json:"proxy_url"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	if body.BaseURL == nil && body.ProxyURL == nil {
		upstreamFailure(c, 400, "invalid_upstream_request")
		return
	}
	writeUpstreamResult(c, nil, service.UpdateManagedUpstream(id, nil, body.BaseURL, body.ProxyURL, nil))
}
func DeleteManagedUpstream(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if ok {
		writeUpstreamResult(c, nil, service.DeleteManagedUpstream(id))
	}
}
func SyncManagedUpstream(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if ok {
		writeUpstreamResult(c, nil, service.SyncUpstreamAccount(c.Request.Context(), id))
	}
}
func ListManagedKeys(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if !ok {
		return
	}
	keys, err := service.ListManagedKeys(c.Request.Context(), id)
	writeUpstreamResult(c, keys, err)
}
func CreateManagedKey(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if !ok {
		return
	}
	var body service.UpstreamKeyInput
	if !upstreamBody(c, &body) {
		return
	}
	key, err := service.CreateManagedKey(c.Request.Context(), id, body)
	writeUpstreamResult(c, key, err)
}
func EditManagedKey(c *gin.Context) {
	id, key, ok := upstreamIDs(c, "key_id")
	if !ok {
		return
	}
	var body service.UpstreamKeyInput
	if upstreamBody(c, &body) {
		writeUpstreamResult(c, nil, service.UpdateManagedKey(c.Request.Context(), id, key, body))
	}
}
func ToggleManagedKey(c *gin.Context) {
	id, key, ok := upstreamIDs(c, "key_id")
	if !ok {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	if body.Enabled == nil {
		upstreamFailure(c, 400, "invalid_upstream_request")
		return
	}
	writeUpstreamResult(c, nil, service.SetManagedKeyEnabled(c.Request.Context(), id, key, *body.Enabled))
}
func DeleteManagedKey(c *gin.Context) {
	id, key, ok := upstreamIDs(c, "key_id")
	if ok {
		writeUpstreamResult(c, nil, service.DeleteManagedKey(c.Request.Context(), id, key))
	}
}
func ListManagedBindings(c *gin.Context) {
	id, _, ok := upstreamIDs(c, "")
	if !ok {
		return
	}
	rows, err := model.ListUpstreamBindings(id)
	writeUpstreamResult(c, rows, err)
}
func BindManagedChannel(c *gin.Context) {
	id, key, ok := upstreamIDs(c, "key_id")
	if !ok {
		return
	}
	var body struct {
		ChannelID int `json:"channel_id"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	if body.ChannelID < 1 {
		upstreamFailure(c, 400, "invalid_id")
		return
	}
	writeUpstreamResult(c, nil, service.BindManagedChannel(c.Request.Context(), id, key, body.ChannelID))
}
func CreateManagedChannel(c *gin.Context) {
	extra := ""
	if c.Param("key_id") != "" {
		extra = "key_id"
	}
	id, key, ok := upstreamIDs(c, extra)
	if !ok {
		return
	}
	var body struct {
		Name    string                    `json:"name"`
		Models  string                    `json:"models"`
		BaseURL string                    `json:"base_url"`
		NewKey  *service.UpstreamKeyInput `json:"new_key"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	if (key == 0) != (body.NewKey != nil) {
		upstreamFailure(c, 400, "invalid_upstream_request")
		return
	}
	channelID, err := service.CreateManagedChannel(c.Request.Context(), id, key, body.NewKey, body.Name, body.Models, body.BaseURL)
	writeUpstreamResult(c, gin.H{"channel_id": channelID}, err)
}
func EditManagedBinding(c *gin.Context) {
	id, ch, ok := upstreamIDs(c, "channel_id")
	if !ok {
		return
	}
	var body struct {
		Enabled *bool   `json:"ratio_sync_enabled"`
		Factor  *string `json:"ratio_sync_factor"`
	}
	if !upstreamBody(c, &body) {
		return
	}
	if body.Enabled == nil && body.Factor == nil {
		upstreamFailure(c, 400, "invalid_upstream_request")
		return
	}
	writeUpstreamResult(c, nil, service.UpdateManagedBinding(c.Request.Context(), id, ch, body.Enabled, body.Factor))
}
func ReverifyManagedBinding(c *gin.Context) {
	id, ch, ok := upstreamIDs(c, "channel_id")
	if ok {
		writeUpstreamResult(c, nil, service.ReverifyManagedBinding(c.Request.Context(), id, ch))
	}
}
func DeleteManagedBinding(c *gin.Context) {
	id, ch, ok := upstreamIDs(c, "channel_id")
	if ok {
		writeUpstreamResult(c, nil, service.DeleteManagedBinding(id, ch))
	}
}

type upstreamAccountSyncHandler struct{}

func (upstreamAccountSyncHandler) Type() string            { return model.SystemTaskTypeUpstreamAccountSync }
func (upstreamAccountSyncHandler) Enabled() bool           { return true }
func (upstreamAccountSyncHandler) Interval() time.Duration { return time.Minute }
func (upstreamAccountSyncHandler) NewPayload() any         { return nil }
func (upstreamAccountSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	err := service.SyncDueUpstreams(ctx)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, nil, err)
}
