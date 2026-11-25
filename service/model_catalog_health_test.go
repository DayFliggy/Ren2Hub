package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func resetCatalogHealthStores(t *testing.T, channelIDs ...int) {
	t.Helper()
	resetRouteObservations(t)
	for _, id := range channelIDs {
		routeQualities.invalidate(id)
	}
	t.Cleanup(func() {
		for _, id := range channelIDs {
			routeQualities.invalidate(id)
		}
	})
}

func TestCatalogHealthMappedAliasesShareOnlyBreaker(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11, 22)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "alias-owner"}).Error)

	// Channel 11 maps two public aliases to the same internal target; channel 22 is the reference peer.
	createRouteChannel(t, db, 11, "alias-a,alias-b")
	createRouteChannel(t, db, 22, "alias-a,alias-b")
	secretTarget := "internal-secret-target-v9"
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("model_mapping", `{"alias-a":"`+secretTarget+`","alias-b":"`+secretTarget+`"}`).Error)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	// Seed >=50 clean samples for alias-a only (none for alias-b).
	now := time.Now()
	for range catalogQualityMinSamples {
		routeQualities.add(routeQualityKey{"alias-a", routeObservationKey{11, secretTarget, relayconstant.RelayModeChatCompletions, true}}, true, 120, 100, time.Second, now)
		routeQualities.add(routeQualityKey{"alias-a", routeObservationKey{22, "alias-a", relayconstant.RelayModeChatCompletions, true}}, true, 120, 100, time.Second, now)
	}
	routeQualities.evaluate(now)

	view, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	offersByKey := map[catalogOfferKey]CatalogOffer{}
	for _, offer := range view.Offers {
		offersByKey[catalogOfferKey{offer.ChannelID, offer.ModelID}] = offer
	}
	require.Equal(t, CatalogOfferHealth{Availability: CatalogAvailabilityNoRestriction, Quality: CatalogQualityNoDowngrade}, offersByKey[catalogOfferKey{11, "alias-a"}].Health)
	require.Equal(t, CatalogOfferHealth{Availability: CatalogAvailabilityNoRestriction, Quality: CatalogQualityUnrated}, offersByKey[catalogOfferKey{11, "alias-b"}].Health, "zero-sample alias-b must never borrow alias-a quality samples")

	// Open breaker on (channel 11, secretTarget): both aliases must surface paused, while secretTarget never leaks.
	epoch := routeObservations.epoch
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, routeBreakerKey{11, secretTarget}, nil, routeBreakerSample{success: false, status: 502, code: "upstream_error"})
	}
	view, err = BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	for _, offer := range view.Offers {
		offersByKey[catalogOfferKey{offer.ChannelID, offer.ModelID}] = offer
	}
	require.Equal(t, CatalogAvailabilityPaused, offersByKey[catalogOfferKey{11, "alias-a"}].Health.Availability)
	require.Equal(t, CatalogAvailabilityPaused, offersByKey[catalogOfferKey{11, "alias-b"}].Health.Availability, "shared mapped target shares breaker state")
	require.Equal(t, CatalogQualityNoDowngrade, offersByKey[catalogOfferKey{11, "alias-a"}].Health.Quality)
	require.Equal(t, CatalogQualityUnrated, offersByKey[catalogOfferKey{11, "alias-b"}].Health.Quality)

	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secretTarget, "internal mapped model target must never appear in catalog")
	require.NotContains(t, string(encoded), "upstream_error")
	require.NotContains(t, string(encoded), "consecutive")

	// A token restricted strictly to alias-b must not infer alias-a from channel 11's summary counts.
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 7).Updates(map[string]any{"model_limits_enabled": true, "model_limits": "alias-b"}).Error)
	limitedView, err := BuildModelCatalog(context.Background(), 17, 7)
	require.NoError(t, err)
	require.Len(t, limitedView.Models, 1)
	require.Equal(t, "alias-b", limitedView.Models[0].ModelName)
	var ch11 *CatalogChannel
	for i := range limitedView.Channels {
		if limitedView.Channels[i].ID == 11 {
			ch11 = &limitedView.Channels[i]
		}
	}
	require.NotNil(t, ch11)
	require.NotNil(t, ch11.Health)
	require.Equal(t, CatalogChannelHealth{VisibleModels: 1, AffectedModels: 1, EvaluatedModels: 0, UnratedModels: 0}, *ch11.Health)
	limitedEncoded, err := common.Marshal(limitedView)
	require.NoError(t, err)
	require.NotContains(t, string(limitedEncoded), "alias-a")
	require.NotContains(t, string(limitedEncoded), secretTarget)
}

