package model

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func automaticPriceDraft(name, price string) billing.PriceVersion {
	return billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: price}}}
}

func installAutomaticPricingTestSource(t *testing.T, source, hash string) {
	t.Helper()
	require.NoError(t, SaveBillingPricingSource(context.Background(), &BillingPricingSource{Source: source, Document: `{}`, Hash: hash}))
}

type legacyAutomaticBillingActivePrice struct {
	Model     string `gorm:"type:varchar(255);primaryKey"`
	VersionID int64  `gorm:"type:bigint;not null"`
}

func (legacyAutomaticBillingActivePrice) TableName() string { return "billing_active_prices" }

func TestAutomaticBillingPriceContractsAcrossDatabases(t *testing.T) {
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(backend.kind), func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " not configured; automatic billing database contracts remain unverified")
			}
			open := gatewayStartupDatabase(t, backend.kind, dsn)
			db, err := open()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(backend.kind)
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&BillingPriceVersion{}, &legacyAutomaticBillingActivePrice{}))
			legacy, err := billing.PreparePriceVersion(automaticPriceDraft("migrated-manual", "11"))
			require.NoError(t, err)
			definition, err := common.Marshal(legacy)
			require.NoError(t, err)
			hash, err := billing.Hash(legacy)
			require.NoError(t, err)
			row := BillingPriceVersion{Model: legacy.Model, Definition: string(definition), Hash: hash, CreatedAt: 1}
			require.NoError(t, db.Create(&row).Error)
			require.NoError(t, db.Create(&legacyAutomaticBillingActivePrice{Model: legacy.Model, VersionID: row.ID}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&Ability{}, &BillingPriceVersion{}, &BillingActivePrice{}, &BillingPricingSource{}, &BillingPricingConfig{}))
				require.NoError(t, migrateBillingPriceModelIdentity(db))
			}
			recorder := &automaticPricingMigrationRecorder{Interface: logger.Default.LogMode(logger.Silent)}
			require.NoError(t, migrateBillingPriceModelIdentity(db.Session(&gorm.Session{Logger: recorder})))
			assert.Zero(t, recorder.alters, "repeated model identity migration must not rewrite tables")
			installAutomaticPricingTestSource(t, BillingPriceOriginSub2API, strings.Repeat("a", 64))
			published, err := PublishAutomaticBillingPrice(context.Background(), automaticPriceDraft(legacy.Model, "1"), BillingPriceOriginSub2API, legacy.Model, strings.Repeat("a", 64))
			require.NoError(t, err)
			assert.False(t, published, "upgrading an unlabelled custom price preserves manual ownership")
			t.Run("configured_and_retained_model_names", func(t *testing.T) {
				require.NoError(t, db.Create(&[]Ability{
					{Group: "default", Model: "channel-alias", ChannelId: 1, Enabled: true},
					{Group: "other", Model: "channel-alias", ChannelId: 1, Enabled: true},
					{Group: "default", Model: "disabled-alias", ChannelId: 2, Enabled: false},
					{Group: "default", Model: "", ChannelId: 3},
				}).Error)
				names, err := ListAutomaticBillingModelNames(context.Background())
				require.NoError(t, err)
				assert.Equal(t, []string{"channel-alias", "disabled-alias", "migrated-manual"}, names)
				cancelled, cancel := context.WithCancel(context.Background())
				cancel()
				_, err = ListAutomaticBillingModelNames(cancelled)
				require.ErrorIs(t, err, context.Canceled)
			})
			t.Run("versions_and_manual_precedence", testAutomaticBillingPriceVersions)
			t.Run("frozen_embedding_scope", func(t *testing.T) {
				ctx := context.Background()
				draft := automaticPriceDraft("scoped-embedding", "0.1")
				draft.UsageScope = billing.UsageScopeEmbedding
				published, err := PublishBillingPrice(ctx, draft, 0, 7)
				require.NoError(t, err)
				loaded, err := GetActiveBillingPrice(ctx, draft.Model)
				require.NoError(t, err)
				assert.Equal(t, published, loaded)
				quote, err := billing.NewQuote(*loaded, "1", nil, time.Unix(1000, 0))
				require.NoError(t, err)
				encoded, err := common.Marshal(quote)
				require.NoError(t, err)
				var replay billing.Quote
				require.NoError(t, common.Unmarshal(encoded, &replay))
				assert.Equal(t, billing.UsageScopeEmbedding, replay.PriceVersion.UsageScope)
				_, err = billing.Calculate(&replay, billing.Usage{InputTokens: 100, OutputTokens: 1, Complete: true})
				require.ErrorContains(t, err, "embedding usage")
			})
			t.Run("legacy_seed_ownership", testAutomaticBillingLegacySeedOwnership)
			t.Run("manual_publication_race", testAutomaticBillingManualRace)
			t.Run("source_snapshot_fences", testAutomaticBillingSourceFences)
			t.Run("catalog_priority_fence", testAutomaticBillingCatalogFence)
			t.Run("case_sensitive_model_identity", testAutomaticBillingModelIdentity)
			t.Run("atomic_batch_publication", testAutomaticBillingBatchPublication)
			t.Run("atomic_batch_deactivation", testAutomaticBillingBatchDeactivation)
			t.Run("persistent_source_and_config", func(t *testing.T) {
				testAutomaticBillingPricingPersistence(t, open)
			})
		})
	}
}

