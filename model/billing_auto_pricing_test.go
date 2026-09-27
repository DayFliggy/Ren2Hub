package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func automaticPriceDraft(name, price string) billing.PriceVersion {
	return billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: price}}}
}

func installPricingSnapshot(t *testing.T, source, document string) (BillingPricingSource, map[string]autopricing.Entry) {
	t.Helper()
	entries, err := autopricing.Parse(autopricing.SourceID(source), []byte(document))
	require.NoError(t, err)
	row := BillingPricingSource{Source: source, Document: LongText(document), Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(document))), ParserVersion: autopricing.ParserVersion, URL: "https://example.test/catalog.json", ModelCount: len(entries)}
	require.NoError(t, SaveBillingPricingSource(context.Background(), &row))
	return row, entries
}

func TestAutomaticBillingPriceContractsAcrossDatabases(t *testing.T) {
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(backend.kind), func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " not configured; database contract remains unverified")
			}
			db, err := gatewayStartupDatabase(t, backend.kind, dsn)()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			if backend.kind == common.DatabaseTypeSQLite {
				connection.SetMaxOpenConns(1)
			}
			previous, kind := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(backend.kind)
			t.Cleanup(func() { DB = previous; common.SetMainDatabaseType(kind); assert.NoError(t, connection.Close()) })
			require.NoError(t, db.AutoMigrate(&BillingPriceVersion{}, &BillingActivePrice{}, &BillingPricingSource{}, &BillingPricingSnapshot{}, &BillingPricingConfig{}, &Ability{}))
			require.NoError(t, migrateBillingPriceModelIdentity(db))
			t.Run("archive_and_cas", testPricingSnapshotArchive)
			t.Run("versions_manual_and_restore", testPricingAutomaticOwnership)
			t.Run("batch_and_deactivation_fences", testPricingAutomaticBatch)
			t.Run("source_evidence_authenticity", testPricingSourceEvidence)
			t.Run("concurrent_publication", testPricingConcurrentPublication)
			t.Run("case_sensitive_identity", func(t *testing.T) {
				ctx := context.Background()
				upper, err := PublishBillingPrice(ctx, automaticPriceDraft("Case-Sensitive-Price", "1"), 0, 1)
				require.NoError(t, err)
				lower, err := PublishBillingPrice(ctx, automaticPriceDraft("case-sensitive-price", "2"), 0, 1)
				require.NoError(t, err)
				assert.NotEqual(t, upper.ID, lower.ID)
				for _, expected := range []*billing.PriceVersion{upper, lower} {
					actual, err := GetActiveBillingPrice(ctx, expected.Model)
					require.NoError(t, err)
					assert.Equal(t, expected.ID, actual.ID)
				}
			})
			t.Run("channel_scope", func(t *testing.T) {
				ctx := context.Background()
				draft := automaticPriceDraft("scoped-model", "1")
				global, err := PublishBillingPrice(ctx, draft, 0, 1)
				require.NoError(t, err)
				draft.ScopeKind, draft.ChannelID = billing.PriceScopeChannel, 42
				channel, err := PublishBillingPrice(ctx, draft, 0, 1)
				require.NoError(t, err)
				selected, err := GetActiveBillingPriceForChannel(ctx, draft.Model, 42)
				require.NoError(t, err)
				assert.Equal(t, channel, selected)
				selected, err = GetActiveBillingPriceForChannel(ctx, draft.Model, 43)
				require.NoError(t, err)
				assert.Equal(t, global, selected)
				_, err = PublishBillingPrice(ctx, draft, global.ID, 1)
				require.ErrorIs(t, err, ErrBillingPriceConflict)
			})
		})
	}
}

