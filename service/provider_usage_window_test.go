package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func providerUsageFixture(t *testing.T, handler http.HandlerFunc) (*gorm.DB, *model.Channel, *providerUsageCollector, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previous, previousKind := model.DB, common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previous
		common.SetMainDatabaseType(previousKind)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.GatewayCredential{}, &model.ProviderUsageSnapshot{}, &model.ProviderUsageSetting{}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	channel := &model.Channel{Id: 71, Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(server.URL), Key: `{"access_token":"test-access","refresh_token":"test-refresh","account_id":"test-account"}`}
	require.NoError(t, db.Create(channel).Error)
	now := time.Now().Truncate(time.Second)
	collector := &providerUsageCollector{now: func() time.Time { return now }, client: func(string) (*http.Client, error) { return server.Client(), nil }, refreshCredential: func(context.Context, int, CodexCredentialRefreshOptions) (*CodexOAuthKey, *model.Channel, error) {
		return nil, nil, errors.New("unexpected refresh")
	}}
	collector.workers = &common.WorkerGroup{}
	t.Cleanup(func() {
		collector.workers.BeginStop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, collector.workers.Wait(ctx))
	})
	return db, channel, collector, &now
}

func TestProviderUsageShutdownCancelsRefreshAndJoinsBeforeDatabaseClose(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	db, channel, collector, _ := providerUsageFixture(t, func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
		close(cancelled)
	})
	var completions atomic.Int32
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("count_usage_completion", func(tx *gorm.DB) {
		if tx.Statement.Table == "provider_usage_snapshots" {
			completions.Add(1)
		}
	}))
	backgroundDone := make(chan error, 1)
	require.True(t, collector.workers.Go(func(ctx context.Context) {
		_, err := collector.refresh(ctx, channel.Id, providerUsageInterval)
		backgroundDone <- err
	}))
	<-started
	completions.Store(0) // The initial durable cooldown has already committed.
	collector.workers.BeginStop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, collector.workers.Wait(ctx))
	require.ErrorIs(t, <-backgroundDone, context.Canceled)
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("provider request did not observe shutdown cancellation")
	}
	assert.Zero(t, completions.Load(), "cancelled collection cannot write a successful snapshot")
	var snapshot model.ProviderUsageSnapshot
	require.NoError(t, db.First(&snapshot).Error)
	assert.Equal(t, "unknown", snapshot.State)
	assert.Zero(t, snapshot.FetchedAt)
	_, err := collector.refresh(context.Background(), channel.Id, providerUsageManualInterval)
	require.ErrorIs(t, err, context.Canceled, "shutdown closes admission to shared refreshes")
}

func TestProviderUsageBrowserCancellationDoesNotCancelSharedCollection(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	_, channel, collector, _ := providerUsageFixture(t, func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		started <- request.Context()
		select {
		case <-release:
			_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":7}}}`)
		case <-request.Context().Done():
		}
	})
	browserCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	browserDone := make(chan error, 1)
	go func() {
		_, err := collector.refresh(browserCtx, channel.Id, providerUsageManualInterval)
		browserDone <- err
	}()
	upstreamCtx := <-started
	cancel()
	require.ErrorIs(t, <-browserDone, context.Canceled)
	require.NoError(t, upstreamCtx.Err(), "one browser cannot cancel shared provider I/O")
	close(release)
	view, err := collector.refresh(context.Background(), channel.Id, providerUsageManualInterval)
	if err != nil {
		require.ErrorIs(t, err, model.ErrProviderUsageCooldown, "a completed shared refresh is read from cache")
	}
	assert.Equal(t, "known", view.State)
	require.Len(t, view.Windows, 1)
	require.NotNil(t, view.Windows[0].UsedPercent)
	assert.Equal(t, 7.0, *view.Windows[0].UsedPercent)
	assert.EqualValues(t, 1, calls.Load())
}