type automaticPricingMigrationRecorder struct {
	logger.Interface
	alters int
}

func (recorder *automaticPricingMigrationRecorder) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	statement, _ := query()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "ALTER TABLE") {
		recorder.alters++
	}
}

func testAutomaticBillingModelIdentity(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	installAutomaticPricingTestSource(t, BillingPriceOriginSub2API, hash)
	for i, name := range []string{"GLM-5", "glm-5"} {
		price := []string{"2", "3"}[i]
		published, err := PublishAutomaticBillingPrice(ctx, automaticPriceDraft(name, price), BillingPriceOriginSub2API, name, hash)
		require.NoError(t, err)
		require.True(t, published)
	}
	upper, err := GetActiveBillingPrice(ctx, "GLM-5")
	require.NoError(t, err)
	lower, err := GetActiveBillingPrice(ctx, "glm-5")
	require.NoError(t, err)
	assert.Equal(t, "GLM-5", upper.Model)
	assert.Equal(t, "2", upper.Prices[0].Price)
	assert.Equal(t, "glm-5", lower.Model)
	assert.Equal(t, "3", lower.Prices[0].Price)
	assert.NotEqual(t, upper.ID, lower.ID)
	_, err = GetActiveBillingPrice(ctx, "Glm-5")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	for _, name := range []string{"GLM-5", "glm-5"} {
		var versions int64
		require.NoError(t, DB.Model(&BillingPriceVersion{}).Where("model = ?", name).Count(&versions).Error)
		assert.EqualValues(t, 1, versions, "price version lookups must use the same exact identity")
	}
}