func TestCatalogHealthSamplingAndFreshness(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	nowMilli := now.UnixMilli()

	offers := []CatalogOffer{
		{ChannelID: 1, ModelID: "m-cold"},
		{ChannelID: 1, ModelID: "m-49"},
		{ChannelID: 1, ModelID: "m-50-clean"},
		{ChannelID: 1, ModelID: "m-20-latency-only"},
		{ChannelID: 1, ModelID: "m-stale"},
		{ChannelID: 1, ModelID: "m-worst-stream"},
		{ChannelID: 1, ModelID: "m-half-open"},
		{ChannelID: 1, ModelID: "m-paused-and-cooled"},
		{ChannelID: 2, ModelID: "m-cooled-only"},
		{ChannelID: 3, ModelID: "m-bad-map"},
		{ChannelID: 4, ModelID: "m-cycle-cooled"},
	}
	mappings := map[int]string{
		3: `{"broken": 123}`,
		4: `{"m-cycle-cooled":"x","x":"m-cycle-cooled"}`,
	}
	qSnap := qualitySnapshot{
		// 49 samples: below public 50-sample threshold -> unrated
		{requested: "m-49", routeObservationKey: routeObservationKey{1, "m-49", 1, true}}: {
			ChannelID: 1, Model: "m-49", ActualModel: "m-49", Samples: 49, Band: 1, UpdatedAt: nowMilli - 1000,
		},
		// Exactly 50 samples, fresh -> no_downgrade
		{requested: "m-50-clean", routeObservationKey: routeObservationKey{1, "m-50-clean", 1, true}}: {
			ChannelID: 1, Model: "m-50-clean", ActualModel: "m-50-clean", Samples: 50, Band: 0, UpdatedAt: nowMilli - 1000,
		},
		// Two 30-sample buckets across stream/non-stream cannot be merged into 60 -> unrated
		{requested: "m-20-latency-only", routeObservationKey: routeObservationKey{1, "m-20-latency-only", 1, true}}: {
			ChannelID: 1, Model: "m-20-latency-only", ActualModel: "m-20-latency-only", Samples: 30, LatencySamples: 25, Band: 1, UpdatedAt: nowMilli - 1000,
		},
		{requested: "m-20-latency-only", routeObservationKey: routeObservationKey{1, "m-20-latency-only", 1, false}}: {
			ChannelID: 1, Model: "m-20-latency-only", ActualModel: "m-20-latency-only", Samples: 30, LatencySamples: 25, Band: 2, UpdatedAt: nowMilli - 1000,
		},
		// Stale (>15m) -> unrated
		{requested: "m-stale", routeObservationKey: routeObservationKey{1, "m-stale", 1, true}}: {
			ChannelID: 1, Model: "m-stale", ActualModel: "m-stale", Samples: 80, Band: 2, UpdatedAt: nowMilli - (15*time.Minute + time.Millisecond).Milliseconds(),
		},
		// Multiple qualified buckets: worst band wins (band 0 vs band 2 -> severely_degraded)
		{requested: "m-worst-stream", routeObservationKey: routeObservationKey{1, "m-worst-stream", 1, true}}: {
			ChannelID: 1, Model: "m-worst-stream", ActualModel: "m-worst-stream", Samples: 60, Band: 0, UpdatedAt: nowMilli - 1000,
		},
		{requested: "m-worst-stream", routeObservationKey: routeObservationKey{1, "m-worst-stream", 1, false}}: {
			ChannelID: 1, Model: "m-worst-stream", ActualModel: "m-worst-stream", Samples: 55, Band: 2, UpdatedAt: nowMilli - 1000,
		},
	}

	breakers := map[routeBreakerKey]int64{
		{1, "m-half-open"}:         nowMilli - 5000, // pause elapsed, still open -> recovering
		{1, "m-paused-and-cooled"}: nowMilli + 30000,
	}
	cooldowns := map[int]int64{
		2: nowMilli + 5000,
		4: nowMilli + 5000,
	}
	// Priority check: channel 1 has no cooldown here; check paused > limited separately on channel 2.
	breakers[routeBreakerKey{2, "m-cooled-only"}] = 0

	offerHealth, channelHealth := projectCatalogHealth(
		now,
		offers,
		mappings,
		qSnap,
		func(k routeBreakerKey) int64 { return breakers[k] },
		func(id int) int64 { return cooldowns[id] },
		nil,
	)

	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, offerHealth[catalogOfferKey{1, "m-cold"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, offerHealth[catalogOfferKey{1, "m-49"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityNoDowngrade}, offerHealth[catalogOfferKey{1, "m-50-clean"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, offerHealth[catalogOfferKey{1, "m-20-latency-only"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, offerHealth[catalogOfferKey{1, "m-stale"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualitySeverelyDegraded}, offerHealth[catalogOfferKey{1, "m-worst-stream"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityRecovering, CatalogQualityUnrated}, offerHealth[catalogOfferKey{1, "m-half-open"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityPaused, CatalogQualityUnrated}, offerHealth[catalogOfferKey{1, "m-paused-and-cooled"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityLimited, CatalogQualityUnrated}, offerHealth[catalogOfferKey{2, "m-cooled-only"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityUnknown, CatalogQualityUnrated}, offerHealth[catalogOfferKey{3, "m-bad-map"}])
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityLimited, CatalogQualityUnrated}, offerHealth[catalogOfferKey{4, "m-cycle-cooled"}])

	// Channel 1 has 8 offers: 3 affected (worst-stream, half-open, paused-and-cooled), 1 evaluated (m-50-clean), 4 unrated.
	require.Equal(t, CatalogChannelHealth{VisibleModels: 8, AffectedModels: 3, EvaluatedModels: 1, UnratedModels: 4}, channelHealth[1])
	require.Equal(t, CatalogChannelHealth{VisibleModels: 1, AffectedModels: 1, EvaluatedModels: 0, UnratedModels: 0}, channelHealth[2])
	require.Equal(t, CatalogChannelHealth{VisibleModels: 1, AffectedModels: 0, EvaluatedModels: 0, UnratedModels: 1}, channelHealth[3])
	require.Equal(t, CatalogChannelHealth{VisibleModels: 1, AffectedModels: 1, EvaluatedModels: 0, UnratedModels: 0}, channelHealth[4])

	// Verify priority paused > limited > recovering when a channel is simultaneously cooled and has an open or half-open breaker.
	cooldowns[1] = nowMilli + 5000
	offerHealthWithCooldown, _ := projectCatalogHealth(
		now,
		[]CatalogOffer{{ChannelID: 1, ModelID: "m-paused-and-cooled"}, {ChannelID: 1, ModelID: "m-half-open"}},
		nil,
		qualitySnapshot{},
		func(k routeBreakerKey) int64 { return breakers[k] },
		func(id int) int64 { return cooldowns[id] },
		nil,
	)
	require.Equal(t, CatalogAvailabilityPaused, offerHealthWithCooldown[catalogOfferKey{1, "m-paused-and-cooled"}].Availability, "paused beats limited")
	require.Equal(t, CatalogAvailabilityLimited, offerHealthWithCooldown[catalogOfferKey{1, "m-half-open"}].Availability, "limited beats recovering")
}

func TestCatalogHealthHalfOpenProbeInvariance(t *testing.T) {
	resetCatalogHealthStores(t, 11)
	epoch := routeObservations.epoch
	key := routeBreakerKey{11, "gpt-4o"}
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, key, nil, routeBreakerSample{success: false, status: 503, code: "upstream_error"})
	}
	// Move openUntil into the past to enter half-open state.
	past := time.Now().Add(-2 * time.Second).UnixMilli()
	routeObservations.mu.Lock()
	st := routeObservations.breakers[key]
	st.openUntil = past
	routeObservations.breakers[key] = st
	routeObservations.breakerOpenUntil.Store(key, past)
	routeObservations.mu.Unlock()

	offers := []CatalogOffer{{ChannelID: 11, ModelID: "gpt-4o"}}
	beforeProbe, _ := projectCatalogHealth(time.Now(), offers, nil, qualitySnapshot{}, routeObservations.catalogBreakerOpenUntil, routeObservations.catalogCooldownUntil, nil)

	probe, avail, _ := routeObservations.claimProbe(key, epoch, false, false)
	require.NotNil(t, probe)
	require.Equal(t, routeAvailable, avail)
	defer routeObservations.releaseProbe(probe)

	duringProbe, _ := projectCatalogHealth(time.Now(), offers, nil, qualitySnapshot{}, routeObservations.catalogBreakerOpenUntil, routeObservations.catalogCooldownUntil, nil)
	require.Equal(t, CatalogAvailabilityRecovering, beforeProbe[catalogOfferKey{11, "gpt-4o"}].Availability)
	require.Equal(t, beforeProbe, duringProbe, "probe occupancy must not change user-visible recovering state")
}

func TestCatalogHealthReadDoesNotTakeRelayLocks(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "lockfree-owner", AffCode: "LF17"}).Error)
	createRouteChannel(t, db, 11, "gpt-4o")
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	epoch := routeObservations.epoch
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, routeBreakerKey{11, "gpt-4o"}, nil, routeBreakerSample{success: false, status: 500, code: "upstream_error"})
	}

	// Deliberately hold all three relay mutexes; full BuildModelCatalog must complete immediately without blocking.
	routeObservations.mu.Lock()
	routeQualities.mu.Lock()
	gatewayConcurrency.mu.Lock()
	type buildRes struct {
		view ModelCatalog
		err  error
	}
	done := make(chan buildRes, 1)
	go func() {
		view, err := BuildModelCatalog(context.Background(), 17, 0)
		done <- buildRes{view, err}
	}()
	select {
	case res := <-done:
		gatewayConcurrency.mu.Unlock()
		routeQualities.mu.Unlock()
		routeObservations.mu.Unlock()
		require.NoError(t, res.err)
		require.Len(t, res.view.Offers, 1)
		require.Equal(t, CatalogAvailabilityPaused, res.view.Offers[0].Health.Availability)
	case <-time.After(2 * time.Second):
		gatewayConcurrency.mu.Unlock()
		routeQualities.mu.Unlock()
		routeObservations.mu.Unlock()
		t.Fatal("BuildModelCatalog blocked on relay locks")
	}
}

func TestCatalogHealthUsesOnlyAuthorizedOffers(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11, 22, 33, 44)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "owner", AffCode: "OWN17"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 18, Username: "other", AffCode: "OTH18"}).Error)

	// 11: authorized; 22: exceeds key max ratio; 33: disabled channel with STALE enabled ability row; 44: excluded by manual route.
	createRouteChannel(t, db, 11, "pub-model")
	createRouteChannel(t, db, 22, "pub-model,expensive-only")
	createRouteChannel(t, db, 33, "stale-disabled-model")
	createRouteChannel(t, db, 44, "other-channel-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("channel_ratio", "3").Error)
	// Simulate stale enabled ability on a disabled channel (direct status update without disabling abilities).
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 33).Update("status", common.ChannelStatusManuallyDisabled).Error)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	// Open breaker on disabled channel 33 and unauthorized channel 22.
	epoch := routeObservations.epoch
	for _, id := range []int{22, 33, 44} {
		routeObservations.coolDown(epoch, id, routeCooldown{until: time.Now().Add(time.Minute).UnixMilli(), source: "retry_after"})
	}

	// Unfiltered catalog (tokenID=0): channel 33 must NEVER appear despite its stale enabled ability.
	unfiltered, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	for _, ch := range unfiltered.Channels {
		require.NotEqual(t, 33, ch.ID, "disabled channel with stale ability must be excluded")
	}
	unfilteredBytes, err := common.Marshal(unfiltered)
	require.NoError(t, err)
	require.NotContains(t, string(unfilteredBytes), "stale-disabled-model")

	// Filtered by key 7 (selected_only=[11], max_channel_ratio=1.5): only channel 11 may appear in Channels & Offers.
	maxRatio := "1.5"
	_, err = model.SaveTokenRouting(context.Background(), 17, 7, model.TokenRouting{
		TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteAuto, SelectedOnly: true, MaxChannelRatio: &maxRatio},
		Entries:           []model.TokenRouteEntry{{ChannelID: 11, Enabled: true}},
	}, map[int]bool{11: true})
	require.NoError(t, err)

	scoped, err := BuildModelCatalog(context.Background(), 17, 7)
	require.NoError(t, err)
	require.Len(t, scoped.Channels, 1)
	require.Equal(t, 11, scoped.Channels[0].ID)
	require.NotNil(t, scoped.Channels[0].Health)
	require.Equal(t, CatalogChannelHealth{VisibleModels: 1, AffectedModels: 0, EvaluatedModels: 0, UnratedModels: 1}, *scoped.Channels[0].Health)
	scopedBytes, err := common.Marshal(scoped)
	require.NoError(t, err)
	require.NotContains(t, string(scopedBytes), "expensive-only")
	require.NotContains(t, string(scopedBytes), "other-channel-model")
	require.NotContains(t, string(scopedBytes), "stale-disabled-model")

	// Another user's key must be rejected with no catalog payload.
	_, err = BuildModelCatalog(context.Background(), 18, 7)
	require.Error(t, err)
}

