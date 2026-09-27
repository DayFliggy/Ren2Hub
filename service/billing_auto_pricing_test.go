package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func automaticPricingTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.BillingActivePrice{}, &model.BillingPricingSource{}, &model.BillingPricingSnapshot{}, &model.BillingPricingConfig{}, &model.Ability{}, &model.Channel{}))
	previous := model.DB
	mainDB, logDB := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	taskPriceTestRegistry(t)
	t.Cleanup(func() {
		model.DB = previous
		common.SetDatabaseTypes(mainDB, logDB)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
}

func TestAutomaticPricingSnapshotStatesAndFailedInitialRefresh(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	initialDocument := `{"existing":{"input_cost_per_token":0.000002,"output_cost_per_token":0}}`
	initialSource := model.BillingPricingSource{Source: "sub2api", Document: model.LongText(initialDocument), Hash: pricingDocumentHash([]byte(initialDocument)), ParserVersion: autopricing.ParserVersion}
	require.NoError(t, model.EnsureBillingPricingSource(ctx, &initialSource))
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(initialDocument))
	require.NoError(t, err)
	price, err := model.PublishAutomaticBillingPrice(ctx, entries["existing"].Price, "sub2api", "existing", initialSource.Hash)
	require.NoError(t, err)
	require.True(t, price)
	require.NoError(t, model.DB.Model(&model.BillingPricingSource{}).Where("source = ?", "sub2api").Updates(map[string]interface{}{"snapshot_id": 0, "hash": ""}).Error)
	original, err := model.GetActiveBillingPrice(ctx, "existing")
	require.NoError(t, err)
	body := "{}"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
	defer server.Close()
	specs := []billingPricingSourceSpec{{Source: autopricing.SourceSub2API, URL: server.URL}}
	failed, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.Error(t, err)
	assert.Zero(t, failed.Deactivated, "a missing cache and failed fetch cannot prove that an existing price was removed")
	retained, err := model.GetActiveBillingPrice(ctx, "existing")
	require.NoError(t, err)
	assert.Equal(t, original, retained)
	status, err := GetAutomaticPricingStatus(ctx)
	require.NoError(t, err)
	require.Len(t, status.Sources, 2)
	assert.Equal(t, "unavailable", status.Sources[0].SnapshotState)
	assert.Equal(t, "fallback", status.Sources[1].Role)

	body = `{"existing":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	_, err = syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	status, err = GetAutomaticPricingStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "current", status.Sources[0].SnapshotState)

	for _, invalid := range []string{"{}", "[]", `{"existing":{"input_cost_per_token":-1,"output_cost_per_token":2}}`} {
		body = invalid
		failed, err = syncAutomaticPricing(ctx, server.Client(), specs)
		require.Error(t, err)
		assert.Zero(t, failed.Deactivated)
		status, err = GetAutomaticPricingStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "cached", status.Sources[0].SnapshotState)
		assert.NotEmpty(t, status.Sources[0].LastError)
	}
}

func TestAutomaticPricingCanceledDownloadRetainsSnapshot(t *testing.T) {
	data := `{"existing":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	previous := model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(data), Hash: pricingDocumentHash([]byte(data)), ModelCount: 1}
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		row     model.BillingPricingSource
		changed bool
		err     error
	}
	completed := make(chan outcome, 1)
	go func() {
		row, changed, err := fetchPricingSource(ctx, server.Client(), billingPricingSourceSpec{Source: autopricing.SourceSub2API, URL: server.URL}, previous)
		completed <- outcome{row, changed, err}
	}()
	<-started
	cancel()
	result := <-completed
	require.ErrorIs(t, result.err, context.Canceled)
	assert.False(t, result.changed)
	assert.Equal(t, previous, result.row)
}