func testAutomaticBillingBatchPublication(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("e", 64)
	catalog := make(map[string]string, len(automaticBillingPricingSourceOrder))
	for _, source := range automaticBillingPricingSourceOrder {
		installAutomaticPricingTestSource(t, source, hash)
		catalog[source] = hash
	}
	entry := func(name, price string) AutomaticBillingPrice {
		return AutomaticBillingPrice{Price: automaticPriceDraft(name, price), Source: BillingPriceOriginSub2API, SourceModel: name, SourceHash: hash}
	}
	manual, err := PublishBillingPrice(ctx, automaticPriceDraft("batch-manual", "8"), 0, 0)
	require.NoError(t, err)
	batch := []AutomaticBillingPrice{entry("batch-first", "1"), entry("batch-manual", "2"), entry("batch-second", "3")}
	published, err := PublishAutomaticBillingPrices(ctx, batch, catalog)
	require.NoError(t, err)
	assert.Equal(t, 2, published)
	activeManual, err := GetActiveBillingPrice(ctx, "batch-manual")
	require.NoError(t, err)
	assert.Equal(t, manual.ID, activeManual.ID)
	published, err = PublishAutomaticBillingPrices(ctx, batch, catalog)
	require.NoError(t, err)
	assert.Zero(t, published, "unchanged automatic entries add no versions")
	var before int64
	require.NoError(t, DB.Model(&BillingPriceVersion{}).Count(&before).Error)
	published, err = PublishAutomaticBillingPrices(ctx, []AutomaticBillingPrice{entry("batch-rollback", "1"), entry("batch-invalid", "-1")}, catalog)
	require.Error(t, err)
	assert.Zero(t, published)
	_, err = GetActiveBillingPrice(ctx, "batch-rollback")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "later errors roll back earlier entries in the batch")
	var after int64
	require.NoError(t, DB.Model(&BillingPriceVersion{}).Count(&after).Error)
	assert.Equal(t, before, after)
	installAutomaticPricingTestSource(t, BillingPriceOriginAxonHub, strings.Repeat("f", 64))
	published, err = PublishAutomaticBillingPrices(ctx, []AutomaticBillingPrice{entry("batch-stale", "1")}, catalog)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	assert.Zero(t, published)
	_, err = PublishAutomaticBillingPrices(ctx, make([]AutomaticBillingPrice, 101), catalog)
	require.ErrorContains(t, err, "exceeds 100")
}