func TestCatalogHealthConcurrentInvalidation(t *testing.T) {
	resetCatalogHealthStores(t, 11, 22)
	const workers = 6
	const iterations = 120

	var wg sync.WaitGroup
	// Concurrent writers: breaker open/reopen/close, cooldown extend, quality add/evaluate, and channel invalidation.
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			routeObservations.mu.Lock()
			epoch := routeObservations.epoch
			routeObservations.mu.Unlock()
			key := routeBreakerKey{11, "gpt-4o"}
			for range routeBreakerOpenFailures {
				routeObservations.recordBreaker(epoch, key, nil, routeBreakerSample{success: false, status: 502, code: "upstream_error"})
			}
			routeObservations.coolDown(epoch, 11, routeCooldown{until: time.Now().Add(time.Minute).UnixMilli(), source: "headerless"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			now := time.Now()
			routeQualities.add(routeQualityKey{"gpt-4o", routeObservationKey{11, "gpt-4o", relayconstant.RelayModeChatCompletions, true}}, true, 150, 80, time.Second, now)
			if i%20 == 0 {
				routeQualities.evaluate(now)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/3; i++ {
			routeObservations.invalidate(11)
		}
	}()

	// Concurrent lock-free catalog health readers.
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			offers := []CatalogOffer{{ChannelID: 11, ModelID: "gpt-4o"}, {ChannelID: 22, ModelID: "gpt-4o"}}
			for i := 0; i < iterations; i++ {
				oh, ch := projectCatalogHealth(time.Now(), offers, nil, routeQualities.snapshot(), routeObservations.catalogBreakerOpenUntil, routeObservations.catalogCooldownUntil, routeObservations.catalogChannelPending)
				require.Len(t, oh, 2)
				require.Equal(t, 1, ch[11].VisibleModels)
				require.Equal(t, ch[11].VisibleModels, ch[11].AffectedModels+ch[11].EvaluatedModels+ch[11].UnratedModels)
			}
		}()
	}
	wg.Wait()

	// Final invalidation must leave read indexes completely clean for channel 11.
	routeObservations.invalidate(11)
	require.Zero(t, routeObservations.catalogBreakerOpenUntil(routeBreakerKey{11, "gpt-4o"}))
	require.Zero(t, routeObservations.catalogCooldownUntil(11))
	require.Equal(t, uint64(0), routeObservations.catalogConfigVersion()%2, "configVersion must settle even after invalidation")
}