func testPricingSnapshotArchive(t *testing.T) {
	ctx := context.Background()
	first, _ := installPricingSnapshot(t, "sub2api", `{"archive":{"input_cost_per_token":0.000001,"output_cost_per_token":0}}`)
	require.Positive(t, first.SnapshotID)
	same := first
	same.CheckedAt = 123
	same.LastError = "network unavailable"
	require.NoError(t, SaveBillingPricingSourceCAS(ctx, &same, first.Hash))
	assert.Equal(t, first.SnapshotID, same.SnapshotID)
	second, _ := installPricingSnapshot(t, "sub2api", `{"archive":{"input_cost_per_token":0.000002,"output_cost_per_token":0}}`)
	require.NotEqual(t, first.SnapshotID, second.SnapshotID)
	require.ErrorIs(t, SaveBillingPricingSourceCAS(ctx, &first, first.Hash), ErrBillingPricingSourceConflict)
	rows, err := ListBillingPricingSources(ctx)
	require.NoError(t, err)
	var current BillingPricingSource
	for _, row := range rows {
		if row.Source == "sub2api" {
			current = row
		}
	}
	assert.Equal(t, second.Document, current.Document)
	assert.Equal(t, second.Hash, current.Hash)
	assert.GreaterOrEqual(t, current.SnapshotCount, int64(2))
	var old BillingPricingSnapshot
	require.NoError(t, DB.First(&old, first.SnapshotID).Error)
	assert.Equal(t, first.Document, old.Document)
	require.Error(t, DB.Model(&old).Update("document", "{}").Error)
	require.Error(t, DB.Delete(&old).Error)
	snapshots, err := ListBillingPricingSnapshots(ctx, "sub2api", 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(snapshots), 2)
	assert.Empty(t, snapshots[0].Document)
	invalid := second
	invalid.Hash = first.Hash
	require.ErrorContains(t, SaveBillingPricingSourceCAS(ctx, &invalid, second.Hash), "integrity")
	var count int64
	require.NoError(t, DB.Model(&BillingPricingSnapshot{}).Where("source = ? AND hash = ?", "sub2api", second.Hash).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.Error(t, SaveBillingPricingSource(ctx, &BillingPricingSource{Source: "retired", Document: "{}"}))
}

func testPricingAutomaticOwnership(t *testing.T) {
	ctx := context.Background()
	source, entries := installPricingSnapshot(t, "sub2api", `{"owned":{"input_cost_per_token":0.000001,"output_cost_per_token":0}}`)
	draft := entries["owned"].Price
	changed, err := PublishAutomaticBillingPrice(ctx, draft, "sub2api", "owned", source.Hash)
	require.NoError(t, err)
	require.True(t, changed)
	first, err := GetActiveBillingPrice(ctx, "owned")
	require.NoError(t, err)
	assert.Equal(t, source.SnapshotID, first.Provenance.SnapshotID)
	assert.NotEmpty(t, first.Provenance.Fields)
	quote, err := billing.NewQuote(*first, "1", nil, time.Now())
	require.NoError(t, err)
	changed, err = PublishAutomaticBillingPrice(ctx, draft, "sub2api", "owned", source.Hash)
	require.NoError(t, err)
	assert.False(t, changed)
	manual, err := PublishBillingPrice(ctx, automaticPriceDraft("owned", "7"), first.ID, 7)
	require.NoError(t, err)
	changed, err = PublishAutomaticBillingPrice(ctx, draft, "sub2api", "owned", source.Hash)
	require.NoError(t, err)
	assert.False(t, changed)
	_, err = RestoreAutomaticBillingPrice(ctx, draft, "sub2api", "owned", source.Hash, first.ID)
	require.ErrorIs(t, err, ErrBillingPriceConflict)
	restored, err := RestoreAutomaticBillingPrice(ctx, draft, "sub2api", "owned", source.Hash, manual.ID)
	require.NoError(t, err)
	assert.Greater(t, restored.ID, manual.ID)
	assert.Equal(t, source.SnapshotID, restored.Provenance.SnapshotID)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000000, Complete: true})
	require.NoError(t, err)
	assert.Equal(t, "1", charge.TotalUSD)
	next, _ := installPricingSnapshot(t, "sub2api", `{"owned":{"input_cost_per_token":0.000003,"output_cost_per_token":0}}`)
	_, err = PublishAutomaticBillingPrice(ctx, draft, "sub2api", "owned", source.Hash)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	assert.NotEqual(t, source.Hash, next.Hash)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "configured-alias", ChannelId: 1, Enabled: true}).Error)
	names, err := ListAutomaticBillingModelNames(ctx)
	require.NoError(t, err)
	assert.Contains(t, names, "owned")
	assert.Contains(t, names, "configured-alias")
}