func testAutomaticBillingBatchDeactivation(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("9", 64)
	catalog := make(map[string]string, len(automaticBillingPricingSourceOrder))
	for _, source := range automaticBillingPricingSourceOrder {
		installAutomaticPricingTestSource(t, source, hash)
		catalog[source] = hash
	}
	create := func(name string) *billing.PriceVersion {
		published, err := PublishAutomaticBillingPrice(ctx, automaticPriceDraft(name, "2"), BillingPriceOriginSub2API, name, hash, catalog)
		require.NoError(t, err)
		require.True(t, published)
		price, err := GetActiveBillingPrice(ctx, name)
		require.NoError(t, err)
		return price
	}
	retire := create("retire-automatic")
	manualBefore := create("retire-manual-race")
	newerBefore := create("retire-newer-race")
	quote, err := billing.NewQuote(*retire, "1", nil, time.Unix(100, 0))
	require.NoError(t, err)
	manual, err := PublishBillingPrice(ctx, automaticPriceDraft(manualBefore.Model, "8"), manualBefore.ID, 0)
	require.NoError(t, err)
	published, err := PublishAutomaticBillingPrice(ctx, automaticPriceDraft(newerBefore.Model, "3"), BillingPriceOriginSub2API, newerBefore.Model, hash, catalog)
	require.NoError(t, err)
	require.True(t, published)
	newer, err := GetActiveBillingPrice(ctx, newerBefore.Model)
	require.NoError(t, err)
	batch := []AutomaticBillingPriceDeactivation{
		{Model: retire.Model, ExpectedVersionID: retire.ID},
		{Model: manualBefore.Model, ExpectedVersionID: manualBefore.ID},
		{Model: manual.Model, ExpectedVersionID: manual.ID},
		{Model: newerBefore.Model, ExpectedVersionID: newerBefore.ID},
	}
	installAutomaticPricingTestSource(t, BillingPriceOriginAxonHub, strings.Repeat("8", 64))
	count, err := DeactivateAutomaticBillingPrices(ctx, batch, catalog)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	assert.Zero(t, count)
	_, err = GetActiveBillingPrice(ctx, retire.Model)
	require.NoError(t, err)
	catalog[BillingPriceOriginAxonHub] = strings.Repeat("8", 64)
	count, err = DeactivateAutomaticBillingPrices(ctx, batch, catalog)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	_, err = GetActiveBillingPrice(ctx, retire.Model)
	require.ErrorIs(t, err, ErrAutomaticBillingPriceUnavailable)
	for _, expected := range []*billing.PriceVersion{manual, newer} {
		active, err := GetActiveBillingPrice(ctx, expected.Model)
		require.NoError(t, err)
		assert.Equal(t, expected.ID, active.ID)
	}
	var historic BillingPriceVersion
	require.NoError(t, DB.First(&historic, retire.ID).Error)
	decoded, err := decodeBillingPrice(historic)
	require.NoError(t, err)
	assert.Equal(t, retire, decoded)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000000, Complete: true})
	require.NoError(t, err)
	assert.Equal(t, "2", charge.TotalUSD)
	count, err = DeactivateAutomaticBillingPrices(ctx, batch, catalog)
	require.NoError(t, err)
	assert.Zero(t, count, "repeated deactivation is harmless")
	// Never-published unavailable identities also block broader aliases. Creating
	// their markers cannot clobber a price that was published concurrently.
	count, err = DeactivateAutomaticBillingPrices(ctx, []AutomaticBillingPriceDeactivation{
		{Model: "never-priced-high", ExpectedVersionID: 0},
		{Model: manual.Model, ExpectedVersionID: 0},
	}, catalog)
	require.NoError(t, err)
	assert.Zero(t, count, "an unavailable marker is not a retired price version")
	_, err = GetActiveBillingPrice(ctx, "never-priced-high")
	require.ErrorIs(t, err, ErrAutomaticBillingPriceUnavailable)
	manualActive, err := GetActiveBillingPrice(ctx, manual.Model)
	require.NoError(t, err)
	assert.Equal(t, manual.ID, manualActive.ID)
	manualReplacement, err := PublishBillingPrice(ctx, automaticPriceDraft("never-priced-high", "9"), 0, 1)
	require.NoError(t, err)
	require.Positive(t, manualReplacement.ID)
	_, err = GetActiveBillingPrice(ctx, manualReplacement.Model)
	require.NoError(t, err)
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(retire.Model, "4"), BillingPriceOriginSub2API, retire.Model, hash, catalog)
	require.NoError(t, err)
	require.True(t, published, "a complete automatic price replaces an unavailable marker")
	recovered, err := GetActiveBillingPrice(ctx, retire.Model)
	require.NoError(t, err)
	assert.NotEqual(t, retire.ID, recovered.ID)
	// A later integrity error must roll back every marker in this batch.
	rollback := create("retire-rollback")
	corrupt := create("retire-corrupt")
	require.NoError(t, DB.Exec("UPDATE billing_price_versions SET model = ? WHERE id = ?", "wrong-model", corrupt.ID).Error)
	count, err = DeactivateAutomaticBillingPrices(ctx, []AutomaticBillingPriceDeactivation{
		{Model: "unavailable-rollback", ExpectedVersionID: 0},
		{Model: rollback.Model, ExpectedVersionID: rollback.ID},
		{Model: corrupt.Model, ExpectedVersionID: corrupt.ID},
	}, catalog)
	require.ErrorContains(t, err, "integrity mismatch")
	assert.Zero(t, count)
	_, err = GetActiveBillingPrice(ctx, "unavailable-rollback")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = GetActiveBillingPrice(ctx, rollback.Model)
	require.NoError(t, err)
	require.NoError(t, DB.Exec("UPDATE billing_price_versions SET model = ? WHERE id = ?", corrupt.Model, corrupt.ID).Error)
	_, err = DeactivateAutomaticBillingPrices(ctx, make([]AutomaticBillingPriceDeactivation, 101), catalog)
	require.ErrorContains(t, err, "exceeds 100")
}

