//go:build known_upstream_restored

package router

import (
	"errors"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"testing"
)

func TestRestoredProbeNativeRemixRouteOriginLookupFailureIsPrivate500(t *testing.T) {
	route := newRemixRoute(t)
	authorization := route.token(t, "remixstorage", "")
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:remix-task-lookup-failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Task" {
			tx.AddError(errors.New("private database failure fixture"))
		}
	}))

	recorder := route.remix(t, authorization, "task_origin_public")

	assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "get_origin_task_failed")
	assert.NotContains(t, recorder.Body.String(), "private database")
	assert.Zero(t, route.upstream.sends)
}
