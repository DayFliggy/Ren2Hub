package billingexpr

import (
	"fmt"

	"github.com/expr-lang/expr/vm"
)

// Schemas validate literal usage dependencies. Dynamic u() reads keep the same
// runtime semantics as NewAPI; they are not silently converted to literals.
func CompileWithUsageSchema(expression string, schema *UsageSchema) (*vm.Program, error) {
	program, err := CompileFromCache(expression)
	if err != nil {
		return nil, err
	}
	if schema != nil {
		for key := range UsedUsageKeys(expression) {
			if _, exists := schema.Fields[key]; !exists {
				return nil, fmt.Errorf("undeclared billing usage field %q", key)
			}
		}
	}
	return program, nil
}

func UsedUsageFields(expression string, schema *UsageSchema) (map[string]bool, error) {
	if _, err := CompileWithUsageSchema(expression, schema); err != nil {
		return nil, err
	}
	return UsedUsageKeys(expression), nil
}

// PublicTrace keeps request-derived names out of plaintext financial evidence.
// The complete expression still runs; its dynamic label can only be recovered
// by decrypting the request and evaluating it again.
func PublicTrace(expression string, trace TraceResult) (TraceResult, error) {
	entry, err := compileEntryFromCacheByHash(expression, ExprHashString(expression))
	if err != nil {
		return TraceResult{}, err
	}
	if !entry.staticTiers[trace.MatchedTier] {
		trace.MatchedTier = ""
	}
	return trace, nil
}