func testAutomaticBillingPriceVersions(t *testing.T) {
	ctx := context.Background()
	firstDraft := automaticPriceDraft("auto-model", "2")
	sourceHash := strings.Repeat("a", 64)
	published, err := PublishAutomaticBillingPrice(ctx, firstDraft, BillingPriceOriginSub2API, "provider/auto-model", sourceHash)
	require.NoError(t, err)
	require.True(t, published)
	first, err := GetActiveBillingPrice(ctx, firstDraft.Model)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*first, "1", nil, time.Unix(100, 0))
	require.NoError(t, err)
	quoteBefore, err := common.Marshal(quote)
	require.NoError(t, err)
	installAutomaticPricingTestSource(t, BillingPriceOriginAxonHub, strings.Repeat("b", 64))
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(first.Model, "2.00"), BillingPriceOriginAxonHub, first.Model, strings.Repeat("b", 64))
	require.NoError(t, err)
	assert.False(t, published)
	var count int64
	require.NoError(t, DB.Model(&BillingPriceVersion{}).Where("model = ?", first.Model).Count(&count).Error)
	assert.EqualValues(t, 1, count, "canonical equivalent prices do not add versions")
	origins, err := ListBillingPriceOrigins(ctx)
	require.NoError(t, err)
	require.Len(t, origins, 2)
	assert.Equal(t, first.Model, origins[0].Model)
	assert.Equal(t, BillingPriceOriginAxonHub, origins[0].Origin)
	assert.Equal(t, first.ID, origins[0].VersionID)
	assert.Equal(t, BillingPriceOriginManual, origins[1].Origin)
	installAutomaticPricingTestSource(t, BillingPriceOriginAxonHub, sourceHash)
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(first.Model, "3"), BillingPriceOriginAxonHub, first.Model, sourceHash)
	require.NoError(t, err)
	assert.True(t, published)
	second, err := GetActiveBillingPrice(ctx, first.Model)
	require.NoError(t, err)
	assert.Greater(t, second.ID, first.ID)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000000, Complete: true})
	require.NoError(t, err)
	assert.Equal(t, "2", charge.TotalUSD)
	quoteAfter, err := common.Marshal(quote)
	require.NoError(t, err)
	assert.Equal(t, quoteBefore, quoteAfter, "published updates cannot mutate a frozen quote")
	var oldRow BillingPriceVersion
	require.NoError(t, DB.First(&oldRow, first.ID).Error)
	decoded, err := decodeBillingPrice(oldRow)
	require.NoError(t, err)
	assert.Equal(t, first, decoded)

	manual, err := PublishBillingPrice(ctx, automaticPriceDraft(first.Model, "7"), second.ID, 0)
	require.NoError(t, err)
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(first.Model, "4"), BillingPriceOriginSub2API, first.Model, sourceHash)
	require.NoError(t, err)
	assert.False(t, published, "actor zero manual publication still takes ownership")
	active, err := GetActiveBillingPrice(ctx, first.Model)
	require.NoError(t, err)
	assert.Equal(t, manual.ID, active.ID)
	_, err = RestoreAutomaticBillingPrice(ctx, firstDraft, BillingPriceOriginSub2API, first.Model, sourceHash, second.ID)
	require.ErrorIs(t, err, ErrBillingPriceConflict)
	restored, err := RestoreAutomaticBillingPrice(ctx, automaticPriceDraft(first.Model, "7"), BillingPriceOriginSub2API, first.Model, sourceHash, manual.ID)
	require.NoError(t, err)
	assert.Equal(t, manual.ID, restored.ID, "same-price restore transfers ownership without a redundant version")
	published, err = PublishAutomaticBillingPrice(ctx, firstDraft, BillingPriceOriginSub2API, first.Model, sourceHash)
	require.NoError(t, err)
	assert.True(t, published)
}

