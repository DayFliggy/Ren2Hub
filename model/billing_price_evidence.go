package model

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"gorm.io/gorm"
)

// Load once per source in a publication batch. Recompilation binds the complete
// plan and its field evidence to archived bytes; a valid snapshot pointer alone
// cannot authorize caller-supplied rates or fabricated evidence paths.
type automaticBillingPriceEvidence struct {
	source     string
	hash       string
	snapshotID int64
	entries    map[string]autopricing.Entry
	blocked    map[string]bool
}

// AutomaticBillingPublicationEvidence belongs to one synchronization run.
// Its contents can only be compiled from archived bytes by this package.
// Batches still lock and compare the current source revision independently.
type AutomaticBillingPublicationEvidence struct {
	sources map[string]*automaticBillingPriceEvidence
}

func PrepareAutomaticBillingPublicationEvidence(ctx context.Context, entries []AutomaticBillingPrice) (*AutomaticBillingPublicationEvidence, error) {
	if DB == nil {
		return nil, fmt.Errorf("billing price database unavailable")
	}
	result := &AutomaticBillingPublicationEvidence{sources: make(map[string]*automaticBillingPriceEvidence)}
	for _, entry := range entries {
		if err := validateAutomaticBillingPriceSource(entry.Source, entry.SourceModel, entry.SourceHash); err != nil {
			return nil, err
		}
		if previous := result.sources[entry.Source]; previous != nil {
			if previous.hash != entry.SourceHash {
				return nil, ErrBillingPricingSourceConflict
			}
			continue
		}
		evidence, err := loadAutomaticBillingPriceEvidence(DB.WithContext(ctx), entry.Source, entry.SourceHash)
		if err != nil {
			return nil, err
		}
		result.sources[entry.Source] = evidence
	}
	return result, nil
}

func loadAutomaticBillingPriceEvidence(tx *gorm.DB, source, hash string) (*automaticBillingPriceEvidence, error) {
	var snapshot BillingPricingSnapshot
	if err := tx.Where("source = ? AND hash = ?", source, hash).Take(&snapshot).Error; err != nil {
		return nil, fmt.Errorf("automatic price snapshot: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(snapshot.Document))) != hash {
		return nil, fmt.Errorf("automatic price snapshot integrity mismatch")
	}
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceID(source), []byte(snapshot.Document))
	if err != nil {
		return nil, fmt.Errorf("automatic price snapshot cannot be compiled: %w", err)
	}
	result := &automaticBillingPriceEvidence{source: source, hash: hash, snapshotID: snapshot.ID, entries: entries, blocked: make(map[string]bool)}
	for _, rejection := range rejected {
		result.blocked[rejection.Model] = true
	}
	return result, nil
}

func (evidence *automaticBillingPriceEvidence) verify(version billing.PriceVersion, publication billingPricePublication) error {
	if evidence.source != publication.origin || evidence.hash != publication.sourceHash {
		return ErrBillingPricingSourceConflict
	}
	for _, name := range reasoning.BillingModelCandidates(version.Model) {
		entry, found := evidence.entries[name]
		if !found {
			if autopricing.IsBlocked(evidence.blocked, name) {
				return fmt.Errorf("automatic price identity is rejected by its snapshot")
			}
			entry, found = autopricing.Resolve(evidence.entries, name)
		}
		if !found {
			continue
		}
		if entry.SourceModel != publication.sourceModel {
			return fmt.Errorf("automatic price source model mismatch")
		}
		expected := entry.Price
		expected.Provenance = billing.ClonePriceProvenance(entry.Price.Provenance)
		expected.Model = version.Model
		expected.Origin, expected.SourceModel, expected.SourceHash = publication.origin, publication.sourceModel, publication.sourceHash
		expected.Provenance.SnapshotID = evidence.snapshotID
		actualHash, actualErr := billing.Hash(version)
		expectedHash, expectedErr := billing.Hash(expected)
		if actualErr != nil || expectedErr != nil || actualHash != expectedHash {
			return fmt.Errorf("automatic price plan or evidence differs from its snapshot")
		}
		return nil
	}
	return fmt.Errorf("automatic price identity is absent from its snapshot")
}