func TestAutomaticPricingHTTPTimeoutRetainsSnapshot(t *testing.T) {
	data := `{"existing":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	previous := model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(data), Hash: pricingDocumentHash([]byte(data)), ModelCount: 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 50 * time.Millisecond
	for _, stage := range []string{"digest", "document"} {
		t.Run(stage, func(t *testing.T) {
			spec := billingPricingSourceSpec{Source: autopricing.SourceSub2API, URL: server.URL}
			if stage == "digest" {
				spec.HashURL = server.URL + "/hash"
			}
			row, changed, err := fetchPricingSource(context.Background(), client, spec, previous)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.False(t, changed)
			assert.Equal(t, previous, row, "a timed-out digest or document never replaces the valid snapshot")
		})
	}
}

func TestAutomaticPricingStatusDoesNotExposeRetiredSources(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	require.NoError(t, model.SaveBillingPricingConfig(ctx, &model.BillingPricingConfig{Enabled: false, CheckIntervalMinutes: 75}))
	require.NoError(t, model.DB.Create(&model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "openrouter", LastError: "historical failure"}).Error)
	require.NoError(t, InitializeAutomaticPricing(ctx))
	status, err := GetAutomaticPricingStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.Config.Enabled)
	assert.Equal(t, 75, status.Config.CheckIntervalMinutes)
	require.Len(t, status.Sources, 2)
	for _, source := range status.Sources {
		assert.Contains(t, []string{"sub2api", "axonhub"}, source.Source)
		assert.Equal(t, "bundled", source.SnapshotState)
		assert.Positive(t, source.ModelCount)
	}
}

func TestAutomaticPricingHashChecksAndRejectedDownloads(t *testing.T) {
	ctx := context.Background()
	data := `{"priced":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	downloads := 0
	remoteHash := pricingDocumentHash([]byte(data))
	body := data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hash" {
			_, _ = io.WriteString(w, remoteHash)
			return
		}
		downloads++
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	spec := billingPricingSourceSpec{Source: autopricing.SourceSub2API, URL: server.URL + "/prices", HashURL: server.URL + "/hash"}
	first, changed, err := fetchPricingSource(ctx, server.Client(), spec, model.BillingPricingSource{})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, remoteHash, first.Hash)
	assert.Equal(t, 1, downloads)
	second, changed, err := fetchPricingSource(ctx, server.Client(), spec, first)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, downloads, "matching digest avoids downloading the catalog")
	remoteHash = strings.Repeat("0", 64)
	retained, _, err := fetchPricingSource(ctx, server.Client(), spec, first)
	require.ErrorContains(t, err, "does not match")
	assert.Equal(t, first, retained, "bad source digest never advances last-good anchor")
	body = `{"not":"a price catalog"}`
	remoteHash = pricingDocumentHash([]byte(body))
	retained, _, err = fetchPricingSource(ctx, server.Client(), spec, first)
	require.Error(t, err)
	assert.Equal(t, first, retained, "parse rejection preserves last-good document")
	remoteHash = "not a digest"
	_, _, err = fetchPricingSource(ctx, server.Client(), spec, first)
	require.ErrorContains(t, err, "invalid")
}

func TestAutomaticPricingConditionalRequestsAndSizeLimit(t *testing.T) {
	data := `{"providers":{"provider":{"models":[{"id":"priced","cost":{"input":2,"output":8}}]}}}`
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"etag"` {
			assert.Equal(t, "Fri, 25 Sep 2026 00:00:00 GMT", r.Header.Get("If-Modified-Since"))
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", "Fri, 25 Sep 2026 00:00:00 GMT")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, data)
	}))
	defer server.Close()
	spec := billingPricingSourceSpec{Source: autopricing.SourceAxonHub, URL: server.URL}
	first, changed, err := fetchPricingSource(context.Background(), server.Client(), spec, model.BillingPricingSource{})
	require.NoError(t, err)
	assert.True(t, changed)
	second, changed, err := fetchPricingSource(context.Background(), server.Client(), spec, first)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, first, second)
	_, _, _, err = pricingSourceRequest(context.Background(), server.Client(), server.URL, nil, 8)
	require.ErrorContains(t, err, "exceeds")
	status = http.StatusServiceUnavailable
	first.ETag, first.LastModified = "", ""
	retained, _, err := fetchPricingSource(context.Background(), server.Client(), spec, first)
	require.ErrorContains(t, err, "503")
	assert.Equal(t, first, retained)
}

func TestAutomaticPricingSyncPersistsChangesAndPreservesManual(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	document := `{"automatic":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"manual":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, document) }))
	defer server.Close()
	specs := []billingPricingSourceSpec{{Source: autopricing.SourceSub2API, URL: server.URL}}
	manual, err := model.PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "manual", Mode: billing.ModeFree}, 0, 0)
	require.NoError(t, err)
	first, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Published)
	automatic, err := model.GetActiveBillingPrice(ctx, "automatic")
	require.NoError(t, err)
	second, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	assert.Zero(t, second.Changed)
	assert.Zero(t, second.Published)
	stillManual, err := model.GetActiveBillingPrice(ctx, "manual")
	require.NoError(t, err)
	assert.Equal(t, manual, stillManual)
	document = strings.ReplaceAll(document, "0.000002", "0.000003")
	third, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	assert.Equal(t, 1, third.Published)
	newAutomatic, err := model.GetActiveBillingPrice(ctx, "automatic")
	require.NoError(t, err)
	assert.NotEqual(t, automatic.ID, newAutomatic.ID)
	document = "bad json"
	failed, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.Error(t, err)
	assert.Len(t, failed.Errors, 1)
	afterError, err := model.GetActiveBillingPrice(ctx, "automatic")
	require.NoError(t, err)
	assert.Equal(t, newAutomatic, afterError)
	status, err := GetAutomaticPricingStatus(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, status.Sources[0].LastError)
	assert.NotZero(t, status.Sources[0].CheckedAt)
	assert.True(t, status.Config.Enabled)
	assert.Equal(t, 30, status.Config.CheckIntervalMinutes)
}

