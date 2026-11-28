package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

type syncRemoteFixture struct {
	account       model.UpstreamAccount
	binding       model.UpstreamChannelBinding
	status        atomic.Int64
	ratio         atomic.Value
	paging        atomic.Bool
	pageError     atomic.Bool
	confirmStatus atomic.Int64
	confirms      atomic.Int64
}

func newSyncFixture(t *testing.T) *syncRemoteFixture {
	t.Helper()
	db := setupUpstreamRatioTest(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.SystemTask{}))
	f := &syncRemoteFixture{}
	f.ratio.Store("1.25")
	f.status.Store(1)
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(s.Close)
	t.Setenv("CRYPTO_SECRET", "test-upstream-sync")
	f.account = model.UpstreamAccount{CredentialUID: "sync-test", Platform: "newapi", BaseURL: s.URL, Status: "active"}
	cipher, err := sealUpstreamCredential(&f.account, UpstreamCredential{PAT: "test-account-pat"})
	require.NoError(t, err)
	f.account.CredentialCiphertext = cipher
	require.NoError(t, model.CreateUpstreamAccount(&f.account))
	zero := 0
	ch := model.Channel{Id: 311, CreatedTime: 10, Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Name: "sync", BaseURL: &s.URL, Models: "sync-model", Key: "sk-test", AutoBan: &zero}
	require.NoError(t, ch.Insert())
	f.binding = model.UpstreamChannelBinding{AccountID: f.account.ID, ChannelID: ch.Id, RemoteKeyID: 17, ChannelCreatedTime: ch.CreatedTime, ChannelFP: upstreamRatioFingerprint(&ch), RatioSyncEnabled: true, RatioSyncFactor: "2", RemoteStatus: "active"}
	require.NoError(t, model.BindUpstreamChannel(&f.binding, func(*model.Channel) error { return nil }))
	return f
}

func (f *syncRemoteFixture) serve(w http.ResponseWriter, r *http.Request) {
	var data string
	key := fmt.Sprintf(`{"id":17,"name":"demo","status":%d,"expired_time":-1,"unlimited_quota":true,"remain_quota":0,"group":""}`, f.status.Load())
	switch r.URL.Path {
	case "/api/status":
		data = `{"version":"1","quota_per_unit":500000,"quota_display_type":"USD"}`
	case "/api/user/self":
		data = `{"username":"fixture","group":"test","quota":100}`
	case "/api/user/self/groups":
		data = fmt.Sprintf(`{"test":{"ratio":%s}}`, f.ratio.Load())
	case "/api/token/":
		if f.paging.Load() {
			page := r.URL.Query().Get("p")
			if page == "2" && f.pageError.Load() {
				w.WriteHeader(503)
				return
			}
			if page != "1" && page != "2" {
				http.NotFound(w, r)
				return
			}
			data = fmt.Sprintf(`{"page":%s,"page_size":1,"total":2,"items":[{"id":%s,"status":1,"expired_time":-1,"unlimited_quota":true,"remain_quota":0}]}`, page, page)
		} else {
			data = fmt.Sprintf(`{"page":1,"page_size":100,"total":1,"items":[%s]}`, key)
		}
	case "/api/token/17":
		f.confirms.Add(1)
		switch status := f.confirmStatus.Load(); status {
		case 429:
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(429)
			return
		case 404:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"success":false,"message":"record not found"}`)
			return
		default:
			data = key
		}
	case "/api/token/17/key":
		data = `{"key":"test"}`
	case "/api/pricing":
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"success":false,"message":"login required"}`)
		return
	default:
		http.NotFound(w, r)
		return
	}
	fmt.Fprintf(w, `{"success":true,"data":%s}`, data)
}

func (f *syncRemoteFixture) sync() error {
	return SyncUpstreamAccount(context.Background(), f.account.ID)
}
func (f *syncRemoteFixture) channel(t *testing.T) *model.Channel {
	t.Helper()
	ch, err := model.GetChannelById(f.binding.ChannelID, true)
	require.NoError(t, err)
	return ch
}
func (f *syncRemoteFixture) storedBinding(t *testing.T) model.UpstreamChannelBinding {
	t.Helper()
	rows, err := model.ListUpstreamBindings(f.account.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]
}