func TestBuildModelCatalogRejectsStaleHealthDuringPendingAndFailedPublish(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11, 22)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "pending-owner", AffCode: "PND17"}).Error)
	createRouteChannel(t, db, 11, "gpt-4o")
	createRouteChannel(t, db, 22, "gpt-4o")
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	// Seed >=50 clean samples on channel 11 + reference channel 22 -> no_downgrade.
	now := time.Now()
	for range catalogQualityMinSamples {
		routeQualities.add(routeQualityKey{"gpt-4o", routeObservationKey{11, "gpt-4o", relayconstant.RelayModeChatCompletions, true}}, true, 100, 80, time.Second, now)
		routeQualities.add(routeQualityKey{"gpt-4o", routeObservationKey{22, "gpt-4o", relayconstant.RelayModeChatCompletions, true}}, true, 100, 80, time.Second, now)
	}
	routeQualities.evaluate(now)

	before, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	findOffer := func(cat ModelCatalog, chID int) CatalogOffer {
		for _, o := range cat.Offers {
			if o.ChannelID == chID && o.ModelID == "gpt-4o" {
				return o
			}
		}
		t.Fatalf("offer for channel %d not found", chID)
		return CatalogOffer{}
	}
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityNoDowngrade}, findOffer(before, 11).Health)

	// Hold concurrencyPublishMu so UpdateChannelLocked commits the new credential to DB
	// but blocks BEFORE refreshConcurrencyLimitLocked can call routeObservations.invalidate(11).
	concurrencyPublishMu.Lock()
	updateDone := make(chan error, 1)
	go func() {
		_, err := model.UpdateChannelLocked(11, func(ch *model.Channel) error {
			ch.Key = "sk-rotated-credential"
			return nil
		})
		updateDone <- err
	}()

	// Wait until the DB update committed and marked channel 11 pending.
	require.Eventually(t, func() bool {
		var ch model.Channel
		if err := db.First(&ch, 11).Error; err != nil {
			return false
		}
		return ch.Key == "sk-rotated-credential" && routeObservations.catalogChannelPending(11)
	}, 2*time.Second, 5*time.Millisecond)

	// While publish is blocked in the post-commit window, BuildModelCatalog must NEVER
	// return stale pre-rotation no_downgrade health for channel 11 (while channel 22 stays intact).
	duringPending, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityUnknown, CatalogQualityUnrated}, findOffer(duringPending, 11).Health)
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityNoDowngrade}, findOffer(duringPending, 22).Health)

	// Release publish lock -> invalidation completes and pending clears -> channel 11 starts fresh unrated.
	concurrencyPublishMu.Unlock()
	require.NoError(t, <-updateDone)
	require.False(t, routeObservations.catalogChannelPending(11))

	afterPublish, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, findOffer(afterPublish, 11).Health)

	// Real failed publish path: corrupt max_concurrency to -1 in DB so RefreshChannelConcurrencyLimit
	// genuinely fails inside refreshConcurrencyLimitLocked before clearing pending or invalidating.
	for range catalogQualityMinSamples {
		routeQualities.add(routeQualityKey{"gpt-4o", routeObservationKey{11, "gpt-4o", relayconstant.RelayModeChatCompletions, true}}, true, 100, 80, time.Second, time.Now())
	}
	routeQualities.evaluate(time.Now())
	require.NoError(t, db.Exec("UPDATE channels SET max_concurrency = -1 WHERE id = 11").Error)
	t.Cleanup(func() {
		_ = db.Exec("UPDATE channels SET max_concurrency = 0 WHERE id = 11").Error
		_ = RefreshChannelConcurrencyLimit(context.Background(), 11)
	})
	require.Error(t, RefreshChannelConcurrencyLimit(context.Background(), 11))
	require.True(t, routeObservations.catalogChannelPending(11))

	// Pending publication after real DB publish failure suppresses stale health until publish succeeds.
	whileFailed, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityUnknown, CatalogQualityUnrated}, findOffer(whileFailed, 11).Health)

	// Repair DB and run real publish -> pending clears and channel 11 returns fresh unrated state.
	require.NoError(t, db.Exec("UPDATE channels SET max_concurrency = 0 WHERE id = 11").Error)
	require.NoError(t, RefreshChannelConcurrencyLimit(context.Background(), 11))
	require.False(t, routeObservations.catalogChannelPending(11))
	recovered, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, findOffer(recovered, 11).Health)
}