func TestAutomaticPricingMergeUsesCompleteFallbackAndRejectsCorruption(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	sources := []struct {
		id   string
		data string
	}{
		{"sub2api", `{"shared":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"unsafe":{"input_cost_per_token":-1,"output_cost_per_token":0.000008}}`},
		{"axonhub", `{"providers":{"vendor":{"models":[{"id":"shared","cost":{"input":10,"output":20}},{"id":"unsafe","cost":{"input":1,"output":2}},{"id":"second","cost":{"input":1,"output":2}}]}}}`},
	}
	for _, source := range sources {
		err := model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: source.id, Document: model.LongText(source.data), Hash: pricingDocumentHash([]byte(source.data))})
		require.NoError(t, err)
	}
	catalog, err := loadAutomaticPriceCatalog(ctx)
	require.NoError(t, err)
	assert.Equal(t, autopricing.SourceSub2API, catalog.resolve("shared").entry.Source)
	assert.Equal(t, autopricing.SourceAxonHub, catalog.resolve("second").entry.Source)
	assert.Equal(t, autopricing.SourceAxonHub, catalog.resolve("unsafe").entry.Source)
	drafts, unavailable, _, err := AutomaticPricingDrafts(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, drafts)
	assert.NotContains(t, unavailable, "unsafe")
	require.NoError(t, model.DB.Model(&model.BillingPricingSource{}).Where("source = ?", "sub2api").Update("hash", strings.Repeat("0", 64)).Error)
	catalog, err = loadAutomaticPriceCatalog(ctx)
	require.NoError(t, err)
	assert.Equal(t, autopricing.SourceAxonHub, catalog.resolve("shared").entry.Source)
	assert.Contains(t, catalog.resolve("missing").reasons[0].Reason, "integrity")
	require.NoError(t, model.DB.Model(&model.BillingPricingSource{}).Where("source = ?", "axonhub").Update("hash", strings.Repeat("0", 64)).Error)
	_, err = loadAutomaticPriceCatalog(ctx)
	require.ErrorContains(t, err, "no validated pricing snapshot")
}

