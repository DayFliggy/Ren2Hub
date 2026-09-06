package main

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Run after prepare-frontend-embed to test what the binary actually embeds.
func TestProductionEmbeddedAssetGraph(t *testing.T) {
	if bytes.Contains(nextIndexPage, []byte(`content="placeholder"`)) {
		t.Skip("build frontend and prepare embed assets for production verification")
	}
	embedded, err := fs.Sub(nextBuildFS, "frontend/embed-dist")
	require.NoError(t, err)
	content, err := fs.ReadFile(embedded, ".vite/manifest.json")
	require.NoError(t, err)
	var manifest map[string]struct {
		File           string   `json:"file"`
		CSS            []string `json:"css"`
		Assets         []string `json:"assets"`
		Imports        []string `json:"imports"`
		DynamicImports []string `json:"dynamicImports"`
	}
	require.NoError(t, common.Unmarshal(content, &manifest))
	require.NotEmpty(t, manifest)
	for name, entry := range manifest {
		for _, dependency := range append(entry.Imports, entry.DynamicImports...) {
			require.Contains(t, manifest, dependency, name)
		}
		for _, file := range append(append([]string{entry.File}, entry.CSS...), entry.Assets...) {
			data, err := fs.ReadFile(embedded, file)
			require.NoError(t, err, "%s references missing embedded asset %s", name, file)
			require.NotEmpty(t, data, file)
		}
	}
}