func TestBuildModelCatalogBatchPublishFirstErrorStillProtectsRemainingChannels(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11, 22, 33)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "batch-owner", AffCode: "BCH17"}).Error)
	createRouteChannel(t, db, 11, "gpt-4o")
	createRouteChannel(t, db, 22, "gpt-4o")
	createRouteChannel(t, db, 33, "gpt-4o")
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	now := time.Now()
	for range catalogQualityMinSamples {
		for _, id := range []int{11, 22, 33} {
			routeQualities.add(routeQualityKey{"gpt-4o", routeObservationKey{id, "gpt-4o", relayconstant.RelayModeChatCompletions, true}}, true, 100, 80, time.Second, now)
		}
	}
	routeQualities.evaluate(now)

	// Force channel 11's post-commit publish to fail while channel 22 succeeds.
	require.NoError(t, db.Exec("UPDATE channels SET max_concurrency = -1 WHERE id = 11").Error)
	t.Cleanup(func() {
		_ = db.Exec("UPDATE channels SET max_concurrency = 0 WHERE id = 11").Error
		_ = RefreshChannelConcurrencyLimit(context.Background(), 11)
	})

	tag := "batch-retagged"
	err := model.BatchSetChannelTag([]int{11, 22}, &tag)
	require.Error(t, err, "channel 11 publish failure surfaces")

	view, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	byCh := map[int]CatalogOfferHealth{}
	for _, o := range view.Offers {
		if o.ModelID == "gpt-4o" {
			byCh[o.ChannelID] = o.Health
		}
	}
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityUnknown, CatalogQualityUnrated}, byCh[11], "failed first channel stays pending/unknown")
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityUnrated}, byCh[22], "second channel in batch was still invalidated and never inherits stale no_downgrade")
	require.Equal(t, CatalogOfferHealth{CatalogAvailabilityNoRestriction, CatalogQualityNoDowngrade}, byCh[33], "unrelated channel retains its quality")
}

