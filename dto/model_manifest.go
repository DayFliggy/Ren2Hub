package dto

import "github.com/QuantumNous/new-api/constant"

// ModelManifest is an internal, request-scoped projection, never a persisted
// model catalog. Public protocols must project only their documented fields.
type ModelManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	Models        []ModelManifestEntry `json:"models"`
}

type ModelManifestEntry struct {
	ID                     string                  `json:"id"`
	Visible                bool                    `json:"visible"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	Capabilities           *ModelCapabilities      `json:"capabilities,omitempty"`
	PriceConfigured        bool                    `json:"price_configured"`
	PriceVersion           string                  `json:"price_version"`
	OwnerChannelType       int                     `json:"-"`
}