func TestUpstreamRatioAutoWritePreservesRouteObservationsAndAppliesMaxRatio(t *testing.T) {
	f := newSyncFixture(t)
	resetRouteObservations(t)
	id := f.binding.ChannelID
	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldCache
		if model.DB != nil {
			model.InitChannelCache()
		}
	})
	model.CacheUpdateChannel(f.channel(t))
	RecordRouteObservation(id, "sync-model", 1, false, true, 100)
	breaker := routeBreakerKey{id, "sync-model"}
	state := routeBreakerState{openUntil: time.Now().Add(time.Minute).UnixMilli()}
	routeObservations.breakers[breaker] = state
	require.NoError(t, f.sync())
	require.Equal(t, 1, routeObservations.get(id, "sync-model", 1, false).Samples)
	require.Equal(t, state, routeObservations.breakers[breaker])
	cache, err := model.CacheGetChannel(id)
	require.NoError(t, err)
	require.Equal(t, "2.5", cache.ChannelRatio)
	ceiling := "2"
	require.Equal(t, "channel_ratio_exceeded", routePolicyReason(cache, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{MaxChannelRatio: &ceiling}}, false))
}

func TestUpstreamSyncPaginationDriftAndSingleGetConfirmMissing(t *testing.T) {
	f := newSyncFixture(t)
	f.paging.Store(true)
	f.pageError.Store(true)
	require.Error(t, f.sync())
	require.Zero(t, f.confirms.Load())
	require.Equal(t, "active", f.storedBinding(t).RemoteStatus)
	f.pageError.Store(false)
	require.NoError(t, f.sync())
	require.EqualValues(t, 1, f.confirms.Load())
	require.Equal(t, "active", f.storedBinding(t).RemoteStatus)
	f.confirmStatus.Store(429)
	before := time.Now().Unix()
	require.Error(t, f.sync())
	require.Equal(t, common.ChannelStatusEnabled, f.channel(t).Status)
	require.Equal(t, "active", f.storedBinding(t).RemoteStatus)
	account, err := model.GetUpstreamAccount(f.account.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, account.NextSyncAt, before+600)
	f.confirmStatus.Store(404)
	require.NoError(t, f.sync())
	require.Equal(t, "missing", f.storedBinding(t).RemoteStatus)
	require.Equal(t, common.ChannelStatusAutoDisabled, f.channel(t).Status)
}

func TestUpstreamProbeBaseURLStripsAntigravity(t *testing.T) {
	base, err := upstreamProbeBaseURL("https://upstream.invalid/prefix/antigravity/")
	require.NoError(t, err)
	require.Equal(t, "https://upstream.invalid/prefix", base)
}

func TestUpstreamSyncAutoDisablesAndSafelyReenablesChannel(t *testing.T) {
	f := newSyncFixture(t)
	f.status.Store(2)
	require.NoError(t, f.sync())
	ch := f.channel(t)
	require.Equal(t, common.ChannelStatusAutoDisabled, ch.Status)
	require.Equal(t, "Upstream key inactive", ch.GetOtherInfo()["status_reason"])
	f.status.Store(1)
	require.NoError(t, f.sync())
	require.Equal(t, common.ChannelStatusEnabled, f.channel(t).Status)
	for _, status := range []int{common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled} {
		_, err := model.UpdateChannelLocked(ch.Id, func(c *model.Channel) error {
			c.Status = status
			c.SetOtherInfo(map[string]any{"status_reason": "operator"})
			return nil
		})
		require.NoError(t, err)
		require.NoError(t, f.sync())
		require.Equal(t, status, f.channel(t).Status)
		require.Equal(t, "operator", f.channel(t).GetOtherInfo()["status_reason"])
	}
}