func TestProviderUsageReadIsCacheOnlyAndPreservesUnknown(t *testing.T) {
	var calls atomic.Int32
	db, channel, collector, now := providerUsageFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/backend-api/wham/usage", r.URL.Path)
		assert.Equal(t, "Bearer test-access", r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, `{"plan_type":"plus","access_token":"must-not-persist","rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":18000,"reset_after_seconds":60},"secondary_window":{"used_percent":null}}}`)
	})
	view, err := GetProviderUsage(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "unknown", view.State)
	assert.False(t, view.AutoRefresh)
	assert.Empty(t, view.Windows)
	var identities int64
	require.NoError(t, db.Model(&model.GatewayCredential{}).Count(&identities).Error)
	assert.Zero(t, identities)
	assert.Zero(t, calls.Load())
	view, err = collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.NoError(t, err)
	require.Len(t, view.Windows, 2)
	assert.Equal(t, "known", view.State)
	require.NotNil(t, view.Windows[0].UsedPercent)
	assert.Zero(t, *view.Windows[0].UsedPercent)
	assert.Nil(t, view.Windows[1].UsedPercent)
	assert.Equal(t, now.Add(time.Minute).Unix(), *view.Windows[0].ResetAt)
	_, err = collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.ErrorIs(t, err, model.ErrProviderUsageCooldown)
	assert.EqualValues(t, 1, calls.Load())
	require.NoError(t, db.Model(&model.ProviderUsageSnapshot{}).Where("channel_id = ?", channel.Id).Update("expires_at", time.Now().Unix()-1).Error)
	view, err = GetProviderUsage(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "stale", view.State)
	assert.EqualValues(t, 1, calls.Load())
	var stored model.ProviderUsageSnapshot
	require.NoError(t, db.First(&stored).Error)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "test-access")
	assert.NotContains(t, string(stored.Windows), "must-not-persist")
}

func TestProviderUsageRateLimitKeepsPreviousObservationAndCooldown(t *testing.T) {
	var calls atomic.Int32
	_, channel, collector, now := providerUsageFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":25}}}`)
			return
		}
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":"private upstream failure"}`)
	})
	_, err := collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.NoError(t, err)
	*now = now.Add(time.Minute)
	view, err := collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.NoError(t, err)
	assert.Equal(t, "error", view.State)
	assert.Equal(t, "rate_limited", view.ErrorCode)
	assert.Equal(t, now.Add(15*time.Minute).Unix(), view.NextRefreshAt)
	assert.Equal(t, 25.0, *view.Windows[0].UsedPercent)
	assert.Equal(t, now.Add(-time.Minute).Unix(), view.FetchedAt)
	*now = now.Add(5 * time.Minute)
	_, err = collector.refresh(t.Context(), channel.Id, providerUsageInterval)
	assert.ErrorIs(t, err, model.ErrProviderUsageCooldown)
	assert.EqualValues(t, 2, calls.Load())
}

func TestProviderUsageReplacementAndEndpointInvalidateCachedIdentity(t *testing.T) {
	db, channel, collector, _ := providerUsageFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":42}}}`)
	})
	_, err := collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.NoError(t, err)
	key := strings.Replace(channel.Key, "test-access", "new-account-key", 1)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("key", key).Error)
	view, err := GetProviderUsage(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "unknown", view.State)
	assert.Empty(t, view.Windows)
	_, err = collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("gateway_identity_version", 2).Error)
	view, err = GetProviderUsage(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "unknown", view.State)
}

func TestProviderUsageInFlightResponseCannotPopulateReplacement(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	db, channel, collector, _ := providerUsageFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":42}}}`)
	})
	done := make(chan error, 1)
	go func() {
		_, err := collector.refresh(context.Background(), channel.Id, providerUsageManualInterval)
		done <- err
	}()
	<-started
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("key", strings.Replace(channel.Key, "test-access", "replacement", 1)).Error)
	close(release)
	require.ErrorIs(t, <-done, model.ErrProviderUsageIdentityChanged)
	view, err := GetProviderUsage(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "unknown", view.State)
	assert.Empty(t, view.Windows)
}

func TestProviderUsageCoalescesAndSeparatesManualFromBackgroundInterval(t *testing.T) {
	var calls atomic.Int32
	_, channel, collector, now := providerUsageFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":1}}}`)
	})
	var group sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := collector.refresh(context.Background(), channel.Id, providerUsageManualInterval)
			errorsFound <- err
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			require.ErrorIs(t, err, model.ErrProviderUsageCooldown)
		}
	}
	assert.EqualValues(t, 1, calls.Load())
	*now = now.Add(time.Minute)
	_, err := collector.refresh(t.Context(), channel.Id, providerUsageInterval)
	require.ErrorIs(t, err, model.ErrProviderUsageCooldown)
	_, err = collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.NoError(t, err)
	assert.EqualValues(t, 2, calls.Load())
}