func TestAutomaticPricingInvalidatesUnavailablePricesWithoutChangingFrozenQuotes(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	document := `{"unsafe":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"removed":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"stable":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, document) }))
	defer server.Close()
	sources := []billingPricingSourceSpec{{Source: autopricing.SourceSub2API, URL: server.URL}}
	require.NoError(t, model.DB.Create(&model.Ability{Model: "models/unsafe", Group: "default", ChannelId: 1, Enabled: true}).Error)
	manual, err := model.PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "manual-only", Mode: billing.ModeFree}, 0, 7)
	require.NoError(t, err)
	initial, err := syncAutomaticPricing(ctx, server.Client(), sources)
	require.NoError(t, err)
	assert.Equal(t, 4, initial.Published)
	before, err := model.GetActiveBillingPrice(ctx, "unsafe")
	require.NoError(t, err)
	quote, err := billing.NewQuote(*before, "1", nil, time.Unix(1000, 0))
	require.NoError(t, err)
	usage := billing.Usage{InputTokens: 1000, OutputTokens: 100, ContextTokens: 1000, Complete: true, InputIncludesCache: true}
	originalCharge, err := billing.Calculate(quote, usage)
	require.NoError(t, err)
	document = `{"unsafe":{"input_cost_per_token":-1,"output_cost_per_token":0.000008},"stable":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	updated, err := syncAutomaticPricing(ctx, server.Client(), sources)
	require.NoError(t, err)
	assert.Equal(t, 3, updated.Deactivated)
	assert.Equal(t, 1, updated.Published, "source provenance changes create a new frozen version even when stable pricing is unchanged")
	for _, name := range []string{"unsafe", "models/unsafe", "removed"} {
		_, err := model.GetActiveBillingPrice(ctx, name)
		require.ErrorIs(t, err, model.ErrAutomaticBillingPriceUnavailable)
		assert.Contains(t, updated.Unconfigured, name)
	}
	stillManual, err := model.GetActiveBillingPrice(ctx, manual.Model)
	require.NoError(t, err)
	assert.Equal(t, manual, stillManual)
	_, err = model.GetActiveBillingPrice(ctx, "stable")
	require.NoError(t, err)
	var historical model.BillingPriceVersion
	require.NoError(t, model.DB.First(&historical, before.ID).Error)
	assert.Equal(t, before.Hash, historical.Hash)
	charge, err := billing.Calculate(quote, usage)
	require.NoError(t, err)
	assert.Equal(t, originalCharge, charge)

	// A failed download keeps the validated snapshot and its active prices.
	document = "bad json"
	failed, err := syncAutomaticPricing(ctx, server.Client(), sources)
	require.Error(t, err)
	assert.Zero(t, failed.Deactivated)
	_, err = model.GetActiveBillingPrice(ctx, "stable")
	require.NoError(t, err)

	// A later repaired source makes both the concrete model and channel alias usable.
	document = `{"unsafe":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000008},"stable":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	repaired, err := syncAutomaticPricing(ctx, server.Client(), sources)
	require.NoError(t, err)
	assert.Equal(t, 3, repaired.Published, "the repaired source revision publishes unsafe aliases and the changed stable snapshot")
	for _, name := range []string{"unsafe", "models/unsafe"} {
		price, err := model.GetActiveBillingPrice(ctx, name)
		require.NoError(t, err)
		assert.NotEqual(t, before.ID, price.ID)
	}
}

func TestBundledAutomaticPricingBootsOfflineAndPreservesPersistedSnapshot(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	// Disabled automatic publication still initializes durable offline catalogs.
	require.NoError(t, model.SaveBillingPricingConfig(ctx, &model.BillingPricingConfig{Enabled: false, CheckIntervalMinutes: 30}))
	require.NoError(t, InitializeAutomaticPricing(ctx))
	rows, err := model.ListBillingPricingSources(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		assert.Positive(t, row.ModelCount)
		assert.Equal(t, pricingDocumentHash([]byte(row.Document)), row.Hash)
		assert.Zero(t, row.CheckedAt, "bundled data is not reported as a successful remote check")
	}
	prices, err := model.ListActiveBillingPrices(ctx)
	require.NoError(t, err)
	assert.Empty(t, prices)
	data := `{"persisted":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`
	require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(data), Hash: pricingDocumentHash([]byte(data)), ModelCount: 1}))
	require.NoError(t, InitializeAutomaticPricing(ctx))
	rows, err = model.ListBillingPricingSources(ctx)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Source == "sub2api" {
			assert.Equal(t, data, string(row.Document))
		}
	}
}

func TestBundledAutomaticPricingPublishesUsableVersions(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	manual, err := model.PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "claude-sonnet-4-6", Mode: billing.ModeFree}, 0, 3)
	require.NoError(t, err)
	require.NoError(t, InitializeAutomaticPricing(ctx))
	unchanged, err := model.GetActiveBillingPrice(ctx, manual.Model)
	require.NoError(t, err)
	assert.Equal(t, manual, unchanged)
	price, err := model.GetActiveBillingPrice(ctx, "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, billing.ModeExpression, price.Mode)
	quote, err := billing.NewQuote(*price, "1", nil, time.Unix(1000, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000, OutputTokens: 100, ContextTokens: 1000, Complete: true, InputIncludesCache: true})
	require.NoError(t, err)
	assert.NotEqual(t, "0", charge.TotalUSD)
	var before int64
	require.NoError(t, model.DB.Model(&model.BillingPriceVersion{}).Count(&before).Error)
	result, err := publishAutomaticPricingCatalog(ctx)
	require.NoError(t, err)
	assert.Zero(t, result.Published)
	var after int64
	require.NoError(t, model.DB.Model(&model.BillingPriceVersion{}).Count(&after).Error)
	assert.Equal(t, before, after, "restart/check with the same catalog does not create versions")
	assert.LessOrEqual(t, len(result.Unconfigured), 100)
	t.Logf("offline published versions=%d; pending review=%d", after, result.UnconfiguredCount)
}

func TestAutomaticPricingAliasesUseCanonicalIdentityWithoutDateSiblingGuessing(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	primary := `{"priced":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"only-dated-20260101":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"gpt-5.4":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008},"unsafe":{"input_cost_per_token":-1,"output_cost_per_token":0.000008}}`
	secondary := `{"providers":{"vendor":{"models":[{"id":"unsafe","cost":{"input":1,"output":2}},{"id":"other","cost":{"input":1,"output":2}}]}}}`
	for _, spec := range billingPricingSources {
		require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: string(spec.Source)}))
	}
	for id, data := range map[string]string{"sub2api": primary, "axonhub": secondary} {
		require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: id, Document: model.LongText(data), Hash: pricingDocumentHash([]byte(data))}))
	}
	for index, name := range []string{"priced-20260201", "priced-20260301", "models/unsafe", "unsafe-20260201", "only-dated-20260201", "gpt-5.4@effort=high"} {
		require.NoError(t, model.DB.Create(&model.Ability{Model: name, Group: "default", ChannelId: index + 1, Enabled: true}).Error)
	}
	_, err := publishAutomaticPricingCatalog(ctx)
	require.NoError(t, err)
	for _, name := range []string{"priced-20260201", "priced-20260301"} {
		price, err := model.GetActiveBillingPrice(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, name, price.Model)
	}
	for _, name := range []string{"models/unsafe", "unsafe-20260201", "gpt-5.4"} {
		_, err := model.GetActiveBillingPrice(ctx, name)
		require.NoError(t, err)
	}
	for _, name := range []string{"only-dated-20260201", "only-dated"} {
		_, err := model.GetActiveBillingPrice(ctx, name)
		if name == "only-dated-20260201" {
			require.ErrorIs(t, err, model.ErrAutomaticBillingPriceUnavailable)
		} else {
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		}
		_, err = RestoreAutomaticPrice(ctx, name, 0)
		require.ErrorContains(t, err, "unavailable")
	}
	updated := strings.ReplaceAll(primary, "0.000002", "0.000003")
	require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(updated), Hash: pricingDocumentHash([]byte(updated))}))
	_, err = publishAutomaticPricingCatalog(ctx)
	require.NoError(t, err)
	for _, name := range []string{"priced-20260201", "priced-20260301"} {
		price, err := model.GetActiveBillingPrice(ctx, name)
		require.NoError(t, err)
		quote, err := billing.NewQuote(*price, "1", nil, time.Unix(1000, 0))
		require.NoError(t, err)
		charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000, ContextTokens: 1000, Complete: true, InputIncludesCache: true})
		require.NoError(t, err)
		assert.Equal(t, "0.003", charge.TotalUSD)
	}
}

func TestAutomaticPricingFallbackSwitchesAndReportsEveryUnavailableSource(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	primary := `{"m":{"input_cost_per_token":1e-6,"output_cost_per_token":2e-6},"stable":{"input_cost_per_token":1e-6,"output_cost_per_token":2e-6}}`
	secondary := `{"providers":{"vendor":{"models":[{"id":"m","cost":{"input":4,"output":8}}]}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			_, _ = io.WriteString(w, primary)
		} else {
			_, _ = io.WriteString(w, secondary)
		}
	}))
	defer server.Close()
	specs := []billingPricingSourceSpec{{Source: autopricing.SourceSub2API, URL: server.URL + "/primary"}, {Source: autopricing.SourceAxonHub, URL: server.URL + "/secondary"}}
	_, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	before, err := model.GetActiveBillingPrice(ctx, "m")
	require.NoError(t, err)
	primary = strings.Replace(primary, `"input_cost_per_token":1e-6`, `"input_cost_per_token":-1`, 1)
	changed, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	assert.Zero(t, changed.Deactivated)
	assert.NotContains(t, changed.Unconfigured, "m")
	origins, err := model.ListBillingPriceOrigins(ctx)
	require.NoError(t, err)
	for _, origin := range origins {
		if origin.Model == "m" {
			assert.Equal(t, "axonhub", origin.Origin)
		}
	}
	after, err := model.GetActiveBillingPrice(ctx, "m")
	require.NoError(t, err)
	assert.NotEqual(t, before.ID, after.ID)
	secondary = `{"providers":{"vendor":{"models":[{"id":"m","cost":{"input":-1,"output":8}},{"id":"other","cost":{"input":1,"output":2}}]}}}`
	failed, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	_, err = model.GetActiveBillingPrice(ctx, "m")
	require.ErrorIs(t, err, model.ErrAutomaticBillingPriceUnavailable)
	var detail *AutomaticPricingUnconfigured
	for i := range failed.UnconfiguredDetails {
		if failed.UnconfiguredDetails[i].Model == "m" {
			detail = &failed.UnconfiguredDetails[i]
		}
	}
	require.NotNil(t, detail)
	require.Len(t, detail.Reasons, 2)
	assert.Contains(t, detail.Reasons[0].Reason, "negative")
	assert.Contains(t, detail.Reasons[1].Reason, "negative")
}

