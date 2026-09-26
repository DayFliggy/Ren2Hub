// Package autopricing converts external catalogs into explicit billing drafts.
// It has no network, mutable global catalog, or model-family price defaults.
package autopricing

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
)

type SourceID string

const (
	SourceSub2API SourceID = "sub2api"
	SourceAxonHub SourceID = "axonhub"
)

type Entry struct {
	Price       billing.PriceVersion `json:"price"`
	Source      SourceID             `json:"source"`
	SourceModel string               `json:"source_model"`
}

// BlockFallback distinguishes missing data from an unsafe or ambiguous identity
// whose aliases must not be reused within this source. A different source may
// still provide a complete price; fields are never merged between sources.
type Rejection struct {
	Model         string `json:"model"`
	Reason        string `json:"reason"`
	BlockFallback bool   `json:"block_fallback"`
}

func Parse(source SourceID, data []byte) (map[string]Entry, error) {
	entries, _, err := ParseWithReport(source, data)
	return entries, err
}

func ParseWithReport(source SourceID, data []byte) (map[string]Entry, []Rejection, error) {
	var adapter func(rawObject) ([]catalogRecord, error)
	switch source {
	case SourceSub2API:
		adapter = sub2APICatalog
	case SourceAxonHub:
		adapter = axonHubCatalog
	default:
		return nil, nil, fmt.Errorf("unsupported pricing source %q", source)
	}
	var root rawObject
	if err := common.Unmarshal(data, &root); err != nil || root == nil {
		return nil, nil, fmt.Errorf("invalid %s pricing catalog object", source)
	}
	records, err := adapter(root)
	if err != nil {
		return nil, nil, err
	}
	entries := make(map[string]Entry)
	// Track identities before converting prices. An unpriced second provider
	// still makes a bare model ambiguous, even if the first provider is usable.
	aliases := make(map[string]map[string]bool)
	rejections := make(map[string]Rejection)
	explicitKeys := make(map[string]bool)
	for _, record := range records {
		explicitKeys[record.key] = true
		if record.alias != "" && record.alias != record.key {
			if aliases[record.alias] == nil {
				aliases[record.alias] = make(map[string]bool)
			}
			aliases[record.alias][record.key] = true
		}
	}
	for _, record := range records {
		err := record.err
		if err == nil {
			var version billing.PriceVersion
			version, err = record.plan.version(record.key)
			if err == nil {
				entry := Entry{Price: version, Source: source, SourceModel: record.model}
				if previous, exists := entries[record.key]; exists && !samePrice(previous, entry) {
					err = unsafePrice("conflicting duplicate model")
				} else {
					entries[record.key] = entry
				}
			}
		}
		if err == nil {
			continue
		}
		block := true
		var reason priceError
		if errors.As(err, &reason) {
			block = reason.block
		}
		if previous, exists := rejections[record.key]; !exists || (!previous.BlockFallback && block) {
			rejections[record.key] = Rejection{Model: record.key, Reason: err.Error(), BlockFallback: block}
		}
	}
	for key := range rejections {
		delete(entries, key)
	}
	for alias, identities := range aliases {
		if explicitKeys[alias] {
			continue
		}
		if len(identities) != 1 {
			rejections[alias] = Rejection{Model: alias, Reason: "ambiguous provider identity", BlockFallback: true}
			continue
		}
		for key := range identities {
			if rejection, rejected := rejections[key]; rejected {
				rejection.Model = alias
				rejections[alias] = rejection
				continue
			}
			chosen := cloneEntry(entries[key])
			chosen.Price.Model = alias
			entries[alias] = chosen
		}
	}
	rejected := make([]Rejection, 0, len(rejections))
	for _, rejection := range rejections {
		rejected = append(rejected, rejection)
	}
	sort.Slice(rejected, func(i, j int) bool {
		if rejected[i].Model == rejected[j].Model {
			return rejected[i].Reason < rejected[j].Reason
		}
		return rejected[i].Model < rejected[j].Model
	})
	if len(entries) == 0 {
		return entries, rejected, fmt.Errorf("%s catalog contains no usable prices", source)
	}
	return entries, rejected, nil
}

func cloneEntry(entry Entry) Entry {
	// Keep PreparePriceVersion's empty-array representation stable. A nil
	// slice would change the draft hash and defeat unchanged-price detection.
	entry.Price.Prices = append(make([]billing.UnitPrice, 0, len(entry.Price.Prices)), entry.Price.Prices...)
	return entry
}

func samePrice(a, b Entry) bool {
	a.Price.Model, b.Price.Model = "", ""
	x, errA := common.Marshal(a.Price)
	y, errB := common.Marshal(b.Price)
	return errA == nil && errB == nil && string(x) == string(y)
}

type priceError struct {
	reason string
	block  bool
}

func (err priceError) Error() string   { return err.reason }
func unsafePrice(reason string) error  { return priceError{reason: reason, block: true} }
func missingPrice(reason string) error { return priceError{reason: reason, block: false} }

type rawObject map[string]common.RawMessage
type catalogRecord struct {
	key, model, alias string
	plan              pricePlan
	err               error
}

func object(data common.RawMessage) (rawObject, error) {
	var value rawObject
	if err := common.Unmarshal(data, &value); err != nil || value == nil {
		return nil, unsafePrice("invalid pricing object")
	}
	return value, nil
}

func rawString(data common.RawMessage) string {
	var value string
	if common.Unmarshal(data, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func keys(object rawObject) []string {
	result := make([]string, 0, len(object))
	for key := range object {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