func TestProviderUsageTrustedRefreshKeepsIdentityAndPropagatesWriteFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var calls atomic.Int32
			db, channel, collector, _ := providerUsageFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") == "Bearer test-access" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":2}}}`)
			})
			identity, err := model.EnsureGatewayCredential(t.Context(), channel.Id, channel.Key)
			require.NoError(t, err)
			collector.refreshCredential = func(ctx context.Context, id int, _ CodexCredentialRefreshOptions) (*CodexOAuthKey, *model.Channel, error) {
				if fail {
					return nil, nil, errors.New("database write rejected")
				}
				current, err := model.GetChannelById(id, true)
				if err != nil {
					return nil, nil, err
				}
				replacement := strings.Replace(current.Key, "test-access", "refreshed-access", 1)
				if err := model.RefreshGatewayCredential(ctx, current, replacement); err != nil {
					return nil, nil, err
				}
				current.Key = replacement
				key, err := parseCodexOAuthKey(replacement)
				return key, current, err
			}
			view, err := collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
			if fail {
				require.ErrorContains(t, err, "database write rejected")
				assert.EqualValues(t, 1, calls.Load())
				cached, readErr := GetProviderUsage(t.Context(), channel.Id)
				require.NoError(t, readErr)
				assert.Equal(t, "error", cached.State)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "known", view.State)
			assert.EqualValues(t, 2, calls.Load())
			var snapshot model.ProviderUsageSnapshot
			require.NoError(t, db.First(&snapshot).Error)
			assert.Equal(t, identity.ID, snapshot.CredentialID)
			cached, err := GetProviderUsage(t.Context(), channel.Id)
			require.NoError(t, err)
			assert.Equal(t, "known", cached.State)
		})
	}
}

func TestProviderUsagePersistenceFailureIsNotSuccess(t *testing.T) {
	db, channel, collector, _ := providerUsageFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"rate_limit":{"primary_window":{"used_percent":2}}}`)
	})
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("reject_usage_completion", func(tx *gorm.DB) {
		if tx.Statement.Table == "provider_usage_snapshots" && len(tx.Statement.Selects) > 0 {
			tx.AddError(errors.New("snapshot write rejected"))
		}
	}))
	_, err := collector.refresh(t.Context(), channel.Id, providerUsageManualInterval)
	require.ErrorContains(t, err, "snapshot write rejected")
}

func TestProviderUsageParserRejectsMalformedWithoutInventingValues(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"rate_limit":{"primary_window":{"used_percent":-1}}}`, `{"rate_limit":{"primary_window":{"used_percent":101}}}`, `{"rate_limit":{"primary_window":{"used_percent":"0"}}}`, `{"rate_limit":{"primary_window":{"reset_at":-1}}}`} {
		_, _, err := parseCodexProviderUsage([]byte(body), time.Now())
		require.Error(t, err, body)
	}
	_, windows, err := parseCodexProviderUsage([]byte(`{"rate_limit":{"primary_window":{},"secondary_window":null}}`), time.Now())
	require.NoError(t, err)
	require.Len(t, windows, 1)
	assert.Nil(t, windows[0].UsedPercent)
	assert.Nil(t, windows[0].ResetAt)
}

func TestProviderUsageBackgroundRequiresExplicitOptIn(t *testing.T) {
	var calls atomic.Int32
	_, channel, collector, _ := providerUsageFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = fmt.Fprint(w, `{}`) })
	previous := defaultProviderUsageCollector
	defaultProviderUsageCollector = collector
	t.Cleanup(func() { defaultProviderUsageCollector = previous })
	require.NoError(t, collectEnabledProviderUsage(t.Context()))
	assert.Zero(t, calls.Load())
	view, err := SetProviderUsageAutoRefresh(t.Context(), channel.Id, true)
	require.NoError(t, err)
	assert.True(t, view.AutoRefresh)
	require.NoError(t, collectEnabledProviderUsage(t.Context()))
	assert.EqualValues(t, 1, calls.Load())
	view, err = GetProviderUsage(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "unknown", view.State)
}