func TestAutomaticPricingReasoningCandidatesCannotBypassSourceRejection(t *testing.T) {
	for _, test := range []struct {
		name, requested, rejected string
		secondary, exact          bool
		want                      autopricing.SourceID
	}{
		{"rejected legacy cannot reuse same source base", "gpt-5.4-high", "gpt-5.4-high", false, false, ""},
		{"rejected legacy uses next source base", "gpt-5.4-high", "gpt-5.4-high", true, false, autopricing.SourceAxonHub},
		{"rejected canonical cannot reuse same source base", "gpt-5.4@effort:high", "gpt-5.4@effort:high@thinking:on", false, false, ""},
		{"rejected canonical uses next source base", "gpt-5.4@effort:high", "gpt-5.4@effort:high@thinking:on", true, false, autopricing.SourceAxonHub},
		{"valid exact remains preferred", "gpt-5.4-high", "gpt-5.4-high", true, true, autopricing.SourceSub2API},
	} {
		t.Run(test.name, func(t *testing.T) {
			automaticPricingTestDB(t)
			ctx := context.Background()
			candidates := reasoning.BillingModelCandidates(test.requested)
			require.Greater(t, len(candidates), 1)
			require.Equal(t, test.rejected, candidates[0])
			require.Equal(t, "gpt-5.4", candidates[len(candidates)-1])
			price := "-1"
			if test.exact {
				price = "0.000003"
			}
			primary := fmt.Sprintf(`{"gpt-5.4":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002},%q:{"input_cost_per_token":%s,"output_cost_per_token":0.000002}}`, test.rejected, price)
			require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(primary), Hash: pricingDocumentHash([]byte(primary))}))
			if test.secondary {
				secondary := `{"vendor":{"models":[{"id":"gpt-5.4","cost":{"input":4,"output":8}}]}}`
				require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "axonhub", Document: model.LongText(secondary), Hash: pricingDocumentHash([]byte(secondary))}))
			}
			catalog, err := loadAutomaticPriceCatalog(ctx)
			require.NoError(t, err)
			match := catalog.resolve(test.requested)
			if test.want == "" {
				assert.Empty(t, match.entry.Source)
				return
			}
			require.NotEmpty(t, match.entry.Source)
			assert.Equal(t, test.want, match.entry.Source)
			if test.exact {
				assert.Equal(t, test.requested, match.entry.Price.Model)
			}
		})
	}
}

