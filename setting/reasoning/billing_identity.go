package reasoning

import (
	"fmt"
	"slices"
	"strings"

	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

// BillingModelCandidates is shared by publication and lookup. Request intent
// selects the catalog identity; final channel parameters are quote attributes.
func BillingModelCandidates(origin string) []string {
	if model_setting.ShouldPreserveThinkingSuffix(origin) {
		return []string{origin}
	}
	var names []string
	if !kitreasoning.ParseModelModifiers(origin).HasModifiers() {
		names = append(names, origin)
	}
	names = append(names, CanonicalBillingModelNames(origin)...)
	names = append(names, BaseModelName(origin))
	result := make([]string, 0, len(names))
	for _, name := range names {
		if name != "" && !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	return result
}

func ValidateBillingModelName(name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return fmt.Errorf("invalid billing model name")
	}
	if !slices.Contains(BillingModelCandidates(name), name) {
		return fmt.Errorf("billing model must use a canonical price name")
	}
	return nil
}
