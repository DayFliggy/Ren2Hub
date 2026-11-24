package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type newAPILoginSession struct {
	FlowToken           string `json:"flow_token"`
	AccessToken         string `json:"access_token"`
	RequireVerification bool   `json:"require_verification"`
}

func encryptUpstreamPassword(password, kid, publicKey string) (string, error) {
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil || kid == "" {
		return "", remoteError("invalid_password_key")
	}
	parsed, e := x509.ParsePKIXPublicKey(block.Bytes)
	if e != nil {
		return "", remoteError("invalid_password_key")
	}
	pk, ok := parsed.(*rsa.PublicKey)
	if !ok || pk.N.BitLen() < 2048 || pk.N.BitLen() > 8192 {
		return "", remoteError("invalid_password_key")
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	wrapped, e := rsa.EncryptOAEP(sha256.New(), rand.Reader, pk, secret, []byte("password-v2"))
	if e != nil {
		return "", remoteError("invalid_password_key")
	}
	c, e := aes.NewCipher(secret)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(c)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	ct := g.Seal(nil, nonce, []byte(password), []byte("password-v2:"+kid))
	b64 := base64.StdEncoding.EncodeToString
	return "v2." + b64(wrapped) + "." + b64(nonce) + "." + b64(ct), nil
}
func (a *upstreamAdapter) newAPILogin(ctx context.Context, user, password, uid string, oldID int, save func(UpstreamCredential) error) (string, error) {
	var key struct {
		Enabled   *bool
		KID       string
		PublicKey string `json:"public_key"`
	}
	if e := a.call(ctx, "GET", "/api/user/login/encryption-key", nil, &key, nil); e != nil {
		return "", e
	}
	fields := map[string]any{}
	if key.Enabled != nil && !*key.Enabled {
		fields["password"] = password
	} else {
		encrypted, e := encryptUpstreamPassword(password, key.KID, key.PublicKey)
		if e != nil {
			return "", e
		}
		fields["password_encrypted"] = encrypted
		fields["encryption_key_id"] = key.KID
	}
	body := maps.Clone(fields)
	body["username"] = user
	var session newAPILoginSession
	if e := a.call(ctx, "POST", "/api/user/login", body, &session, nil); e != nil {
		return "", e
	}
	if session.RequireVerification && session.FlowToken != "" {
		return session.FlowToken, nil
	}
	fields["method"] = "password"
	return "", a.issuePAT(ctx, session.AccessToken, uid, oldID, fields, save)
}
func (a *upstreamAdapter) issuePAT(ctx context.Context, session, uid string, oldID int, auth map[string]any, save func(UpstreamCredential) error) error {
	if session == "" || len(uid) < 8 {
		return remoteError("upstream_invalid_response")
	}
	h := http.Header{"Authorization": {"Bearer " + session}, "Origin": {a.origin}}
	defer func() {
		if e := a.call(context.WithoutCancel(ctx), "POST", "/api/user/auth/logout", nil, nil, h); e != nil {
			common.SysLog("upstream session logout failed")
		}
	}()
	proof := func(ctx context.Context, scope string, params map[string]any) (http.Header, error) {
		body := maps.Clone(auth)
		body["scope"] = scope
		body["context"] = params
		var p struct {
			Token string `json:"proof_token"`
		}
		if e := a.call(ctx, "POST", "/api/verify", body, &p, h); e != nil {
			return nil, e
		}
		if p.Token == "" {
			return nil, remoteError("upstream_invalid_response")
		}
		headers := h.Clone()
		headers.Set("X-Security-Proof", p.Token)
		return headers, nil
	}
	params := map[string]any{"expires_at": 0, "scopes": []string{"profile:read", "api_key:read", "api_key:write", "api_key:reveal"}}
	headers, e := proof(ctx, "access_token.generate", params)
	if e != nil {
		return e
	}
	params["name"] = "ren2hub-" + uid[:8]
	var created struct {
		Token string
		Item  struct{ ID int }
	}
	if e = a.call(ctx, "POST", "/api/user/access_tokens", params, &created, headers); e != nil {
		return e
	}
	if created.Token == "" || created.Item.ID <= 0 {
		return remoteError("upstream_invalid_response")
	}
	cred := UpstreamCredential{PAT: created.Token, RemotePATID: created.Item.ID}
	saveErr := save(cred)
	// If local persistence fails, the previous PAT still backs the account.
	// Use the same active session to compensate by revoking the new PAT.
	revokeID := oldID
	if saveErr != nil {
		revokeID = created.Item.ID
	}
	if revokeID > 0 && (saveErr != nil || revokeID != created.Item.ID) {
		cleanupCtx := context.WithoutCancel(ctx)
		headers, e := proof(cleanupCtx, "access_token.revoke", map[string]any{"token_id": revokeID})
		if e == nil {
			e = a.call(cleanupCtx, "DELETE", fmt.Sprintf("/api/user/access_tokens/%d", revokeID), nil, nil, headers)
		}
		if e != nil {
			common.SysLog(fmt.Sprintf("upstream PAT cleanup failed: site=%s credential_uid=%s remote_pat_id=%d", a.origin, uid, revokeID))
		}
	}
	return saveErr
}
func (a *upstreamAdapter) newAPISnapshot(ctx context.Context) (UpstreamSnapshot, error) {
	var s UpstreamSnapshot
	status, err := a.publicStatus(ctx)
	if err != nil {
		return s, err
	}
	s.UpstreamQuotaDisplay = status.UpstreamQuotaDisplay
	var me struct {
		Username, Group string
		Quota           common.RawMessage
	}
	if e := a.call(ctx, "GET", "/api/user/self", nil, &me, nil); e != nil {
		return s, e
	}
	s.Username = me.Username
	s.UserGroup = me.Group
	balance, e := upstreamNumericValue(me.Quota, false)
	if e != nil {
		return s, e
	}
	s.Balance = balance
	var groups map[string]struct{ Ratio common.RawMessage }
	if e := a.call(ctx, "GET", "/api/user/self/groups", nil, &groups, nil); e != nil {
		return s, e
	}
	s.Groups = make([]UpstreamGroup, 0, len(groups))
	s.Subscriptions = []UpstreamSubscription{}
	for _, name := range slices.Sorted(maps.Keys(groups)) {
		ratio, e := upstreamNumericValue(groups[name].Ratio, true)
		if e != nil {
			return s, e
		}
		s.Groups = append(s.Groups, UpstreamGroup{Name: name, Ratio: ratio})
	}
	return s, nil
}
func decodeNewAPIKey(b []byte) (UpstreamRemoteKey, error) {
	var k struct {
		ID               int
		Name, Key, Group string
		Status           int
		ExpiredTime      *int64 `json:"expired_time"`
		Unlimited        *bool  `json:"unlimited_quota"`
		Remain           *int64 `json:"remain_quota"`
		Used             *int64 `json:"used_quota"`
	}
	if common.Unmarshal(b, &k) != nil || k.ID <= 0 || k.Status < 1 || k.Status > 4 || k.ExpiredTime == nil || k.Unlimited == nil || k.Remain == nil {
		return UpstreamRemoteKey{}, remoteError("upstream_invalid_response")
	}
	status := "active"
	switch {
	case k.Status == 2:
		status = "inactive"
	case k.Status == 3 || (*k.ExpiredTime != -1 && *k.ExpiredTime < time.Now().Unix()):
		status = "expired"
	case k.Status == 4 || (!*k.Unlimited && *k.Remain <= 0):
		status = "exhausted"
	}
	return UpstreamRemoteKey{ID: k.ID, Name: k.Name, Prefix: maskedRemoteKey(k.Key), Status: status, RemainQuota: k.Remain, UsedQuota: k.Used, UnlimitedQuota: k.Unlimited, ExpiredTime: k.ExpiredTime, UpstreamGroup: k.Group}, nil
}
func applyNewAPIKeyFields(body map[string]any, in UpstreamKeyInput) error {
	if in.Name != nil {
		if len(*in.Name) > 50 {
			return remoteError("invalid_key_name")
		}
		body["name"] = *in.Name
	}
	if in.UpstreamGroup != nil {
		body["group"] = *in.UpstreamGroup
	}
	if in.RemainQuota != nil {
		body["remain_quota"] = *in.RemainQuota
	}
	if in.UnlimitedQuota != nil {
		body["unlimited_quota"] = *in.UnlimitedQuota
	}
	if in.ExpiredTime != nil {
		if *in.ExpiredTime != -1 && *in.ExpiredTime <= time.Now().Unix() {
			return remoteError("invalid_expired_time")
		}
		body["expired_time"] = *in.ExpiredTime
	}
	return nil
}
func (a *upstreamAdapter) newestNewAPIKeys(ctx context.Context) ([]common.RawMessage, error) {
	var page struct {
		Items []common.RawMessage
		Total *int
	}
	if e := a.call(ctx, "GET", "/api/token/?p=1&page_size=20", nil, &page, nil); e != nil {
		return nil, e
	}
	if page.Total == nil || len(page.Items) == 0 && *page.Total > 0 {
		return nil, remoteError("upstream_invalid_response")
	}
	return page.Items, nil
}
func (a *upstreamAdapter) createNewAPIKey(ctx context.Context, in UpstreamKeyInput) (UpstreamRemoteKey, error) {
	var key UpstreamRemoteKey
	body := map[string]any{"expired_time": -1, "unlimited_quota": in.RemainQuota == nil}
	if e := applyNewAPIKeyFields(body, in); e != nil {
		return key, e
	}
	before, e := a.newestNewAPIKeys(ctx)
	if e != nil {
		return key, e
	}
	maxID := 0
	for _, b := range before {
		var row struct{ ID int }
		if common.Unmarshal(b, &row) != nil || row.ID < 1 {
			return key, remoteError("upstream_invalid_response")
		}
		maxID = max(maxID, row.ID)
	}
	if e = a.call(ctx, "POST", "/api/token/", body, nil, nil); e != nil {
		return key, e
	}
	after, e := a.newestNewAPIKeys(ctx)
	if e != nil {
		return key, e
	}
	matches := 0
	for _, b := range after {
		row, e := decodeNewAPIKey(b)
		if e != nil {
			return key, e
		}
		if row.ID > maxID && row.Name == *in.Name {
			key = row
			matches++
		}
	}
	if matches != 1 {
		return UpstreamRemoteKey{}, remoteError("remote_key_locate_ambiguous")
	}
	return key, nil
}
func (a *upstreamAdapter) updateNewAPIKey(ctx context.Context, id int, in UpstreamKeyInput) error {
	var existing map[string]common.RawMessage
	if e := a.call(ctx, "GET", a.keyPath(id), nil, &existing, nil); e != nil {
		return e
	}
	var got int
	if common.Unmarshal(existing["id"], &got) != nil || got != id {
		return remoteError("upstream_invalid_response")
	}
	body := map[string]any{"id": id}
	for _, field := range []string{"name", "expired_time", "remain_quota", "unlimited_quota", "model_limits_enabled", "model_limits", "allow_ips", "group", "cross_group_retry"} {
		v, ok := existing[field]
		if !ok {
			return remoteError("upstream_invalid_response")
		}
		body[field] = v
	}
	// These two fields have presence-aware update semantics on NewAPI.
	for _, field := range []string{"allow_user_quota_query", "auto_groups"} {
		if v, ok := existing[field]; ok {
			body[field] = v
		}
	}
	if e := applyNewAPIKeyFields(body, in); e != nil {
		return e
	}
	return a.call(ctx, "PUT", "/api/token/", body, nil, nil)
}