func TestAutomaticPricingPersistsUnavailableSpecificIdentityUntilSourceRecovers(t *testing.T) {
	for _, previouslyPublished := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously_published=%t", previouslyPublished), func(t *testing.T) {
			automaticPricingTestDB(t)
			ctx := context.Background()
			for _, spec := range billingPricingSources {
				require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: string(spec.Source)}))
			}
			valid := `{"gpt-5.4":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002},"gpt-5.4-high":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000006}}`
			if previouslyPublished {
				require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(valid), Hash: pricingDocumentHash([]byte(valid))}))
				_, err := publishAutomaticPricingCatalog(ctx)
				require.NoError(t, err)
				_, err = model.GetActiveBillingPrice(ctx, "gpt-5.4-high")
				require.NoError(t, err)
			}
			invalid := strings.Replace(valid, `"input_cost_per_token":0.000003`, `"input_cost_per_token":-1`, 1)
			require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(invalid), Hash: pricingDocumentHash([]byte(invalid))}))
			_, err := publishAutomaticPricingCatalog(ctx)
			require.NoError(t, err)
			_, err = model.GetActiveBillingPrice(ctx, "gpt-5.4-high")
			require.ErrorIs(t, err, model.ErrAutomaticBillingPriceUnavailable)
			_, err = model.GetActiveBillingPrice(ctx, "gpt-5.4")
			require.NoError(t, err)
			repeated, err := publishAutomaticPricingCatalog(ctx)
			require.NoError(t, err)
			assert.Zero(t, repeated.Deactivated)
			assert.Zero(t, repeated.Published)
			require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(valid), Hash: pricingDocumentHash([]byte(valid))}))
			_, err = publishAutomaticPricingCatalog(ctx)
			require.NoError(t, err)
			_, err = model.GetActiveBillingPrice(ctx, "gpt-5.4-high")
			require.NoError(t, err)
		})
	}
}

func TestAutomaticPricingCanonicalUnavailableCanRecoverFromLowerSourceBase(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	for _, spec := range billingPricingSources {
		require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: string(spec.Source)}))
	}
	requested := "gpt-5.4@effort:high"
	canonical := reasoning.BillingModelCandidates(requested)[0]
	require.Equal(t, "gpt-5.4@effort:high@thinking:on", canonical)
	require.NoError(t, model.DB.Create(&model.Ability{Model: requested, Group: "default", ChannelId: 1, Enabled: true}).Error)
	primary := fmt.Sprintf(`{"gpt-5.4":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002},%q:{"input_cost_per_token":-1,"output_cost_per_token":0.000002}}`, canonical)
	require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "sub2api", Document: model.LongText(primary), Hash: pricingDocumentHash([]byte(primary))}))
	_, err := publishAutomaticPricingCatalog(ctx)
	require.NoError(t, err)
	_, err = model.GetActiveBillingPrice(ctx, canonical)
	require.ErrorIs(t, err, model.ErrAutomaticBillingPriceUnavailable)
	secondary := `{"vendor":{"models":[{"id":"gpt-5.4","cost":{"input":4,"output":8}}]}}`
	require.NoError(t, model.SaveBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "axonhub", Document: model.LongText(secondary), Hash: pricingDocumentHash([]byte(secondary))}))
	result, err := publishAutomaticPricingCatalog(ctx)
	require.NoError(t, err)
	assert.NotContains(t, result.Unconfigured, canonical)
	price, err := model.GetActiveBillingPrice(ctx, canonical)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*price, "1", nil, time.Unix(1000, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000, ContextTokens: 1000})
	require.NoError(t, err)
	assert.Equal(t, "0.004", charge.TotalUSD)
	origins, err := model.ListBillingPriceOrigins(ctx)
	require.NoError(t, err)
	for _, origin := range origins {
		if origin.Model == canonical {
			assert.Equal(t, "axonhub", origin.Origin)
		}
	}
}

