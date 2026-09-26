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
	SourceSub2API    SourceID = "sub2api"
	SourceAxonHub    SourceID = "axonhub"
	SourceModelsDev  SourceID = "models_dev"
	SourceOpenRouter SourceID = "openrouter"
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
	var root rawObject
	if err := common.Unmarshal(data, &root); err != nil || root == nil {
		return nil, nil, fmt.Errorf("invalid %s pricing catalog object", source)
	}
	records, err := sourceRecords(source, root)
	if err != nil {
		return nil, nil, err
	}
	entries := make(map[string]Entry)
	var rejected []Rejection
	aliases := make(map[string][]Entry)
	blocked := make(map[string]bool)
	explicitKeys := make(map[string]bool)
	for _, record := range records {
		explicitKeys[record.key] = true
	}
	for _, record := range records {
		plan, err := pricePlan{}, record.err
		if err == nil {
			plan, err = parseRecord(source, record.data)
		}
		if err == nil {
			var version billing.PriceVersion
			version, err = plan.version(record.key)
			if err == nil {
				entry := Entry{Price: version, Source: source, SourceModel: record.model}
				if previous, exists := entries[record.key]; exists && !samePrice(previous, entry) {
					err = unsafePrice("conflicting duplicate model")
				} else {
					entries[record.key] = entry
					if record.alias != "" && record.alias != record.key {
						aliases[record.alias] = append(aliases[record.alias], entry)
					}
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
		for _, key := range []string{record.key, record.alias} {
			if key == "" {
				continue
			}
			if key != record.key && explicitKeys[key] {
				continue
			}
			rejected = append(rejected, Rejection{Model: key, Reason: err.Error(), BlockFallback: block})
			if block {
				blocked[key] = true
				delete(entries, key)
			}
			if key == record.alias && key == record.key {
				break
			}
		}
	}
	for alias, candidates := range aliases {
		if explicitKeys[alias] {
			continue
		}
		if blocked[alias] {
			continue
		}
		chosen := candidates[0]
		for _, candidate := range candidates {
			if !samePrice(chosen, candidate) {
				blocked[alias] = true
				break
			}
		}
		if blocked[alias] {
			delete(entries, alias)
			rejected = append(rejected, Rejection{Model: alias, Reason: "ambiguous provider prices", BlockFallback: true})
			continue
		}
		chosen = cloneEntry(chosen)
		chosen.Price.Model = alias
		entries[alias] = chosen
	}
	for key := range blocked {
		delete(entries, key)
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

// Merge gives the first catalog precedence for an entire model. It never fills
// missing dimensions from another source, because price schedules are atomic.
func Merge(catalogs ...map[string]Entry) map[string]Entry {
	result := make(map[string]Entry)
	for _, catalog := range catalogs {
		for model, entry := range catalog {
			if _, exists := result[model]; !exists {
				result[model] = cloneEntry(entry)
			}
		}
	}
	return result
}

func cloneEntry(entry Entry) Entry {
	entry.Price.Prices = append([]billing.UnitPrice(nil), entry.Price.Prices...)
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
type sourceRecord struct {
	key, model, alias string
	data              rawObject
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