func testAutomaticBillingLegacySeedOwnership(t *testing.T) {
	ctx := context.Background()
	seeds, _, err := InitialBillingPriceCatalog()
	require.NoError(t, err)
	require.NotEmpty(t, seeds)
	seed := seeds[0]
	seedDefinition, err := common.Marshal(seed)
	require.NoError(t, err)
	seedHash, err := billing.Hash(seed)
	require.NoError(t, err)
	row := BillingPriceVersion{Model: seed.Model, Definition: string(seedDefinition), Hash: seedHash, CreatedAt: 10}
	require.NoError(t, DB.Create(&row).Error)
	require.NoError(t, DB.Create(&BillingActivePrice{Model: seed.Model, VersionID: row.ID}).Error)
	origins, err := ListBillingPriceOrigins(ctx)
	require.NoError(t, err)
	found := false
	for _, origin := range origins {
		if origin.Model == seed.Model {
			found = true
			assert.Equal(t, BillingPriceOriginLegacySeed, origin.Origin)
		}
	}
	assert.True(t, found)
	installAutomaticPricingTestSource(t, BillingPriceOriginSub2API, strings.Repeat("c", 64))
	published, err := PublishAutomaticBillingPrice(ctx, automaticPriceDraft(seed.Model, "9"), BillingPriceOriginSub2API, seed.Model, strings.Repeat("c", 64))
	require.NoError(t, err)
	assert.True(t, published, "only a verified historical seed may be adopted")

	installAutomaticPricingTestSource(t, BillingPriceOriginAxonHub, strings.Repeat("d", 64))
	for _, test := range []struct{ name, origin string }{{"legacy-custom", ""}, {"unknown-origin", "other-import"}} {
		manual, err := PublishBillingPrice(ctx, automaticPriceDraft(test.name, "6"), 0, 0)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&BillingActivePrice{}).Where("model = ?", test.name).Update("origin", test.origin).Error)
		published, err := PublishAutomaticBillingPrice(ctx, automaticPriceDraft(test.name, "1"), BillingPriceOriginAxonHub, test.name, strings.Repeat("d", 64))
		require.NoError(t, err)
		assert.False(t, published)
		active, err := GetActiveBillingPrice(ctx, test.name)
		require.NoError(t, err)
		assert.Equal(t, manual.ID, active.ID)
	}
	// The old seed hash cannot grant ownership to changed or corrupt contents.
	corruptSeed := seeds[1]
	corruptHash, err := billing.Hash(corruptSeed)
	require.NoError(t, err)
	corrupt := BillingPriceVersion{Model: corruptSeed.Model, Definition: "{}", Hash: corruptHash, CreatedAt: 10}
	require.NoError(t, DB.Create(&corrupt).Error)
	require.NoError(t, DB.Create(&BillingActivePrice{Model: corrupt.Model, VersionID: corrupt.ID}).Error)
	installAutomaticPricingTestSource(t, BillingPriceOriginSub2API, strings.Repeat("a", 64))
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(corrupt.Model, "1"), BillingPriceOriginSub2API, corrupt.Model, strings.Repeat("a", 64))
	require.Error(t, err)
	assert.False(t, published)
	var active BillingActivePrice
	require.NoError(t, DB.Where("model = ?", corrupt.Model).First(&active).Error)
	assert.Equal(t, corrupt.ID, active.VersionID)
}

func testAutomaticBillingManualRace(t *testing.T) {
	ctx := context.Background()
	draft := automaticPriceDraft("manual-race", "1")
	hash := strings.Repeat("e", 64)
	installAutomaticPricingTestSource(t, BillingPriceOriginSub2API, hash)
	installAutomaticPricingTestSource(t, BillingPriceOriginAxonHub, hash)
	published, err := PublishAutomaticBillingPrice(ctx, draft, BillingPriceOriginSub2API, draft.Model, hash)
	require.NoError(t, err)
	require.True(t, published)
	initial, err := GetActiveBillingPrice(ctx, draft.Model)
	require.NoError(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var manual *billing.PriceVersion
	var manualErr, autoErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		manual, manualErr = PublishBillingPrice(ctx, automaticPriceDraft(draft.Model, "7"), initial.ID, 0)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, autoErr = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(draft.Model, "2"), BillingPriceOriginAxonHub, draft.Model, hash)
	}()
	close(start)
	wg.Wait()
	require.NoError(t, autoErr)
	if errors.Is(manualErr, ErrBillingPriceConflict) {
		active, err := GetActiveBillingPrice(ctx, draft.Model)
		require.NoError(t, err)
		manual, manualErr = PublishBillingPrice(ctx, automaticPriceDraft(draft.Model, "7"), active.ID, 0)
	}
	require.NoError(t, manualErr)
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(draft.Model, "3"), BillingPriceOriginSub2API, draft.Model, hash)
	require.NoError(t, err)
	assert.False(t, published)
	active, err := GetActiveBillingPrice(ctx, draft.Model)
	require.NoError(t, err)
	assert.Equal(t, manual.ID, active.ID)
	assert.Equal(t, "7", active.Prices[0].Price)
}

