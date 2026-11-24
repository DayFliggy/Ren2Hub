package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var managedAccountLocks = func() (locks [127]chan struct{}) {
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
	}
	return
}()

func managedAccountLock(id int) chan struct{} {
	return managedAccountLocks[uint(id)%uint(len(managedAccountLocks))]
}

func lockManagedAccount(id int) func() {
	lock := managedAccountLock(id)
	lock <- struct{}{}
	return func() { <-lock }
}

type upstreamRotation struct {
	previous string
	cred     UpstreamCredential
}

var pendingRotations sync.Map // account ID -> upstreamRotation, serialized by lockManagedAccount
func adapterForAccount(ctx context.Context, a *model.UpstreamAccount, force bool) (*upstreamAdapter, error) {
	if a.Status != "active" {
		return nil, errors.New("account_inactive")
	}
	c, err := credentialForAccount(a)
	if err != nil {
		return nil, err
	}
	adapter, err := newUpstreamAdapter(a.Platform, a.BaseURL, a.ProxyURL, c)
	if err != nil || a.Platform != "sub2api" {
		return adapter, err
	}
	pending, ok := pendingRotations.Load(a.ID)
	if ok && pending.(upstreamRotation).previous != a.CredentialCiphertext {
		pendingRotations.Delete(a.ID)
		ok = false
	}
	now := time.Now()
	if !ok && !force && c.AccessExpiresAt >= now.Add(2*time.Hour).Unix() && c.RefreshExpiresAt >= now.Add(7*24*time.Hour).Unix() {
		return adapter, nil
	}
	if ok {
		c = pending.(upstreamRotation).cred
	} else {
		c, err = adapter.Refresh(ctx)
		if err != nil {
			return nil, err
		}
		pendingRotations.Store(a.ID, upstreamRotation{a.CredentialCiphertext, c})
	}
	ciphertext, err := sealUpstreamCredential(a, c)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 3; i++ {
		err = model.UpdateUpstreamAccountCAS(a.ID, a.Version, map[string]any{"credential_ciphertext": ciphertext})
		if err == nil {
			a.Version++
			a.CredentialCiphertext = ciphertext
			pendingRotations.Delete(a.ID)
			adapter.cred = c
			return adapter, nil
		}
		if errors.Is(err, model.ErrUpstreamAccountConflict) || ctx.Err() != nil {
			break
		}
		time.Sleep(time.Duration(i+1) * 10 * time.Millisecond)
	}
	common.SysError(fmt.Sprintf("upstream refresh persistence failed account_id=%d", a.ID))
	return nil, err
}
func withManagedAdapter[T any](ctx context.Context, id int, run func(*upstreamAdapter) (T, error)) (zero T, err error) {
	defer lockManagedAccount(id)()
	a, err := model.GetUpstreamAccount(id)
	if err != nil {
		return
	}
	ad, err := adapterForAccount(ctx, a, false)
	if err != nil {
		return
	}
	v, err := run(ad)
	if err != nil && err.Error() == "token_expired" && a.Platform == "sub2api" {
		ad, err = adapterForAccount(ctx, a, true)
		if err != nil {
			return zero, err
		}
		return run(ad)
	}
	return v, err
}
func ListManagedKeys(ctx context.Context, id int) ([]UpstreamRemoteKey, error) {
	return withManagedAdapter(ctx, id, func(a *upstreamAdapter) ([]UpstreamRemoteKey, error) { return a.ListKeys(ctx) })
}
func CreateManagedKey(ctx context.Context, id int, in UpstreamKeyInput) (UpstreamRemoteKey, error) {
	return withManagedAdapter(ctx, id, func(a *upstreamAdapter) (UpstreamRemoteKey, error) { return a.CreateKey(ctx, in) })
}
func UpdateManagedKey(ctx context.Context, id, keyID int, in UpstreamKeyInput) error {
	_, err := withManagedAdapter(ctx, id, func(a *upstreamAdapter) (bool, error) { return false, a.UpdateKey(ctx, keyID, in) })
	return err
}
func SetManagedKeyEnabled(ctx context.Context, id, keyID int, enabled bool) error {
	_, err := withManagedAdapter(ctx, id, func(a *upstreamAdapter) (bool, error) { return false, a.SetKeyEnabled(ctx, keyID, enabled) })
	return err
}
func DeleteManagedKey(ctx context.Context, id, keyID int) error {
	_, err := withManagedAdapter(ctx, id, func(a *upstreamAdapter) (bool, error) { return false, a.DeleteKey(ctx, keyID) })
	return err
}
func UpdateManagedUpstream(id int, name, baseURL, proxyURL *string, enabled *bool) error {
	defer lockManagedAccount(id)()
	a, err := model.GetUpstreamAccount(id)
	if err != nil {
		return err
	}
	updates := map[string]any{}
	if name != nil {
		v := strings.TrimSpace(*name)
		if v == "" || len(v) > 128 {
			return errors.New("invalid_account_name")
		}
		updates["name"] = v
	}
	if enabled != nil {
		if !*enabled {
			updates["status"] = "disabled"
		} else if a.Status == "disabled" && a.CredentialCiphertext != "" {
			updates["status"] = "active"
		} else if a.Status != "active" {
			return errors.New("reauth_required")
		}
	}
	if baseURL != nil && strings.TrimRight(*baseURL, "/") != a.BaseURL {
		a.BaseURL = strings.TrimRight(*baseURL, "/")
		updates["base_url"] = a.BaseURL
	}
	if proxyURL != nil && *proxyURL != a.ProxyURL {
		a.ProxyURL = *proxyURL
		updates["proxy_url"] = a.ProxyURL
	}
	if err = validateUpstreamEndpoint(a.Platform, a.BaseURL, a.ProxyURL); err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}
	err = model.UpdateUpstreamAccountCAS(a.ID, a.Version, updates)
	if err == nil && (updates["base_url"] != nil || updates["proxy_url"] != nil) {
		pendingRotations.Delete(id)
	}
	return err
}
func DeleteManagedUpstream(id int) error {
	defer lockManagedAccount(id)()
	err := model.DeleteUpstreamAccount(id)
	if err == nil {
		pendingRotations.Delete(id)
	}
	return err
}
func DeleteManagedBinding(id, channelID int) error {
	defer lockManagedAccount(id)()
	return model.DeleteUpstreamBinding(id, channelID)
}
func UpdateManagedBinding(id, channelID int, enabled *bool, factor *string) error {
	defer lockManagedAccount(id)()
	updates := map[string]any{}
	if enabled != nil {
		updates["ratio_sync_enabled"] = *enabled
	}
	if factor != nil {
		v, err := model.NormalizeChannelRatio(*factor)
		if err != nil {
			return errors.New("invalid_ratio_sync_factor")
		}
		updates["ratio_sync_factor"] = v
	}
	rows, err := model.ListUpstreamBindings(id)
	if err != nil {
		return err
	}
	for _, b := range rows {
		if b.ChannelID == channelID {
			return model.UpdateUpstreamBinding(b.ID, updates)
		}
	}
	return gorm.ErrRecordNotFound
}
func normalizeNewAPIKey(k string) string { return strings.TrimPrefix(strings.TrimSpace(k), "sk-") }
func BindManagedChannel(ctx context.Context, id, keyID, channelID int) error {
	_, err := withManagedAdapter(ctx, id, func(ad *upstreamAdapter) (bool, error) {
		key, err := ad.GetKey(ctx, keyID)
		if err != nil {
			return false, err
		}
		var raw string
		if ad.platform == "newapi" {
			raw, err = ad.Reveal(ctx, keyID)
			if err != nil {
				return false, err
			}
		}
		ch, err := model.GetChannelById(channelID, true)
		if err != nil {
			return false, err
		}
		b := &model.UpstreamChannelBinding{AccountID: id, RemoteKeyID: keyID, ChannelID: channelID, ChannelCreatedTime: ch.CreatedTime, RemoteStatus: key.Status}
		return false, model.BindUpstreamChannel(b, func(current *model.Channel) error {
			sum := sha256.Sum256([]byte(current.Key))
			if ad.platform == "sub2api" && hex.EncodeToString(sum[:]) != key.KeySHA256 || ad.platform == "newapi" && normalizeNewAPIKey(current.Key) != normalizeNewAPIKey(raw) {
				return errors.New("channel_key_mismatch")
			}
			b.ChannelFP = upstreamRatioFingerprint(current)
			return nil
		})
	})
	return err
}
func CreateManagedChannel(ctx context.Context, id, keyID int, newKey *UpstreamKeyInput, name, models, baseURL string) (int, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(models) == "" {
		return 0, errors.New("invalid_upstream_request")
	}
	// Creating a key has remote side effects. Never replay the composite operation.
	defer lockManagedAccount(id)()
	a, err := model.GetUpstreamAccount(id)
	if err != nil {
		return 0, err
	}
	ad, err := adapterForAccount(ctx, a, false)
	if err != nil {
		return 0, err
	}
	if baseURL != "" {
		if !sameUpstreamOrigin(a.BaseURL, baseURL) {
			return 0, errors.New("invalid_base_url")
		}
		if err := validateUpstreamEndpoint(a.Platform, baseURL, a.ProxyURL); err != nil {
			return 0, err
		}
	}
	snapshot, err := ad.Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	var key UpstreamRemoteKey
	if newKey != nil {
		key, err = ad.CreateKey(ctx, *newKey)
	} else {
		key, err = ad.GetKey(ctx, keyID)
	}
	if err != nil {
		return 0, err
	}
	if baseURL == "" {
		baseURL = a.BaseURL
		if a.Platform == "sub2api" {
			platform := ""
			for _, g := range snapshot.Groups {
				if g.ID == key.UpstreamGroupID {
					platform = g.Platform
					break
				}
			}
			switch platform {
			case "antigravity":
				baseURL += "/antigravity"
			case "openai", "anthropic", "gemini", "composite":
			default:
				return 0, errors.New("upstream_channel_base_url_required")
			}
		}
	}
	raw, err := ad.Reveal(ctx, key.ID)
	if err != nil {
		return 0, err
	}
	kind := constant.ChannelTypeSub2API
	if a.Platform == "newapi" {
		kind = constant.ChannelTypeNewAPI
		raw = "sk-" + normalizeNewAPIKey(raw)
	}
	ch := &model.Channel{Type: kind, Name: name, Models: models, BaseURL: &baseURL, Key: raw, Status: common.ChannelStatusEnabled, CreatedTime: time.Now().Unix()}
	if key.Status != "active" {
		ch.Status = common.ChannelStatusAutoDisabled
		ch.SetOtherInfo(map[string]any{"status_reason": "Upstream key " + key.Status, "status_time": time.Now().Unix()})
	}
	if a.ProxyURL != "" {
		data, err := common.Marshal(map[string]string{"proxy": a.ProxyURL})
		if err != nil {
			return 0, err
		}
		v := string(data)
		ch.Setting = &v
	}
	b := &model.UpstreamChannelBinding{AccountID: id, RemoteKeyID: key.ID, RemoteStatus: key.Status}
	err = model.CreateBoundUpstreamChannel(b, ch, upstreamRatioFingerprint)
	return ch.Id, err
}
func sameUpstreamOrigin(base, channel string) bool {
	a, e := url.Parse(base)
	if e != nil {
		return false
	}
	b, e := url.Parse(channel)
	return e == nil && strings.EqualFold(a.Host, b.Host) && a.Scheme == b.Scheme
}