func TestUpstreamBindingStaleFingerprintStopsAutomation(t *testing.T) {
	f := newSyncFixture(t)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", f.binding.ChannelID).Update("base_url", "https://changed.invalid").Error)
	f.status.Store(2)
	require.NoError(t, f.sync())
	require.True(t, f.storedBinding(t).Stale)
	ch := f.channel(t)
	require.Equal(t, "1", ch.ChannelRatio)
	require.Equal(t, common.ChannelStatusEnabled, ch.Status)
}

func TestUpstreamRatioAutoWriteRejectsOutOfRangeAndStaleAccount(t *testing.T) {
	f := newSyncFixture(t)
	for _, ratio := range []string{"0", "501", `"auto"`} {
		f.ratio.Store(ratio)
		require.NoError(t, f.sync())
		require.NotEmpty(t, f.storedBinding(t).RatioSyncError)
		require.Equal(t, "1", f.channel(t).ChannelRatio)
	}
	f.ratio.Store("2")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", f.account.ID).Update("status", "reauth_required").Error)
	_ = f.sync()
	require.Equal(t, "1", f.channel(t).ChannelRatio)
}

func TestUpstreamNewAPIAccountLinkedChannelSkipsDirectProbe(t *testing.T) {
	f := newSyncFixture(t)
	due, err := model.GetDueUpstreamRatioChannels(time.Now().Unix(), 100)
	require.NoError(t, err)
	require.Empty(t, due)
	require.NoError(t, f.sync())
	record, err := model.GetChannelUpstreamRatio(f.binding.ChannelID)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "account", record.Mapping)
	require.Equal(t, "1.25", record.Ratio)
	record.Mode = model.UpstreamRatioModeManual
	record.Ratio = "3"
	require.NoError(t, model.SaveChannelUpstreamRatio(record, record.Version, nil))
	require.NoError(t, f.sync())
	saved, err := model.GetChannelUpstreamRatio(f.binding.ChannelID)
	require.NoError(t, err)
	require.Equal(t, "3", saved.Ratio)
	require.Equal(t, model.UpstreamRatioModeManual, saved.Mode)
}

func TestUpstreamSub2APIPeakTransitionsUseLocalBillingSnapshot(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.SystemTask{}))
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	t.Cleanup(server.Close)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	a := model.UpstreamAccount{Platform: "sub2api", BaseURL: server.URL, Status: "active", LastSyncAt: now.Unix()}
	require.NoError(t, model.CreateUpstreamAccount(&a))
	ch := model.Channel{Type: constant.ChannelTypeSub2API, CreatedTime: 55, Models: "peak", Name: "peak", Key: "test", BaseURL: &server.URL}
	require.NoError(t, ch.Insert())
	fp := upstreamRatioFingerprint(&ch)
	b := model.UpstreamChannelBinding{AccountID: a.ID, ChannelID: ch.Id, ChannelCreatedTime: 55, ChannelFP: fp, RemoteKeyID: 2, RemoteStatus: "active", RatioSyncEnabled: true, RatioSyncFactor: "2"}
	require.NoError(t, model.BindUpstreamChannel(&b, func(*model.Channel) error { return nil }))
	rec := model.ChannelUpstreamRatio{Source: "sub2api", Mode: "auto", Status: "ok", ChannelId: ch.Id, ChannelCreatedTime: 55, CredentialFP: fp, Ratio: "1.5", FreshUntil: now.Add(2 * time.Hour).Unix(), Detail: marshalUpstreamRatioDetail(Sub2APIBilling{ResolvedRate: 1.5, PeakEnabled: true, PeakStart: "10:00", PeakEnd: "11:00", Timezone: "UTC", PeakRate: 3})}
	require.NoError(t, model.SaveChannelUpstreamRatio(&rec, 0, nil))
	ids, err := model.Sub2APIRatioAccountIDs()
	require.NoError(t, err)
	require.Equal(t, []int{a.ID}, ids)
	check := func(at time.Time, want string) {
		t.Helper()
		require.NoError(t, refreshSub2APIRatiosForAccount(context.Background(), a.ID, at))
		stored, err := model.GetChannelById(ch.Id, true)
		require.NoError(t, err)
		require.Equal(t, want, stored.ChannelRatio)
	}
	check(now.Add(-time.Second), "3")
	check(now, "9")
	check(now.Add(time.Hour), "3")
	require.NoError(t, model.UpdateUpstreamAccountCAS(a.ID, a.Version, map[string]any{"last_error": "upstream_timeout"}))
	check(now, "3")
	require.NoError(t, model.UpdateUpstreamAccountCAS(a.ID, a.Version+1, map[string]any{"last_error": ""}))
	require.NoError(t, model.UpdateUpstreamBinding(b.ID, map[string]any{"remote_status": "inactive"}))
	check(now, "3")
	require.NoError(t, model.UpdateUpstreamBinding(b.ID, map[string]any{"remote_status": "active"}))
	require.NoError(t, db.Model(&ch).Update("key", "changed").Error)
	check(now, "3")
	rows, err := model.ListUpstreamBindings(a.ID)
	require.NoError(t, err)
	require.True(t, rows[0].Stale)
	require.Zero(t, requests.Load())
}

