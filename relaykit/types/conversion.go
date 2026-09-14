package types

import (
	"fmt"
	"strings"
)

type ConversionLossPolicy string

const (
	ConversionLossPolicySafe   ConversionLossPolicy = "safe"
	ConversionLossPolicyAllow  ConversionLossPolicy = "allow"
	ConversionLossPolicyStrict ConversionLossPolicy = "strict"
)

func (p ConversionLossPolicy) Validate() error {
	switch p {
	case "", ConversionLossPolicySafe, ConversionLossPolicyAllow, ConversionLossPolicyStrict:
		return nil
	default:
		return fmt.Errorf("invalid conversion policy %q", p)
	}
}

type ConversionDiagnosticSeverity string

const (
	ConversionDiagnosticWarning ConversionDiagnosticSeverity = "warning"
	ConversionDiagnosticError   ConversionDiagnosticSeverity = "error"
)

type ConversionDiagnostic struct {
	Code     string                       `json:"code"`
	Path     string                       `json:"path,omitempty"`
	Message  string                       `json:"message"`
	Severity ConversionDiagnosticSeverity `json:"severity"`
	From     RelayFormat                  `json:"from"`
	To       RelayFormat                  `json:"to"`
}

type ConversionLossError struct {
	Diagnostics []ConversionDiagnostic
}

func (e *ConversionLossError) Error() string {
	parts := make([]string, 0, len(e.Diagnostics))
	for _, diagnostic := range e.Diagnostics {
		parts = append(parts, diagnostic.Path+": "+diagnostic.Message)
	}
	return "conversion cannot preserve request semantics: " + strings.Join(parts, "; ")
}

func RejectConversionLoss(policy ConversionLossPolicy, diagnostics []ConversionDiagnostic) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	var rejected []ConversionDiagnostic
	for _, diagnostic := range diagnostics {
		// Provider-owned state cannot be made portable by selecting allow.
		if diagnostic.Code == "native_state_required" || policy == ConversionLossPolicyStrict ||
			(policy != ConversionLossPolicyAllow && diagnostic.Severity == ConversionDiagnosticError) {
			rejected = append(rejected, diagnostic)
		}
	}
	if len(rejected) != 0 {
		return &ConversionLossError{Diagnostics: rejected}
	}
	return nil
}
