package types

// TaskPluginRef identifies immutable executable code, independently of the
// currently active plugin or routing generation.
type TaskPluginRef struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	SourceHash string `json:"source_hash"`
}
