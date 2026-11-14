//go:build known_upstream_restored

package router

import (
	"embed"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

//go:embed frontend/embed-dist/probe.txt
var restoredProbeAssets embed.FS

func TestRestoredProbeAdjacentPgPrefix(t *testing.T) {
	old := common.GlobalWebRateLimitEnable
	common.GlobalWebRateLimitEnable = false
	t.Cleanup(func() { common.GlobalWebRateLimitEnable = old })
	r := gin.New()
	SetWebRouter(r, WebAssets{BuildFS: restoredProbeAssets, IndexPage: []byte("probe index")}, func(c *gin.Context) { c.Next() })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pgfoo", nil))
	assert.Equal(t, 404, w.Code)
}
