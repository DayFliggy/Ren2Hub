package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

func (a *upstreamAdapter) sub2Auth(ctx context.Context, path string, body map[string]string) (UpstreamCredential, string, error) {
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Requires2FA  bool   `json:"requires_2fa"`
		TempToken    string `json:"temp_token"`
	}
	if err := a.call(ctx, "POST", path, body, &r, nil); err != nil {
		return UpstreamCredential{}, "", err
	}
	if r.Requires2FA && r.TempToken != "" && path == "/api/v1/auth/login" {
		return UpstreamCredential{}, r.TempToken, nil
	}
	if r.AccessToken == "" || r.RefreshToken == "" || r.ExpiresIn <= 0 {
		return UpstreamCredential{}, "", remoteError("upstream_invalid_response")
	}
	now := time.Now().Unix()
	return UpstreamCredential{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken, AccessExpiresAt: now + r.ExpiresIn, RefreshExpiresAt: now + 30*86400}, "", nil
}
func (a *upstreamAdapter) Refresh(ctx context.Context) (UpstreamCredential, error) {
	if a.platform != "sub2api" || a.cred.RefreshToken == "" {
		return UpstreamCredential{}, remoteError("upstream_refresh_unavailable")
	}
	c, _, e := a.sub2Auth(ctx, "/api/v1/auth/refresh", map[string]string{"refresh_token": a.cred.RefreshToken})
	return c, e
}
func maskedRemoteKey(k string) string {
	if len(k) <= 12 {
		return "****"
	}
	return k[:6] + "****" + k[len(k)-4:]
}
func decodeSub2Key(b []byte) (UpstreamRemoteKey, error) {
	var k struct {
		ID                int
		Name, Key, Status string
		GroupID           int `json:"group_id"`
		Group             struct{ Name string }
		Quota             float64
		Used              float64    `json:"quota_used"`
		ExpiresAt         *time.Time `json:"expires_at"`
	}
	if common.Unmarshal(b, &k) != nil || k.ID < 1 || k.Status == "" || k.Key == "" {
		return UpstreamRemoteKey{}, remoteError("upstream_invalid_response")
	}
	var status string
	switch k.Status {
	case "active":
		status = "active"
		if k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()) {
			status = "expired"
		} else if k.Quota > 0 && k.Used >= k.Quota {
			status = "exhausted"
		}
	case "disabled", "inactive":
		status = "inactive"
	case "quota_exhausted":
		status = "exhausted"
	case "expired":
		status = "expired"
	default:
		return UpstreamRemoteKey{}, remoteError("upstream_invalid_response")
	}
	expiresAt := ""
	if k.ExpiresAt != nil {
		expiresAt = k.ExpiresAt.Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(k.Key))
	return UpstreamRemoteKey{ID: k.ID, Name: k.Name, Status: status, Quota: &k.Quota, QuotaUsed: &k.Used, ExpiresAt: &expiresAt, Prefix: maskedRemoteKey(k.Key), KeySHA256: hex.EncodeToString(sum[:]), UpstreamGroupID: k.GroupID, UpstreamGroup: k.Group.Name}, nil
}

type sub2KeyRequest struct {
	Name          *string  `json:"name,omitempty"`
	GroupID       *int     `json:"group_id,omitempty"`
	Quota         *float64 `json:"quota,omitempty"`
	ExpiresAt     *string  `json:"expires_at,omitempty"`
	ExpiresInDays *int     `json:"expires_in_days,omitempty"`
}

func (a *upstreamAdapter) createSub2Key(ctx context.Context, in UpstreamKeyInput) (UpstreamRemoteKey, error) {
	body := sub2KeyRequest{Name: in.Name, GroupID: in.UpstreamGroupID, Quota: in.Quota, ExpiresInDays: in.ExpiresInDays}
	h := http.Header{"Idempotency-Key": {uuid.NewString()}}
	var b common.RawMessage
	e := a.call(ctx, "POST", "/api/v1/keys", body, &b, h)
	if e != nil && e.Error() == "upstream_timeout" && ctx.Err() == nil {
		e = a.call(ctx, "POST", "/api/v1/keys", body, &b, h)
	}
	if e != nil {
		return UpstreamRemoteKey{}, e
	}
	return decodeSub2Key(b)
}
func (a *upstreamAdapter) updateSub2Key(ctx context.Context, id int, in UpstreamKeyInput) error {
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		if _, e := time.Parse(time.RFC3339, *in.ExpiresAt); e != nil {
			return remoteError("invalid_expires_at")
		}
	}
	body := sub2KeyRequest{Name: in.Name, GroupID: in.UpstreamGroupID, Quota: in.Quota, ExpiresAt: in.ExpiresAt}
	return a.call(ctx, "PUT", a.keyPath(id), body, nil, nil)
}
func (a *upstreamAdapter) sub2Snapshot(ctx context.Context) (UpstreamSnapshot, error) {
	var snap UpstreamSnapshot
	var me struct {
		Username, Email string
		Balance         common.RawMessage
	}
	if e := a.call(ctx, "GET", "/api/v1/auth/me", nil, &me, nil); e != nil {
		return snap, e
	}
	snap.Username = me.Username
	if snap.Username == "" {
		snap.Username = me.Email
	}
	balance, e := upstreamNumericValue(me.Balance, false)
	if e != nil {
		return snap, e
	}
	snap.Balance = balance
	var groups []struct {
		ID             int
		Name, Platform string
		Ratio          common.RawMessage `json:"rate_multiplier"`
	}
	if e := a.call(ctx, "GET", "/api/v1/groups/available", nil, &groups, nil); e != nil {
		return snap, e
	}
	var rates map[string]common.RawMessage
	if e := a.call(ctx, "GET", "/api/v1/groups/rates", nil, &rates, nil); e != nil {
		return snap, e
	}
	snap.Groups = make([]UpstreamGroup, 0, len(groups))
	for _, g := range groups {
		ratio := g.Ratio
		if override, ok := rates[strconv.Itoa(g.ID)]; ok {
			ratio = override
		}
		value, e := upstreamNumericValue(ratio, false)
		if e != nil {
			return snap, e
		}
		snap.Groups = append(snap.Groups, UpstreamGroup{ID: g.ID, Name: g.Name, Platform: g.Platform, Ratio: value})
	}
	var subs []struct {
		Subscription struct {
			ID        int
			GroupID   int `json:"group_id"`
			Status    string
			ExpiresAt string `json:"expires_at"`
		}
	}
	if e := a.call(ctx, "GET", "/api/v1/subscriptions/progress", nil, &subs, nil); e != nil {
		return snap, e
	}
	snap.Subscriptions = make([]UpstreamSubscription, 0, len(subs))
	for _, row := range subs {
		s := row.Subscription
		snap.Subscriptions = append(snap.Subscriptions, UpstreamSubscription{s.ID, s.GroupID, s.Status, s.ExpiresAt})
	}
	return snap, nil
}
