package types

// ProviderCredentialError identifies a selected credential rejected during
// authentication before the model request is sent. It must not penalize the
// entire provider or turn transient token exchange failures into key failures.
type ProviderCredentialError struct{ Err error }

func (e *ProviderCredentialError) Error() string { return e.Err.Error() }
func (e *ProviderCredentialError) Unwrap() error { return e.Err }