func testAutomaticBillingSourceFences(t *testing.T) {
	ctx := context.Background()
	oldHash, newHash := strings.Repeat("a", 64), strings.Repeat("b", 64)
	draft := automaticPriceDraft("snapshot-fence", "1")
	published, err := PublishAutomaticBillingPrice(ctx, draft, BillingPriceOriginOpenRouter, draft.Model, oldHash)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	assert.False(t, published, "publications require an existing source snapshot")
	old := BillingPricingSource{Source: BillingPriceOriginOpenRouter, Document: `{"old":true}`, Hash: oldHash}
	require.ErrorIs(t, SaveBillingPricingSourceCAS(ctx, &old, "missing-old-hash"), ErrBillingPricingSourceConflict)
	require.NoError(t, SaveBillingPricingSourceCAS(ctx, &old, ""))
	newer := old
	newer.Hash, newer.Document = newHash, `{"new":true}`
	newer.LastError, newer.CheckedAt = "check failed", 10
	require.NoError(t, SaveBillingPricingSourceCAS(ctx, &newer, oldHash))
	old.LastError, old.CheckedAt = "slow old response", 20
	require.ErrorIs(t, SaveBillingPricingSourceCAS(ctx, &old, oldHash), ErrBillingPricingSourceConflict)
	var persisted BillingPricingSource
	require.NoError(t, DB.Where("source = ?", old.Source).Take(&persisted).Error)
	assert.Equal(t, newer, persisted)
	published, err = PublishAutomaticBillingPrice(ctx, draft, old.Source, draft.Model, oldHash)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	assert.False(t, published)
	published, err = PublishAutomaticBillingPrice(ctx, draft, old.Source, draft.Model, newHash)
	require.NoError(t, err)
	assert.True(t, published)
	active, err := GetActiveBillingPrice(ctx, draft.Model)
	require.NoError(t, err)
	_, err = RestoreAutomaticBillingPrice(ctx, automaticPriceDraft(draft.Model, "2"), old.Source, draft.Model, oldHash, active.ID)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	newer.LastError = ""
	newer.CheckedAt = 30
	require.NoError(t, SaveBillingPricingSourceCAS(ctx, &newer, newHash))
	require.NoError(t, DB.Where("source = ?", old.Source).Take(&persisted).Error)
	assert.Equal(t, newer, persisted, "same-snapshot checks may update metadata")
}

func testAutomaticBillingCatalogFence(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("c", 64)
	for _, source := range automaticBillingPricingSourceOrder {
		record := BillingPricingSource{Source: source, Document: "{}", Hash: hash}
		if source == BillingPriceOriginModelsDev {
			record.Document, record.Hash = "", ""
		}
		require.NoError(t, SaveBillingPricingSource(ctx, &record))
	}
	guard := map[string]string{
		BillingPriceOriginSub2API: hash, BillingPriceOriginAxonHub: hash,
		BillingPriceOriginModelsDev: "", BillingPriceOriginOpenRouter: hash,
	}
	draft := automaticPriceDraft("catalog-priority", "1")
	published, err := PublishAutomaticBillingPrice(ctx, draft, BillingPriceOriginOpenRouter, draft.Model, hash, guard)
	require.NoError(t, err)
	assert.True(t, published)
	active, err := GetActiveBillingPrice(ctx, draft.Model)
	require.NoError(t, err)
	// A new higher-priority catalog invalidates a fallback decision, even
	// though the selected fallback source still has exactly the same hash.
	newHash := strings.Repeat("d", 64)
	installAutomaticPricingTestSource(t, BillingPriceOriginSub2API, newHash)
	published, err = PublishAutomaticBillingPrice(ctx, automaticPriceDraft(draft.Model, "2"), BillingPriceOriginOpenRouter, draft.Model, hash, guard)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	assert.False(t, published)
	_, err = RestoreAutomaticBillingPrice(ctx, draft, BillingPriceOriginOpenRouter, draft.Model, hash, active.ID, guard)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	guard[BillingPriceOriginSub2API] = newHash
	_, err = RestoreAutomaticBillingPrice(ctx, draft, BillingPriceOriginOpenRouter, draft.Model, hash, active.ID, guard)
	require.NoError(t, err)
	// An omitted catalog key cannot be interpreted as an absent cache.
	delete(guard, BillingPriceOriginModelsDev)
	_, err = PublishAutomaticBillingPrice(ctx, draft, BillingPriceOriginOpenRouter, draft.Model, hash, guard)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict)
	guard[BillingPriceOriginModelsDev] = ""
	require.NoError(t, DB.Where("source = ?", BillingPriceOriginModelsDev).Delete(&BillingPricingSource{}).Error)
	_, err = PublishAutomaticBillingPrice(ctx, draft, BillingPriceOriginOpenRouter, draft.Model, hash, guard)
	require.ErrorIs(t, err, ErrBillingPricingSourceConflict, "empty source hashes still require a placeholder row")
	var count int64
	require.NoError(t, DB.Model(&BillingPriceVersion{}).Where("model = ?", draft.Model).Count(&count).Error)
	assert.EqualValues(t, 1, count, "rejected stale decisions never create a version")
}