func testPricingAutomaticBatch(t *testing.T) {
	ctx := context.Background()
	source, entries := installPricingSnapshot(t, "sub2api", `{"batch-a":{"input_cost_per_token":0.000001,"output_cost_per_token":0},"batch-b":{"input_cost_per_token":0.000002,"output_cost_per_token":0},"batch-manual":{"input_cost_per_token":0.000003,"output_cost_per_token":0}}`)
	fallback, _ := installPricingSnapshot(t, "axonhub", `{"p":[{"id":"batch-a","cost":{"input":3,"output":0}}]}`)
	guard := map[string]string{"sub2api": source.Hash, "axonhub": fallback.Hash}
	manual, err := PublishBillingPrice(ctx, automaticPriceDraft("batch-manual", "9"), 0, 1)
	require.NoError(t, err)
	batch := []AutomaticBillingPrice{}
	for _, name := range []string{"batch-a", "batch-b", "batch-manual"} {
		batch = append(batch, AutomaticBillingPrice{Price: entries[name].Price, Source: "sub2api", SourceModel: name, SourceHash: source.Hash})
	}
	count, err := PublishAutomaticBillingPrices(ctx, batch, guard)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	count, err = PublishAutomaticBillingPrices(ctx, batch, guard)
	require.NoError(t, err)
	assert.Zero(t, count)
	before, err := GetActiveBillingPrice(ctx, "batch-a")
	require.NoError(t, err)
	nextSource, nextEntries := installPricingSnapshot(t, "sub2api", `{"batch-a":{"input_cost_per_token":0.000008,"output_cost_per_token":0},"batch-b":{"input_cost_per_token":0.000002,"output_cost_per_token":0}}`)
	guard["sub2api"] = nextSource.Hash
	invalid := AutomaticBillingPrice{Price: nextEntries["batch-b"].Price, Source: "sub2api", SourceModel: "batch-b", SourceHash: nextSource.Hash}
	invalid.Price.Model = "batch-invalid"
	invalid.Price.Expression = "v1:p *"
	changed := AutomaticBillingPrice{Price: nextEntries["batch-a"].Price, Source: "sub2api", SourceModel: "batch-a", SourceHash: nextSource.Hash}
	_, err = PublishAutomaticBillingPrices(ctx, []AutomaticBillingPrice{changed, invalid}, guard)
	require.Error(t, err)
	after, err := GetActiveBillingPrice(ctx, "batch-a")
	require.NoError(t, err)
	assert.Equal(t, before.ID, after.ID)
	manualAfter, err := GetActiveBillingPrice(ctx, "batch-manual")
	require.NoError(t, err)
	assert.Equal(t, manual.ID, manualAfter.ID)
	count, err = DeactivateAutomaticBillingPrices(ctx, []AutomaticBillingPriceDeactivation{{Model: "batch-a", ExpectedVersionID: before.ID}, {Model: "batch-manual", ExpectedVersionID: manual.ID}}, guard)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	_, err = GetActiveBillingPrice(ctx, "batch-a")
	require.ErrorIs(t, err, ErrAutomaticBillingPriceUnavailable)
	_, err = GetActiveBillingPrice(ctx, "batch-manual")
	require.NoError(t, err)
	guard["sub2api"] = "stale"
	_, err = PublishAutomaticBillingPrices(ctx, batch, guard)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	_, err = DeactivateAutomaticBillingPrices(ctx, []AutomaticBillingPriceDeactivation{{Model: "batch-b", ExpectedVersionID: 0}}, guard)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	_, err = PublishAutomaticBillingPrices(ctx, make([]AutomaticBillingPrice, 101), guard)
	require.Error(t, err)
}