func SyncDueUpstreams(ctx context.Context) error { return RunUpstreamAccountSyncCycle(ctx) }

func RunUpstreamAccountSyncCycle(ctx context.Context) error {
	if err := model.CheckUpstreamBindingIdentities(ctx, 0); err != nil {
		return err
	}
	rows, err := model.GetDueUpstreamAccounts(time.Now().Unix(), 20)
	if err != nil {
		return err
	}
	var failures []error
	for _, a := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := SyncUpstreamAccount(ctx, a.ID); err != nil {
			failures = append(failures, fmt.Errorf("account %d: %w", a.ID, err))
		}
	}
	return errors.Join(append(failures, refreshSub2APIBoundRatios(ctx))...)
}
func SyncUpstreamAccount(ctx context.Context, id int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	lock := managedAccountLock(id)
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return remoteError("upstream_sync_deadline")
		}
		return ctx.Err()
	}
	a, err := model.GetUpstreamAccount(id)
	if err != nil {
		return err
	}
	if err := model.CheckUpstreamBindingIdentities(ctx, id); err != nil {
		return err
	}
	if a.Status != "active" {
		return errors.New("account_inactive")
	}
	ad, err := adapterForAccount(ctx, a, false)
	if err == nil {
		err = syncManagedAccount(ctx, a, ad)
	}
	if err != nil && err.Error() == "token_expired" && a.Platform == "sub2api" {
		ad, err = adapterForAccount(ctx, a, true)
		if err == nil {
			err = syncManagedAccount(ctx, a, ad)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && err != nil {
		err = remoteError("upstream_sync_deadline")
	}
	if err == nil {
		return nil
	}
	var remote *UpstreamRemoteError
	delay := 5 * time.Minute
	code := "upstream_sync_failed"
	if errors.As(err, &remote) {
		code = remote.Code
		if remote.RetryAfter > delay {
			delay = remote.RetryAfter
		}
	}
	// Decryption failure has already published a terminal status.
	if err.Error() != "credential_undecryptable" {
		saveErr := model.UpdateUpstreamAccountCAS(id, a.Version, map[string]any{"next_sync_at": time.Now().Add(delay).Unix(), "last_error": code})
		if saveErr != nil {
			return errors.Join(err, saveErr)
		}
	}
	return err
}
func syncManagedAccount(ctx context.Context, a *model.UpstreamAccount, ad *upstreamAdapter) error {
	snapshot, err := ad.Snapshot(ctx)
	if err != nil {
		return err
	}
	keys, err := ad.ListKeys(ctx)
	if err != nil {
		return err
	}
	bindings, err := model.ListUpstreamBindings(a.ID)
	if err != nil {
		return err
	}
	byID := make(map[int]UpstreamRemoteKey, len(keys))
	for _, k := range keys {
		byID[k.ID] = k
	}
	// Resolve every absence before the first write. Even the last confirmation
	// can be rate-limited or return an inconclusive proxy response.
	for _, b := range bindings {
		if b.Stale {
			continue
		}
		if _, ok := byID[b.RemoteKeyID]; ok {
			continue
		}
		k, e := ad.GetKey(ctx, b.RemoteKeyID)
		if e != nil {
			if e.Error() != "remote_key_not_found" {
				return e
			}
			k = UpstreamRemoteKey{ID: b.RemoteKeyID, Status: "missing"}
		}
		byID[k.ID] = k
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, b := range bindings {
		if b.Stale {
			continue
		}
		if err := syncUpstreamBinding(a, b, byID[b.RemoteKeyID], snapshot); err != nil {
			return err
		}
	}
	groups, err := common.Marshal(snapshot.Groups)
	if err != nil {
		return err
	}
	subs, err := common.Marshal(snapshot.Subscriptions)
	if err != nil {
		return err
	}
	meta, err := common.Marshal(snapshot.UpstreamSiteMeta)
	if err != nil {
		return err
	}
	return model.UpdateUpstreamAccountCAS(a.ID, a.Version, map[string]any{"groups_json": string(groups), "subscriptions_json": string(subs), "site_meta_json": string(meta), "last_sync_at": time.Now().Unix(), "next_sync_at": time.Now().Add(30 * time.Minute).Unix(), "last_error": ""})
}

func syncUpstreamBinding(a *model.UpstreamAccount, b model.UpstreamChannelBinding, k UpstreamRemoteKey, s UpstreamSnapshot) error {
	if b.Stale || a.Status != "active" {
		return nil
	}
	verify := b.Verify
	ch, err := model.UpdateChannelLocked(b.ChannelID, func(ch *model.Channel) error {
		if err := verify(ch); err != nil {
			return err
		}
		info := map[string]any{}
		if ch.OtherInfo != "" {
			if err := common.UnmarshalJsonStr(ch.OtherInfo, &info); err != nil {
				return err
			}
			if info == nil {
				info = map[string]any{}
			}
		}
		reason, _ := info["status_reason"].(string)
		switch k.Status {
		case "active":
			if ch.Status != common.ChannelStatusAutoDisabled || !strings.HasPrefix(reason, "Upstream key ") {
				return nil
			}
			ch.Status = common.ChannelStatusEnabled
			delete(info, "status_reason")
		case "inactive", "expired", "exhausted", "missing":
			if ch.Status != common.ChannelStatusEnabled {
				return nil
			}
			ch.Status = common.ChannelStatusAutoDisabled
			info["status_reason"] = "Upstream key " + k.Status
		default:
			return errors.New("upstream_invalid_response")
		}
		info["status_time"] = time.Now().Unix()
		ch.SetOtherInfo(info)
		return nil
	})
	if err != nil {
		return handleBindingError(b, err)
	}
	updates := map[string]any{"remote_status": k.Status}
	if k.Status == "active" {
		rec, err := managedRatioRecord(a, ch, b, k, s, verify)
		if err != nil {
			if errors.Is(err, model.ErrUpstreamRatioConflict) {
				return err
			}
			return handleBindingError(b, err)
		}
		if b.RatioSyncEnabled {
			code, err := writeBoundRatio(b, ch, rec, time.Now())
			if err != nil {
				return handleBindingError(b, err)
			}
			updates["ratio_sync_error"] = code
		}
	}
	return model.UpdateUpstreamBinding(b.ID, updates)
}
func managedRatioRecord(a *model.UpstreamAccount, ch *model.Channel, b model.UpstreamChannelBinding, k UpstreamRemoteKey, s UpstreamSnapshot, verify func(*model.Channel) error) (*model.ChannelUpstreamRatio, error) {
	defer lockUpstreamRatioChannel(ch.Id)()
	rec, err := model.GetChannelUpstreamRatio(ch.Id)
	if err != nil {
		return nil, err
	}
	if a.Platform != "newapi" {
		return rec, nil
	}
	if rec == nil || !rec.BelongsTo(ch) {
		rec = newUpstreamRatioRecord(ch.Id)
	}
	if rec.Mode == model.UpstreamRatioModeManual {
		return rec, nil
	}
	group := k.UpstreamGroup
	if group == "" {
		group = s.UserGroup
	}
	rec.GroupName, rec.Mapping, rec.TokenName = group, "account", k.Name
	rec.CredentialFP, rec.ChannelCreatedTime, rec.Source = b.ChannelFP, ch.CreatedTime, model.UpstreamRatioSourceNewAPI
	rec.Status, rec.Ratio = model.UpstreamRatioStatusNeedsMapping, ""
	detail := NewAPIDetail{}
	for _, g := range s.Groups {
		row := NewAPIGroup{Name: g.Name}
		if r, ok := model.ParseBoundedRatio(g.Ratio); ok {
			f, _ := r.Float64()
			row.Ratio = &f
			if g.Name == group && group != "auto" {
				rec.Ratio = r.String()
				rec.Status = model.UpstreamRatioStatusOK
			}
		}
		detail.Groups = append(detail.Groups, row)
	}
	data, err := common.Marshal(detail)
	if err != nil {
		return nil, err
	}
	if len(data) > 65535 {
		return nil, errors.New("detail_too_large")
	}
	rec.Detail = string(data)
	now := time.Now()
	rec.ObservedAt, rec.ReceivedAt, rec.UpdatedAt = now.Unix(), now.Unix(), now.Unix()
	rec.FreshUntil = now.Add(time.Hour).Unix()
	rec.NextProbeAt = now.Add(30 * time.Minute).Unix()
	rec.LastError = ""
	rec.FailureCount = 0
	rec.HTTPStatus = 200
	err = model.SaveChannelUpstreamRatio(rec, rec.Version, verify)
	return rec, err
}
func upstreamTargetRatio(view ChannelUpstreamRatioView, factor string) (string, string) {
	if view.Stale || (view.Mode != model.UpstreamRatioModeManual && view.Status != model.UpstreamRatioStatusOK) {
		return "", "upstream_ratio_unavailable"
	}
	r, ok := model.ParseBoundedRatio(view.EffectiveRatio)
	if !ok || !r.IsPositive() {
		return "", "upstream_ratio_unavailable"
	}
	f, ok := model.ParseBoundedRatio(factor)
	if !ok {
		return "", "ratio_out_of_range"
	}
	v, err := model.NormalizeChannelRatio(r.Mul(f).Round(8).String())
	if err != nil {
		return "", "ratio_out_of_range"
	}
	return v, ""
}
