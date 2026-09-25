package jsplugin

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataPreviewDoesNotExecuteSource(t *testing.T) {
	meta, unresolved, err := PreviewMetadata(`throw new Error("never execute"); export const meta={key:"fixture",version:"1",models:["model"], allowedHosts:(()=>{throw new Error("never call")})(),get secret(){throw new Error("never get")}};`)
	require.NoError(t, err)
	assert.Equal(t, "fixture", meta["key"])
	assert.Equal(t, []any{"model"}, meta["models"])
	assert.NotContains(t, meta, "allowedHosts")
	assert.NotContains(t, meta, "secret")
	assert.Contains(t, unresolved, "allowedHosts")
}

func TestMetadataPreviewResolvesSafeConstants(t *testing.T) {
	meta, unresolved, err := PreviewMetadata(`
throw new Error("not executed");
const MODEL = 'source-model', MODELS = [MODEL];
const ALIAS = MODELS;
const ROUTE = {method:'POST',path:'/source/submit',models:ALIAS};
export const meta = {models:ALIAS,routes:[ROUTE]};
function validate(model) { return MODELS.includes(model); }
`)
	require.NoError(t, err)
	assert.Empty(t, unresolved)
	assert.Equal(t, []any{"source-model"}, meta["models"])
	assert.Equal(t, []any{map[string]any{"method": "POST", "path": "/source/submit", "models": []any{"source-model"}}}, meta["routes"])
}

func TestMetadataPreviewRejectsMutableOrEscapingConstants(t *testing.T) {
	for _, declaration := range []string{
		`let MODELS = ["m"];`,
		`export const MODELS = ["m"];`,
		`const MODELS = getModels();`,
		`const MODELS = LATER; const LATER = ["m"];`,
		`const MODELS = ALIAS; const ALIAS = MODELS;`,
		`const MODELS = ["m"]; MODELS.push("changed");`,
		`const MODELS = ["m"]; MODELS[0] = "changed";`,
		`const MODELS = ["m"]; mutate(MODELS);`,
		`const MODELS = ["m"]; const ALIAS = MODELS; ALIAS.push("changed");`,
		`const MODELS = ["m"]; const BOX = {models: MODELS}; mutate(BOX);`,
		`const MODELS = ["m"]; const BOX = {models: MODELS, includes() { this.models.push("changed"); }}; BOX.includes();`,
		`const MODELS = ["m"]; export {MODELS};`,
		`const MODELS = ["m"]; function expose() { return MODELS; }`,
		`const MODELS = ["m"]; function change(MODELS) { MODELS.push("changed"); }`,
		`const MODELS = ["m"]; M\u004fDELS.push("changed");`,
	} {
		t.Run(declaration, func(t *testing.T) {
			meta, unresolved, err := PreviewMetadata(declaration + ` export const meta={models:MODELS,baseUrl:'https://example.com'};`)
			require.NoError(t, err)
			assert.NotContains(t, meta, "models")
			assert.Contains(t, unresolved, "models")
			assert.Equal(t, "https://example.com", meta["baseUrl"])
		})
	}
	for _, source := range []string{
		`const MODELS=['m'];export const meta={models:MODELS};const ALIAS=MODELS;ALIAS.splice(0,1);`,
		`export const meta={models:['m']};mutate(meta);`,
		`export const meta={models:['m']};meta.models.push('changed');`,
	} {
		meta, unresolved, err := PreviewMetadata(source)
		require.NoError(t, err)
		assert.NotContains(t, meta, "models")
		assert.NotEmpty(t, unresolved)
	}
}

func TestMetadataPreviewBoundsConstantExpansion(t *testing.T) {
	var source strings.Builder
	source.WriteString(`const LEVEL0=['m'];`)
	for i := 1; i <= 16; i++ {
		fmt.Fprintf(&source, "const LEVEL%d=[LEVEL%d,LEVEL%d];", i, i-1, i-1)
	}
	source.WriteString(`export const meta={routes:LEVEL16,models:['m']};`)
	meta, unresolved, err := PreviewMetadata(source.String())
	require.NoError(t, err)
	assert.Equal(t, []any{"m"}, meta["models"])
	assert.NotContains(t, meta, "routes")
	assert.Contains(t, unresolved, "routes")
}

func TestMetadataPreviewDoesNotExposeOverwrittenLiteral(t *testing.T) {
	for _, source := range []string{
		`export const meta={models:['m'],models:dynamic()};`,
		`export const meta={models:['m'],get models(){throw new Error('no execution')}};`,
	} {
		meta, unresolved, err := PreviewMetadata(source)
		require.NoError(t, err)
		assert.NotContains(t, meta, "models")
		assert.Contains(t, unresolved, "models")
	}
}
