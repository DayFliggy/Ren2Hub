package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteVideoDataURLUsesPrivateCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	require.NoError(t, writeVideoDataURL(c, "data:video/mp4;base64,AAE="))
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, "private, max-age=86400", recorder.Header().Get("Cache-Control"))
}