func TestDeleteChannelsInvalidatesCatalogHealthImmediately(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11, 22)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "del-owner", AffCode: "DEL17"}).Error)
	createRouteChannel(t, db, 11, "gpt-4o")
	createRouteChannel(t, db, 22, "gpt-4o")
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	epoch := routeObservations.epoch
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, routeBreakerKey{11, "gpt-4o"}, nil, routeBreakerSample{success: false, status: 502, code: "upstream_error"})
	}
	require.NotZero(t, routeObservations.catalogBreakerOpenUntil(routeBreakerKey{11, "gpt-4o"}))

	deleted, err := model.DeleteChannels([]int{11})
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	require.Zero(t, routeObservations.catalogBreakerOpenUntil(routeBreakerKey{11, "gpt-4o"}), "DeleteChannels must immediately invalidate read index before controller cleanup")

	view, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Len(t, view.Channels, 1)
	require.Equal(t, 22, view.Channels[0].ID)
}

func TestBuildModelCatalogFailsClosedOnSustainedInvalidation(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "seqlock-owner", AffCode: "SQ17"}).Error)
	createRouteChannel(t, db, 11, "gpt-4o")
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
	t.Cleanup(func() { catalogAfterBuildOnce = nil })

	// 1) Odd configVersion (active invalidation) across all retries -> ErrCatalogConfigChanging.
	routeObservations.configVersion.Store(1)
	_, err := BuildModelCatalog(context.Background(), 17, 0)
	require.ErrorIs(t, err, ErrCatalogConfigChanging)

	// 2) Single mid-build invalidation -> BuildModelCatalog retries and succeeds on attempt 2.
	routeObservations.configVersion.Store(2)
	calls := 0
	catalogAfterBuildOnce = func() {
		calls++
		if calls == 1 {
			routeObservations.invalidate(11)
		}
	}
	view, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Len(t, view.Offers, 1)

	// 3) Every single build attempt interrupted by mid-build invalidation -> fails closed after catalogBuildMaxAttempts.
	calls = 0
	catalogAfterBuildOnce = func() {
		calls++
		routeObservations.invalidate(11)
	}
	_, err = BuildModelCatalog(context.Background(), 17, 0)
	require.ErrorIs(t, err, ErrCatalogConfigChanging)
	require.Equal(t, catalogBuildMaxAttempts, calls)
}