func TestChannelUpstreamRatioFingerprintDistinguishesAntigravity(t *testing.T) {
	base := "https://site.invalid/prefix"
	channel := &model.Channel{Type: constant.ChannelTypeSub2API, BaseURL: &base, Key: "fixture-key"}
	before := upstreamRatioFingerprint(channel)
	base += "/antigravity"
	require.NotEqual(t, before, upstreamRatioFingerprint(channel))
}

func TestUpstreamNewAPIChannelChangeRestoresDueProbeWhenAccountUnreachable(t *testing.T) {
	for _, entry := range []string{"manual", "scheduled", "due probe"} {
		t.Run(entry, func(t *testing.T) {
			f := newSyncFixture(t)
			require.NoError(t, f.sync())
			saved, err := model.GetChannelUpstreamRatio(f.binding.ChannelID)
			require.NoError(t, err)
			require.Greater(t, saved.NextProbeAt, time.Now().Unix())
			f.paging.Store(true)
			f.pageError.Store(true)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", f.binding.ChannelID).Update("base_url", "https://changed.invalid").Error)
			switch entry {
			case "manual":
				require.Error(t, f.sync())
			case "scheduled":
				require.NoError(t, SyncDueUpstreams(context.Background()))
			case "due probe":
				_, err = model.GetDueUpstreamRatioChannels(time.Now().Unix(), 1)
				require.NoError(t, err)
			}
			require.True(t, f.storedBinding(t).Stale)
			due, err := model.GetDueUpstreamRatioChannels(time.Now().Unix(), 1)
			require.NoError(t, err)
			require.Len(t, due, 1)
			require.Equal(t, f.binding.ChannelID, due[0].Id)
		})
	}
}

func TestUpstreamSyncSub2APIUnknownStatusKeepsChannelEnabled(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	t.Setenv("CRYPTO_SECRET", "fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			fmt.Fprint(w, `{"code":0,"data":{"username":"fixture","balance":2}}`)
		case "/api/v1/groups/available", "/api/v1/subscriptions/progress":
			fmt.Fprint(w, `{"code":0,"data":[]}`)
		case "/api/v1/groups/rates":
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		case "/api/v1/keys":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page_size":200,"page":1,"items":[{"id":17,"name":"k1","status":"pending_review","key":"sub2-test-key","group_id":1}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	base := server.URL
	channel := model.Channel{Name: "sub2-unknown-status", Type: constant.ChannelTypeSub2API, Key: "sub2-test-key", BaseURL: &base, Status: common.ChannelStatusEnabled, Models: "gpt-4o", ChannelRatio: "1", CreatedTime: 1800000000}
	require.NoError(t, channel.Insert())
	account := model.UpstreamAccount{CredentialUID: "uid-sub2-unknown", Name: "sub2", Platform: "sub2api", BaseURL: server.URL, Status: "active"}
	enc, err := sealUpstreamCredential(&account, UpstreamCredential{AccessToken: "at", RefreshToken: "rt", AccessExpiresAt: time.Now().Add(24 * time.Hour).Unix(), RefreshExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix()})
	require.NoError(t, err)
	account.CredentialCiphertext = enc
	require.NoError(t, model.CreateUpstreamAccount(&account))
	binding := model.UpstreamChannelBinding{AccountID: account.ID, RemoteKeyID: 17, ChannelID: channel.Id, ChannelCreatedTime: channel.CreatedTime, ChannelFP: upstreamRatioFingerprint(&channel), RemoteStatus: "active", RatioSyncFactor: "1"}
	require.NoError(t, model.BindUpstreamChannel(&binding, func(*model.Channel) error { return nil }))

	err = SyncUpstreamAccount(context.Background(), account.ID)
	require.ErrorContains(t, err, "upstream_invalid_response")
	gotChannel, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, gotChannel.Status)
	rows, err := model.ListUpstreamBindings(account.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "active", rows[0].RemoteStatus)
}

