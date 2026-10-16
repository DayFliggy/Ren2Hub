package middleware

import (
	"github.com/QuantumNous/new-api/constant"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyFailuresStopBeforeAdmissionAndDispatch(t *testing.T) {
	old := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = old })
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/images/generations", "/v1/videos", "/mj/submit/imagine", "/v1/video/generations", "/v1/audio/transcriptions", "/v1/audio/translations", "/v1/images/edits"} {
		for _, stage := range []string{"admission", "distribution"} {
			for _, oversized := range []bool{false, true} {
				t.Run(path+"/"+stage+"/"+map[bool]string{true: "large", false: "invalid"}[oversized], func(t *testing.T) {
					engine := gin.New()
					called := false
					handler := GatewayAdmission()
					if stage == "distribution" {
						handler = Distribute()
					}
					engine.POST(path, handler, func(c *gin.Context) { called = true })
					body := "{"
					want := 400
					if oversized {
						body = strings.Repeat("x", (1<<20)+1)
						want = 413
					}
					request := httptest.NewRequest("POST", path, strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					engine.ServeHTTP(response, request)
					require.Equal(t, want, response.Code, response.Body.String())
					require.False(t, called)
					require.Equal(t, 1, strings.Count(response.Body.String(), "Invalid request:"), response.Body.String())
				})
			}
		}
	}
}

func TestMultipartAndPinnedPluginBodyFailuresKeepStatus(t *testing.T) {
	old := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = old })
	for _, stage := range []string{"multipart-audio", "multipart-image", "plugin-route", "plugin-endpoint"} {
		for _, oversized := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/invalid", true: "/large"}[oversized], func(t *testing.T) {
				engine := gin.New()
				called := false
				path := "/fixture/submit"
				contentType := "application/json"
				if stage == "multipart-audio" {
					path = "/v1/audio/transcriptions"
					contentType = "multipart/form-data; boundary=fixture"
				}
				if stage == "multipart-image" {
					path = "/v1/images/edits"
					contentType = "multipart/form-data; boundary=fixture"
				}
				prepare := Distribute()
				if stage == "plugin-route" {
					prepare = PrepareTaskPluginRoute()
				}
				if stage == "plugin-endpoint" {
					prepare = PrepareTaskPluginEndpoint()
				}
				engine.POST(path, func(c *gin.Context) {
					pluginEngine, err := pluginruntime.Compile("export const native = {};", pluginruntime.Options{Key: "fixture"})
					require.NoError(t, err)
					plugin := &pluginruntime.LoadedPlugin{Meta: pluginruntime.Meta{Key: "fixture"}, Engine: pluginEngine}
					c.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{Plugin: plugin, Route: pluginruntime.Route{Type: pluginruntime.RouteTypeSubmit}})
					c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{Plugin: plugin, Generation: &pluginruntime.RoutingGeneration{}, Protocol: "openai_image"})
				}, prepare, func(c *gin.Context) { called = true })
				body := "{"
				want := 400
				if oversized {
					body = strings.Repeat("x", (1<<20)+1)
					want = 413
				}
				request := httptest.NewRequest("POST", path, strings.NewReader(body))
				request.Header.Set("Content-Type", contentType)
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				require.Equal(t, want, response.Code, response.Body.String())
				require.False(t, called)
			})
		}
	}
}
