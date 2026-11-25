package service

import (
	"errors"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// CatalogAvailability describes coarse user-visible routing state for one
// authorized (channel, requested model) offer. It never reveals in-flight load,
// probe occupancy, upstream error text/codes, or deadline timestamps.
type CatalogAvailability string

const (
	CatalogAvailabilityUnknown       CatalogAvailability = "unknown"
	CatalogAvailabilityNoRestriction CatalogAvailability = "no_restriction"
	CatalogAvailabilityLimited       CatalogAvailability = "limited"
	CatalogAvailabilityPaused        CatalogAvailability = "paused"
	CatalogAvailabilityRecovering    CatalogAvailability = "recovering"
)

// CatalogQuality describes relative recent quality across qualified buckets
// (exact channel + requested model + actual mapped model, >=50 samples, <=15m).
type CatalogQuality string

const (
	CatalogQualityUnrated          CatalogQuality = "unrated"
	CatalogQualityNoDowngrade      CatalogQuality = "no_downgrade"
	CatalogQualityDegraded         CatalogQuality = "degraded"
	CatalogQualitySeverelyDegraded CatalogQuality = "severely_degraded"
)

type CatalogOfferHealth struct {
	Availability CatalogAvailability `json:"availability"`
	Quality      CatalogQuality      `json:"quality"`
}

// CatalogChannelHealth summarizes mutually exclusive counts over the final
// visible offers emitted for a channel in this catalog response.
type CatalogChannelHealth struct {
	VisibleModels   int `json:"visible_models"`
	AffectedModels  int `json:"affected_models"`
	EvaluatedModels int `json:"evaluated_models"`
	UnratedModels   int `json:"unrated_models"`
}

const (
	catalogQualityMinSamples = 50
	catalogQualityMaxAge     = 15 * time.Minute
	catalogBuildMaxAttempts  = 5
)

// ErrCatalogConfigChanging is returned if channel invalidations race with every
// retry of BuildModelCatalog so stale or revoked offers are never served.
var ErrCatalogConfigChanging = errors.New("model catalog configuration is updating; please retry")

type catalogOfferKey struct {
	channelID int
	modelID   string
}

type catalogQualityLookupKey struct {
	channelID int
	requested string
	actual    string
}

func catalogQualityFromBand(band int) CatalogQuality {
	switch {
	case band >= 2:
		return CatalogQualitySeverelyDegraded
	case band == 1:
		return CatalogQualityDegraded
	default:
		return CatalogQualityNoDowngrade
	}
}

func isCatalogOfferAffected(health CatalogOfferHealth) bool {
	switch health.Availability {
	case CatalogAvailabilityPaused, CatalogAvailabilityLimited, CatalogAvailabilityRecovering:
		return true
	}
	return health.Quality == CatalogQualityDegraded || health.Quality == CatalogQualitySeverelyDegraded
}

func isCatalogOfferEvaluatedClean(health CatalogOfferHealth) bool {
	return health.Availability == CatalogAvailabilityNoRestriction && health.Quality == CatalogQualityNoDowngrade
}

// projectCatalogHealth computes sanitized offer-level and channel-level health
// projections from immutable quality snapshots and lock-free availability lookups.
// It never mutates inputs, acquires relay locks, or leaks mapped target model names.
func projectCatalogHealth(
	now time.Time,
	offers []CatalogOffer,
	mappings map[int]string,
	qualitySnap qualitySnapshot,
	breakerOpenUntil func(routeBreakerKey) int64,
	cooldownUntil func(int) int64,
	channelPending func(int) bool,
) (map[catalogOfferKey]CatalogOfferHealth, map[int]CatalogChannelHealth) {
	nowMilli := now.UnixMilli()
	maxAgeMilli := catalogQualityMaxAge.Milliseconds()

	type resolvedOffer struct {
		actual  string
		ok      bool
		pending bool
	}
	resolved := make(map[catalogOfferKey]resolvedOffer, len(offers))
	neededQuality := make(map[catalogQualityLookupKey]struct{}, len(offers))
	for _, offer := range offers {
		key := catalogOfferKey{channelID: offer.ChannelID, modelID: offer.ModelID}
		if _, seen := resolved[key]; seen {
			continue
		}
		if channelPending != nil && channelPending(offer.ChannelID) {
			resolved[key] = resolvedOffer{pending: true}
			continue
		}
		actual, _, err := relaycommon.ResolveMappedModelName(offer.ModelID, mappings[offer.ChannelID])
		if err != nil || actual == "" {
			resolved[key] = resolvedOffer{ok: false}
			continue
		}
		resolved[key] = resolvedOffer{actual: actual, ok: true}
		neededQuality[catalogQualityLookupKey{
			channelID: offer.ChannelID,
			requested: offer.ModelID,
			actual:    actual,
		}] = struct{}{}
	}

	worstBand := make(map[catalogQualityLookupKey]int, len(neededQuality))
	for qKey, row := range qualitySnap {
		lookup := catalogQualityLookupKey{
			channelID: qKey.id,
			requested: qKey.requested,
			actual:    qKey.model,
		}
		if _, needed := neededQuality[lookup]; !needed {
			continue
		}
		if row.Samples < catalogQualityMinSamples || row.UpdatedAt <= 0 || nowMilli-row.UpdatedAt > maxAgeMilli {
			continue
		}
		if prev, exists := worstBand[lookup]; !exists || row.Band > prev {
			worstBand[lookup] = row.Band
		}
	}

	offerHealth := make(map[catalogOfferKey]CatalogOfferHealth, len(resolved))
	channelHealth := make(map[int]CatalogChannelHealth)
	for key, res := range resolved {
		health := CatalogOfferHealth{
			Availability: CatalogAvailabilityUnknown,
			Quality:      CatalogQualityUnrated,
		}
		if !res.pending {
			cooled := cooldownUntil(key.channelID) > nowMilli
			if !res.ok {
				if cooled {
					health.Availability = CatalogAvailabilityLimited
				}
			} else {
				openUntil := breakerOpenUntil(routeBreakerKey{id: key.channelID, model: res.actual})
				switch {
				case openUntil > nowMilli:
					health.Availability = CatalogAvailabilityPaused
				case cooled:
					health.Availability = CatalogAvailabilityLimited
				case openUntil > 0:
					health.Availability = CatalogAvailabilityRecovering
				default:
					health.Availability = CatalogAvailabilityNoRestriction
				}
				lookup := catalogQualityLookupKey{
					channelID: key.channelID,
					requested: key.modelID,
					actual:    res.actual,
				}
				if band, ok := worstBand[lookup]; ok {
					health.Quality = catalogQualityFromBand(band)
				}
			}
		}
		offerHealth[key] = health

		summary := channelHealth[key.channelID]
		summary.VisibleModels++
		switch {
		case isCatalogOfferAffected(health):
			summary.AffectedModels++
		case isCatalogOfferEvaluatedClean(health):
			summary.EvaluatedModels++
		default:
			summary.UnratedModels++
		}
		channelHealth[key.channelID] = summary
	}

	return offerHealth, channelHealth
}