func TestRefreshChannelUpstreamRatioUsesNewAPIAccountSync(t *testing.T) {
	f := newSyncFixture(t)
	view, err := RefreshChannelUpstreamRatio(context.Background(), f.binding.ChannelID)
	require.NoError(t, err)
	require.Equal(t, model.UpstreamRatioSourceNewAPI, view.Source)
	require.Equal(t, "account", view.Mapping)
	require.Equal(t, model.UpstreamRatioStatusOK, view.Status)
	require.Equal(t, "1.25", view.EffectiveRatio)
	// RatioSyncEnabled=true with factor=2 -> 1.25 * 2 = 2.5 written immediately.
	require.Equal(t, "2.5", f.channel(t).ChannelRatio)
}

func TestUpdateManagedBindingAndManualRatioWriteChannelRatioImmediately(t *testing.T) {
	f := newSyncFixture(t)
	disabled := false
	require.NoError(t, UpdateManagedBinding(context.Background(), f.account.ID, f.binding.ChannelID, &disabled, nil))
	require.NoError(t, f.sync())
	require.Equal(t, "1", f.channel(t).ChannelRatio)

	// Turning sync on with factor=1.5 immediately writes 1.25 * 1.5 = 1.875.
	enabled := true
	factor := "1.5"
	require.NoError(t, UpdateManagedBinding(context.Background(), f.account.ID, f.binding.ChannelID, &enabled, &factor))
	require.Equal(t, "1.875", f.channel(t).ChannelRatio)
	require.Empty(t, f.storedBinding(t).RatioSyncError)

	// Changing factor to 2 immediately updates channel_ratio to 2.5.
	factor2 := "2"
	require.NoError(t, UpdateManagedBinding(context.Background(), f.account.ID, f.binding.ChannelID, nil, &factor2))
	require.Equal(t, "2.5", f.channel(t).ChannelRatio)

	// Updating channel upstream ratio to manual 4 immediately writes 4 * 2 = 8.
	rec, err := model.GetChannelUpstreamRatio(f.binding.ChannelID)
	require.NoError(t, err)
	view, err := UpdateChannelUpstreamRatio(f.binding.ChannelID, UpstreamRatioUpdate{
		Mode:    model.UpstreamRatioModeManual,
		Ratio:   "4",
		Version: rec.Version,
	})
	require.NoError(t, err)
	require.Equal(t, "4", view.EffectiveRatio)
	require.Equal(t, "8", f.channel(t).ChannelRatio)

	// Disabling sync clears any recorded sync error and stops modifying channel_ratio.
	require.NoError(t, model.UpdateUpstreamBinding(f.binding.ID, map[string]any{"ratio_sync_error": "upstream_ratio_unavailable"}))
	require.NoError(t, UpdateManagedBinding(context.Background(), f.account.ID, f.binding.ChannelID, &disabled, nil))
	require.Empty(t, f.storedBinding(t).RatioSyncError)
	require.Equal(t, "8", f.channel(t).ChannelRatio)
}