func testPricingSourceEvidence(t *testing.T) {
	ctx := context.Background()
	source, entries := installPricingSnapshot(t, "sub2api", `{"evidence":{"litellm_provider":"openai","input_cost_per_token":0.000001,"output_cost_per_token":0}}`)
	draft := entries["evidence"].Price
	changed, err := PublishAutomaticBillingPrice(ctx, draft, "sub2api", "evidence", source.Hash)
	require.NoError(t, err)
	require.True(t, changed)
	before, err := GetActiveBillingPrice(ctx, "evidence")
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		mutate func(*billing.PriceVersion)
	}{
		{"different_rate", func(price *billing.PriceVersion) { price.Expression = "v1:p * 999 + c * 0" }},
		{"fabricated_path", func(price *billing.PriceVersion) { price.Provenance.Fields[0].SourcePath = "/not-present" }},
		{"fabricated_raw_value", func(price *billing.PriceVersion) { price.Provenance.Fields[0].RawValue = "999" }},
		{"fabricated_conversion", func(price *billing.PriceVersion) { price.Provenance.Fields[0].Conversion = "multiply_by_999" }},
		{"different_provider", func(price *billing.PriceVersion) { price.Provenance.Provider = "other" }},
		{"different_parser", func(price *billing.PriceVersion) { price.Provenance.ParserVersion = "unregistered" }},
		{"added_fixed_fee", func(price *billing.PriceVersion) { price.FlatFee = "1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			forged := draft
			forged.Provenance = billing.ClonePriceProvenance(draft.Provenance)
			test.mutate(&forged)
			_, err := PublishAutomaticBillingPrice(ctx, forged, "sub2api", "evidence", source.Hash)
			require.Error(t, err)
			after, err := GetActiveBillingPrice(ctx, "evidence")
			require.NoError(t, err)
			assert.Equal(t, before.ID, after.ID)
		})
	}
	_, err = PublishAutomaticBillingPrice(ctx, draft, "sub2api", "wrong-model", source.Hash)
	require.ErrorContains(t, err, "source model mismatch")
	// An old version must keep its original archive after a successful refresh.
	next, nextEntries := installPricingSnapshot(t, "sub2api", `{"evidence":{"litellm_provider":"openai","input_cost_per_token":0.000002,"output_cost_per_token":0}}`)
	archived, err := GetBillingPricingSnapshot(ctx, before.Provenance.SnapshotID)
	require.NoError(t, err)
	assert.Equal(t, source.Document, archived.Document)
	require.NoError(t, DB.Session(&gorm.Session{SkipHooks: true}).Model(&BillingPricingSnapshot{}).Where("id = ?", next.SnapshotID).Update("document", "{}").Error)
	_, err = PublishAutomaticBillingPrice(ctx, nextEntries["evidence"].Price, "sub2api", "evidence", next.Hash)
	require.ErrorContains(t, err, "snapshot integrity mismatch")
	require.NoError(t, DB.Session(&gorm.Session{SkipHooks: true}).Model(&BillingPricingSnapshot{}).Where("id = ?", next.SnapshotID).Update("document", next.Document).Error)
}

func testPricingConcurrentPublication(t *testing.T) {
	ctx := context.Background()
	first, err := PublishBillingPrice(ctx, automaticPriceDraft("price-concurrency", "1"), 0, 1)
	require.NoError(t, err)
	start := make(chan struct{})
	var wait sync.WaitGroup
	var outcomes [2]error
	for i := range 2 {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			_, outcomes[i] = PublishBillingPrice(ctx, automaticPriceDraft("price-concurrency", fmt.Sprint(i+2)), first.ID, i+2)
		}(i)
	}
	close(start)
	wait.Wait()
	assert.NotEqual(t, outcomes[0] == nil, outcomes[1] == nil, "one CAS publication wins")
	for _, err := range outcomes {
		if err != nil {
			require.ErrorIs(t, err, ErrBillingPriceConflict)
		}
	}
	var versions, pointers int64
	require.NoError(t, DB.Model(&BillingPriceVersion{}).Where("model = ?", "price-concurrency").Count(&versions).Error)
	require.NoError(t, DB.Model(&BillingActivePrice{}).Where("model = ?", "price-concurrency").Count(&pointers).Error)
	assert.EqualValues(t, 2, versions)
	assert.EqualValues(t, 1, pointers)
}
