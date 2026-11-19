//go:build upstream_probe

package router

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Shared error-priority changes remain pending upstream. These desired
// responses are failing diagnostics, not default local regression contracts.
func TestUpstreamMidjourneyContinuationOfMissingOrForeignTaskIsNotFoundFirst(t *testing.T) {
	for _, owner := range []string{"missing", "foreign"} {
		for _, condition := range []string{"empty manual list", "all candidates full", "model not allowed"} {
			t.Run(owner+"/"+condition, func(t *testing.T) {
				route := newMjRoute(t)
				a := route.channel(t, "origin-a", "key-a1\nkey-a2")
				b := route.channel(t, "other-b", "key-b")
				if owner == "foreign" {
					route.origin(t, mjUserID+1, a.Id)
				}
				limits := ""
				if condition == "model not allowed" {
					limits = "gpt-4o"
				}
				token := route.token(t, "mjmissing", limits)
				switch condition {
				case "empty manual list":
					route.manual(t, token)
				case "all candidates full":
					route.fill(t, a)
					route.fill(t, b)
				}
				before := mjUserQuota(t)

				recorder := route.submit(t, token, "/mj/submit/change", mjContinuations[0].body)

				assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				assert.JSONEq(t, `{"code":4,"description":"task_not_found ","type":"upstream_error"}`, recorder.Body.String())
				assert.Zero(t, route.upstream.sends)
				assert.Equal(t, before, mjUserQuota(t))
			})
		}
	}
}

func TestUpstreamMidjourneyContinuationLookupFailureIsPrivate500(t *testing.T) {
	route := newMjRoute(t)
	a := route.channel(t, "origin-a", "key-a")
	route.origin(t, mjUserID, a.Id)
	token := route.token(t, "mjstorage", "")
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:mj-origin-lookup-failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Midjourney" {
			tx.AddError(errors.New("private database failure fixture"))
		}
	}))

	recorder := route.submit(t, token, "/mj/submit/change", mjContinuations[0].body)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "get_origin_task_failed")
	assert.NotContains(t, recorder.Body.String(), "private database")
	assert.Zero(t, route.upstream.sends)
}