func TestSub2APIBoundMainstreamChannelAutoProbesAndWritesChannelRatio(t *testing.T) {
	db := setupUpstreamRatioTest(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.SystemTask{}))
	t.Setenv("CRYPTO_SECRET", "sub2-mainstream-test")

	billingServer := (&fakeUpstream{
		billing: sub2apiBody("1.5", "1.5", "1.5", `,"peak_rate_enabled":false`),
	}).serve(t)
	base := billingServer.URL

	// Mainstream protocol (OpenAI type 1), NOT type 59.
	ch := model.Channel{
		Name:         "openai-via-sub2api",
		Type:         constant.ChannelTypeOpenAI,
		Key:          fakeUpstreamKey,
		BaseURL:      &base,
		Status:       common.ChannelStatusEnabled,
		Models:       "gpt-4o",
		ChannelRatio: "1",
		CreatedTime:  1800000100,
	}
	require.NoError(t, ch.Insert())

	account := model.UpstreamAccount{
		CredentialUID: "uid-sub2-mainstream",
		Name:          "sub2-mainstream",
		Platform:      model.UpstreamRatioSourceSub2API,
		BaseURL:       base,
		Status:        "active",
		LastSyncAt:    time.Now().Unix(),
	}
	require.NoError(t, model.CreateUpstreamAccount(&account))

	binding := model.UpstreamChannelBinding{
		AccountID:          account.ID,
		RemoteKeyID:        42,
		ChannelID:          ch.Id,
		ChannelCreatedTime: ch.CreatedTime,
		ChannelFP:          upstreamRatioFingerprint(&ch),
		RemoteStatus:       "active",
		RatioSyncEnabled:   true,
		RatioSyncFactor:    "2",
	}
	require.NoError(t, model.BindUpstreamChannel(&binding, func(*model.Channel) error { return nil }))

	// 1. GetDueUpstreamRatioChannels includes mainstream OpenAI channel bound to active Sub2API account.
	due, err := model.GetDueUpstreamRatioChannels(time.Now().Unix(), 20)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, ch.Id, due[0].Id)

	// 2. Scheduled probe cycle probes /v1/sub2api/billing directly and writes channel_ratio = 1.5 * 2 = 3.
	summary, err := RunChannelUpstreamRatioCycle(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, summary.Statuses[model.UpstreamRatioStatusOK])
	got, err := model.GetChannelById(ch.Id, true)
	require.NoError(t, err)
	require.Equal(t, "3", got.ChannelRatio)

	// 3. Updating factor via UpdateManagedBinding immediately updates channel_ratio to 1.5 * 3 = 4.5.
	factor3 := "3"
	require.NoError(t, UpdateManagedBinding(context.Background(), account.ID, ch.Id, nil, &factor3))
	got, err = model.GetChannelById(ch.Id, true)
	require.NoError(t, err)
	require.Equal(t, "4.5", got.ChannelRatio)
}

func TestReverifyManagedBindingRestoresStaleChannelAndSyncsRatio(t *testing.T) {
	f := newSyncFixture(t)
	// Simulate channel setting/proxy change -> fingerprint mismatch -> stale=true.
	setting := `{"proxy":""}`
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", f.binding.ChannelID).Update("setting", setting).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamChannelBinding{}).Where("id = ?", f.binding.ID).Update("stale", true).Error)
	require.True(t, f.storedBinding(t).Stale)

	// Reverify with matching key restores stale=false and syncs ratio immediately (1.25 * 2 = 2.5).
	require.NoError(t, ReverifyManagedBinding(context.Background(), f.account.ID, f.binding.ChannelID))
	require.False(t, f.storedBinding(t).Stale)
	require.Equal(t, "2.5", f.channel(t).ChannelRatio)

	// Mismatched key is rejected and binding remains stale.
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", f.binding.ChannelID).Update("key", "sk-wrong-key").Error)
	require.NoError(t, model.DB.Model(&model.UpstreamChannelBinding{}).Where("id = ?", f.binding.ID).Update("stale", true).Error)
	err := ReverifyManagedBinding(context.Background(), f.account.ID, f.binding.ChannelID)
	require.ErrorContains(t, err, "channel_key_mismatch")
	require.True(t, f.storedBinding(t).Stale)
}