func testAutomaticBillingPricingPersistence(t *testing.T, open func() (*gorm.DB, error)) {
	ctx := context.Background()
	config, err := GetBillingPricingConfig(ctx)
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.Equal(t, 30, config.CheckIntervalMinutes)
	var count int64
	require.NoError(t, DB.Model(&BillingPricingConfig{}).Count(&count).Error)
	assert.Zero(t, count, "reading defaults must not create persistent state")
	config.Enabled, config.CheckIntervalMinutes = false, 5
	require.NoError(t, SaveBillingPricingConfig(ctx, config))
	for _, interval := range []int{4, 1441} {
		invalid := *config
		invalid.CheckIntervalMinutes = interval
		require.Error(t, SaveBillingPricingConfig(ctx, &invalid))
	}
	document := `{"padding":"` + strings.Repeat("a", 70000) + `"}`
	source := BillingPricingSource{Source: BillingPriceOriginSub2API, Document: LongText(document), Hash: strings.Repeat("f", 64), ETag: "etag-1", LastModified: "last-modified", CheckedAt: 123, UpdatedAt: 120, ModelCount: 1}
	require.NoError(t, SaveBillingPricingSource(ctx, &source))
	source.LastError = "upstream unavailable"
	source.CheckedAt = 130
	require.NoError(t, SaveBillingPricingSource(ctx, &source))
	staleBundled := source
	staleBundled.Document, staleBundled.CheckedAt = "old bundle", 1
	require.NoError(t, EnsureBillingPricingSource(ctx, &staleBundled))
	reopened, err := open()
	require.NoError(t, err)
	connection, err := reopened.DB()
	require.NoError(t, err)
	original := DB
	DB = reopened
	t.Cleanup(func() { DB = original; assert.NoError(t, connection.Close()) })
	loaded, err := GetBillingPricingConfig(ctx)
	require.NoError(t, err)
	assert.False(t, loaded.Enabled)
	assert.Equal(t, 5, loaded.CheckIntervalMinutes)
	sources, err := ListBillingPricingSources(ctx)
	require.NoError(t, err)
	var restoredSource BillingPricingSource
	for _, candidate := range sources {
		if candidate.Source == source.Source {
			restoredSource = candidate
		}
	}
	assert.Equal(t, source, restoredSource, "full snapshots above MySQL TEXT size survive reconnection")
	encoded, err := common.Marshal(sources)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "padding", "public source metadata must not expose cached documents")
	source.LastError = ""
	require.NoError(t, SaveBillingPricingSource(ctx, &source))
	sources, err = ListBillingPricingSources(ctx)
	require.NoError(t, err)
	for _, candidate := range sources {
		if candidate.Source == source.Source {
			assert.Empty(t, candidate.LastError)
		}
	}
}
