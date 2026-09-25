package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskModelAliasesRejectCyclesAndCrossPluginAmbiguity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previous, cached := DB, taskAliasViewPtr.Load()
	DB = db
	taskAliasViewPtr.Store(nil)
	t.Cleanup(func() {
		DB = previous
		taskAliasViewPtr.Store(cached)
		connection, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.AutoMigrate(&Channel{}))
	registry := jsplugin.NewRegistry()
	for index, key := range []string{"alias-alpha", "alias-beta"} {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:"Alias",version:"1.0.0",author:{name:"Test"},channelTypes:[%d],models:[%q,"shared-tail"],fetchMode:"per_task"};export function buildSubmitRequest(){return {url:"https://example.com"}};export function parseSubmitResponse(){return {taskId:"job"}};export function buildQueryRequest(){return {url:"https://example.com"}};export function parseTaskResult(){return {status:"SUCCESS"}};`, key, 24+index, key+"-model")
		_, err := registry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	for _, item := range []struct{ models, mapping string }{
		{"stable", `{"stable":"hop","hop":"alias-alpha-model"}`},
		{"cycle-a", `{"cycle-a":"cycle-b","cycle-b":"cycle-a"}`},
		{"ambiguous", `{"ambiguous":"alias-alpha-model"}`},
		{"AMBIGUOUS", `{"AMBIGUOUS":"alias-beta-model"}`},
		{"shared-alias", `{"shared-alias":"shared-tail"}`},
	} {
		mapping := item.mapping
		require.NoError(t, db.Create(&Channel{Status: common.ChannelStatusEnabled, Models: item.models, ModelMapping: &mapping}).Error)
	}
	generation := registry.Generation()
	resolved, found := ResolveTaskModelAlias(generation, "STABLE")
	require.True(t, found)
	assert.Equal(t, "alias-alpha-model", resolved.Declared)
	assert.Equal(t, "alias-alpha", resolved.PluginKey)
	for _, name := range []string{"cycle-a", "cycle-b", "ambiguous", "AMBIGUOUS", "shared-alias"} {
		_, found := ResolveTaskModelAlias(generation, name)
		assert.False(t, found, name)
	}
	assert.Len(t, generation.PluginsByModel("shared-tail"), 2, "direct shared model still retains both providers")
}