func TestAutomaticPricingRetriesPublicationWhenSourceHashIsUnchanged(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	document := `{"retry":{"input_cost_per_token":1e-6,"output_cost_per_token":2e-6}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, document) }))
	defer server.Close()
	specs := []billingPricingSourceSpec{{Source: autopricing.SourceSub2API, URL: server.URL}}
	failPublication := true
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test_pricing_failure", func(tx *gorm.DB) {
		if failPublication && tx.Statement.Table == "billing_price_versions" {
			tx.AddError(fmt.Errorf("publication interrupted"))
		}
	}))
	first, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.ErrorContains(t, err, "publication interrupted")
	assert.Equal(t, 1, first.Changed)
	failPublication = false
	second, err := syncAutomaticPricing(ctx, server.Client(), specs)
	require.NoError(t, err)
	assert.Zero(t, second.Changed)
	assert.Equal(t, 1, second.Published)
	_, err = model.GetActiveBillingPrice(ctx, "retry")
	require.NoError(t, err)
}

func TestAutomaticPricingLongSourceErrorPreservesSnapshotAndChecksLaterSources(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	previous := `{"vendor":{"models":{"stable":{"cost":{"input":1,"output":2}}}}}`
	previousHash := pricingDocumentHash([]byte(previous))
	require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{ParserVersion: "autopricing-v3", Source: "axonhub", Document: model.LongText(previous), Hash: previousHash}))
	malformed := `{"` + strings.Repeat("供应商", 8000) + `":{"models":[42]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/malformed" {
			_, _ = io.WriteString(w, malformed)
			return
		}
		_, _ = io.WriteString(w, `{"later":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	}))
	defer server.Close()
	result, err := syncAutomaticPricing(ctx, server.Client(), []billingPricingSourceSpec{
		{Source: autopricing.SourceAxonHub, URL: server.URL + "/malformed"},
		{Source: autopricing.SourceSub2API, URL: server.URL + "/later"},
	})
	require.ErrorContains(t, err, "unidentified axonhub model")
	assert.Equal(t, 2, result.Checked)
	assert.Equal(t, 1, result.Changed)
	assert.Zero(t, result.Deactivated)
	assert.Len(t, result.Errors, 1)
	rows, err := model.ListBillingPricingSources(ctx)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Source == "axonhub" {
			assert.Equal(t, previous, string(row.Document))
			assert.Equal(t, previousHash, row.Hash)
			assert.Contains(t, row.LastError, "unidentified axonhub model")
			assert.LessOrEqual(t, len(row.LastError), 512)
			assert.True(t, utf8.ValidString(row.LastError))
		}
	}
	for _, name := range []string{"stable", "later"} {
		_, err := model.GetActiveBillingPrice(ctx, name)
		require.NoError(t, err)
	}
}

func TestAutomaticPricingInitialSourceFailureDoesNotBlockFallback(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			_, _ = io.WriteString(w, "not-json")
			return
		}
		_, _ = io.WriteString(w, `{"fallback":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	}))
	defer server.Close()

	result, err := syncAutomaticPricing(ctx, server.Client(), []billingPricingSourceSpec{
		{Source: autopricing.SourceAxonHub, URL: server.URL + "/primary"},
		{Source: autopricing.SourceSub2API, URL: server.URL + "/fallback"},
	})
	require.Error(t, err, "the failed source remains visible to operators")
	require.NotEmpty(t, result.Errors)
	price, err := model.GetActiveBillingPrice(ctx, "fallback")
	require.NoError(t, err, "a valid fallback source must still publish")
	assert.Equal(t, billing.PriceScopeGlobal, price.ScopeKind)
}

