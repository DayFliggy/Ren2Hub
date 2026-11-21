package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestFieldConflictReplay(t *testing.T) {
	for _, body := range []string{`{"model":"m","tools":[{"input_schema":{"properties":{"id":{},"ID":{}}}}]}`, `{"model":"m","messages":[{"content":[{"type":"tool_use","input":{"id":1,"ID":2}}]}]}`} {
		r := gin.New()
		r.Use(BodyStorageCleanup(), RejectConflictingRequestFields())
		r.POST("/", func(c *gin.Context) {
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			data, err := io.ReadAll(storage)
			require.NoError(t, err)
			require.Equal(t, body, string(data))
			c.Status(204)
		})
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		require.Equal(t, 204, res.Code)
	}
}

func TestRequestFieldConflictUsesLocalErrorResponse(t *testing.T) {
	r := gin.New()
	r.Use(RequestId(), BodyStorageCleanup(), RejectConflictingRequestFields())
	r.POST("/", func(c *gin.Context) { t.Error("conflict reached handler") })
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("POST", "/", strings.NewReader(`{"model":"cheap","Model":"expensive"}`)))
	require.Equal(t, 400, res.Code)
	requestID := res.Header().Get(common.RequestIdKey)
	require.NotEmpty(t, requestID)
	var response struct {
		Error struct{ Code, Type, Message string } `json:"error"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &response))
	require.Equal(t, "duplicate_request_field", response.Error.Code)
	require.Equal(t, "new_api_error", response.Error.Type)
	require.Equal(t, "duplicate request field: Model (request id: "+requestID+")", response.Error.Message)
}
