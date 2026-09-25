package common

import (
	"context"
	"github.com/QuantumNous/new-api/relaykit/types"
	"slices"
)

// RecordConversionDiagnostics bounds request-correlated protocol evidence.
func (info *RelayInfo) RecordConversionDiagnostics(_ context.Context, values []types.ConversionDiagnostic) {
	if info == nil {
		return
	}
	for _, value := range values {
		if len(info.ConversionDiagnostics) >= 32 {
			return
		}
		if !slices.Contains(info.ConversionDiagnostics, value) {
			info.ConversionDiagnostics = append(info.ConversionDiagnostics, value)
		}
	}
}