func TestAutomaticPricingRequiredDimensionsSelectOneCompleteSource(t *testing.T) {
	automaticPricingTestDB(t)
	ctx := context.Background()
	for _, source := range []struct{ name, document string }{
		{"sub2api", `{"needs-cache":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`},
		{"axonhub", `{"vendor":[{"id":"needs-cache","cost":{"input":3,"output":4,"cache_read":0}}]}`},
	} {
		require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{Source: source.name, Document: model.LongText(source.document), Hash: pricingDocumentHash([]byte(source.document)), ParserVersion: autopricing.ParserVersion}))
	}
	_, err := publishAutomaticPricingCatalog(ctx)
	require.NoError(t, err)
	before, err := model.GetActiveBillingPrice(ctx, "needs-cache")
	require.NoError(t, err)
	assert.Equal(t, "sub2api", before.Origin)
	selected, err := ResolveAndPublishAutomaticPrice(ctx, "needs-cache", billing.PricingRequirements{Dimensions: []string{"p", "c", "cr"}})
	require.NoError(t, err)
	assert.Equal(t, "axonhub", selected.Origin)
	assert.Greater(t, selected.ID, before.ID)
	assert.Positive(t, selected.Provenance.SnapshotID)
	_, err = ResolveAndPublishAutomaticPrice(ctx, "needs-cache", billing.PricingRequirements{Dimensions: []string{"ai"}})
	var missing *billing.PricingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "missing_price_dimension", missing.Code)
	manual, err := model.PublishBillingPrice(ctx, billing.PriceVersion{Model: "needs-cache", Mode: billing.ModeFree, Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "operator promotion"}}, selected.ID, 1)
	require.NoError(t, err)
	_, err = ResolveAndPublishAutomaticPrice(ctx, "needs-cache", billing.PricingRequirements{Dimensions: []string{"cr"}})
	require.ErrorContains(t, err, "automatically managed")
	retained, err := model.GetActiveBillingPrice(ctx, "needs-cache")
	require.NoError(t, err)
	assert.Equal(t, manual.ID, retained.ID)
}

func TestAutomaticPricingFallbackPreservesDeclaredProviderIdentity(t *testing.T) {
	for _, provider := range []string{"openai", "different-provider"} {
		t.Run(provider, func(t *testing.T) {
			automaticPricingTestDB(t)
			ctx := context.Background()
			for _, source := range []struct{ name, document string }{
				{"sub2api", `{"same-name":{"litellm_provider":"openai","input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`},
				{"axonhub", fmt.Sprintf(`{"%s":[{"id":"same-name","cost":{"input":3,"output":4,"cache_read":0}}]}`, provider)},
			} {
				require.NoError(t, model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{Source: source.name, Document: model.LongText(source.document), Hash: pricingDocumentHash([]byte(source.document)), ParserVersion: autopricing.ParserVersion}))
			}
			_, err := publishAutomaticPricingCatalog(ctx)
			require.NoError(t, err)
			before, err := model.GetActiveBillingPrice(ctx, "same-name")
			require.NoError(t, err)
			assert.Equal(t, "sub2api", before.Origin)
			assert.Equal(t, "openai", before.Provenance.Provider)
			selected, err := ResolveAndPublishAutomaticPrice(ctx, "same-name", billing.PricingRequirements{Dimensions: []string{"cr"}})
			if provider == "openai" {
				require.NoError(t, err)
				assert.Equal(t, "axonhub", selected.Origin)
				assert.Equal(t, "openai", selected.Provenance.Provider)
			} else {
				var ambiguous *billing.PricingError
				require.ErrorAs(t, err, &ambiguous)
				assert.Equal(t, "ambiguous_price_identity", ambiguous.Code)
				after, err := model.GetActiveBillingPrice(ctx, "same-name")
				require.NoError(t, err)
				assert.Equal(t, before.ID, after.ID)
			}
		})
	}
}

func TestAutomaticPricingUnconfiguredDiagnosticsFitDurableTaskHistory(t *testing.T) {
	result := &AutomaticPricingSyncResult{Errors: []string{}}
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("%03d", i) + strings.Repeat("\"", 300)
		var reasons []AutomaticPricingRejection
		for _, spec := range billingPricingSources {
			reasons = append(reasons, AutomaticPricingRejection{Source: string(spec.Source), Reason: strings.Repeat("\x01", 150)})
		}
		result.addUnconfigured(name, reasons)
	}
	require.NoError(t, result.boundDiagnostics())
	assert.Equal(t, 120, result.UnconfiguredCount)
	assert.NotEmpty(t, result.UnconfiguredDetails)
	assert.LessOrEqual(t, len(result.UnconfiguredDetails), 100)
	assert.Len(t, result.Unconfigured, len(result.UnconfiguredDetails))
	encoded, err := common.Marshal(result)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(encoded), 60<<10)

	// Fetch errors are merged after publication diagnostics; their escaped
	// bytes must also fit the same durable result instead of overflowing TEXT.
	before := len(result.UnconfiguredDetails)
	for _, spec := range billingPricingSources {
		result.Errors = append(result.Errors, string(spec.Source)+": "+strings.Repeat("\x01", 512))
	}
	encoded, err = common.Marshal(result)
	require.NoError(t, err)
	require.Greater(t, len(encoded), 60<<10)
	require.NoError(t, result.boundDiagnostics())
	assert.Equal(t, 120, result.UnconfiguredCount)
	assert.Len(t, result.Errors, 2)
	assert.Less(t, len(result.UnconfiguredDetails), before)
	assert.Len(t, result.Unconfigured, len(result.UnconfiguredDetails))
	encoded, err = common.Marshal(result)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(encoded), 60<<10)
}