func TestCatalogHealthIndexCleanupOnCloseExpireAndEvict(t *testing.T) {
	db := setupCatalogRouteTest(t)
	resetCatalogHealthStores(t, 11, 22, 33, 44)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "cleanup-owner", AffCode: "CLN17"}).Error)
	createRouteChannel(t, db, 11, "gpt-4o")
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	prevCap := routeObservationCapacity
	routeObservationCapacity = 2
	t.Cleanup(func() { routeObservationCapacity = prevCap })

	epoch := routeObservations.epoch
	key := routeBreakerKey{11, "gpt-4o"}

	// 1) Open breaker -> BuildModelCatalog sees paused -> half-open -> 3 successful probes -> deleted from index & BuildModelCatalog sees no_restriction.
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, key, nil, routeBreakerSample{success: false, status: 502, code: "upstream_error"})
	}
	require.NotZero(t, routeObservations.catalogBreakerOpenUntil(key))
	openedView, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, CatalogAvailabilityPaused, openedView.Offers[0].Health.Availability)

	past := time.Now().Add(-time.Second).UnixMilli()
	routeObservations.mu.Lock()
	st := routeObservations.breakers[key]
	st.openUntil = past
	routeObservations.breakers[key] = st
	routeObservations.breakerOpenUntil.Store(key, past)
	routeObservations.mu.Unlock()

	for range routeBreakerCloseSuccesses {
		probe, avail, _ := routeObservations.claimProbe(key, epoch, false, false)
		require.NotNil(t, probe)
		require.Equal(t, routeAvailable, avail)
		routeObservations.recordBreaker(epoch, key, probe, routeBreakerSample{success: true})
	}
	require.Zero(t, routeObservations.catalogBreakerOpenUntil(key), "closing breaker via 3 probes must remove read index entry")
	closedView, err := BuildModelCatalog(context.Background(), 17, 0)
	require.NoError(t, err)
	require.Equal(t, CatalogAvailabilityNoRestriction, closedView.Offers[0].Health.Availability)

	// 2) Capacity eviction in makeBreakerRoomLocked: active open breaker survives while oldest closed key is evicted.
	keyOpen := routeBreakerKey{22, "m-open"}
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, keyOpen, nil, routeBreakerSample{success: false, status: 502, code: "upstream_error"})
	}
	// Table now at capacity 2 (key closed + keyOpen open); inserting a 3rd key triggers makeBreakerRoomLocked.
	keyThird := routeBreakerKey{33, "m-third"}
	for range routeBreakerOpenFailures {
		routeObservations.recordBreaker(epoch, keyThird, nil, routeBreakerSample{success: false, status: 502, code: "upstream_error"})
	}
	require.Zero(t, routeObservations.catalogBreakerOpenUntil(key))
	require.NotZero(t, routeObservations.catalogBreakerOpenUntil(keyOpen), "open breaker must survive closed-entry eviction")
	require.NotZero(t, routeObservations.catalogBreakerOpenUntil(keyThird))

	// 3) Cooldown expiry + capacity purge removes stale entries from cooldownUntil index.
	expiredAt := time.Now().Add(-time.Second).UnixMilli()
	futureAt := time.Now().Add(time.Minute).UnixMilli()
	routeObservations.coolDown(epoch, 22, routeCooldown{until: expiredAt, source: "headerless"})
	routeObservations.coolDown(epoch, 33, routeCooldown{until: futureAt, source: "retry_after"})
	routeObservations.coolDown(epoch, 44, routeCooldown{until: futureAt, source: "retry_after"})

	require.Zero(t, routeObservations.catalogCooldownUntil(22), "expired cooldown evicted on capacity check must leave read index")
	require.Equal(t, futureAt, routeObservations.catalogCooldownUntil(33))
	require.Equal(t, futureAt, routeObservations.catalogCooldownUntil(44))

	// Explicit pruneLocked removes expired cooldowns from both maps.
	routeObservations.mu.Lock()
	routeObservations.lastPrune = 0
	routeObservations.pruneLocked(futureAt + 1000)
	routeObservations.mu.Unlock()
	require.Zero(t, routeObservations.catalogCooldownUntil(33))
	require.Zero(t, routeObservations.catalogCooldownUntil(44))
}
