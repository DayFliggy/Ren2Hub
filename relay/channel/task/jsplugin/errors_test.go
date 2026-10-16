package jsplugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginErrorsMaskConfiguredAndAllowedHosts(t *testing.T) {
	const detail = "502 custom.provider.example failed via proxy.private.example and files.private.example; see https://docs.example.org/errors"
	source := strings.Replace(mockPlugin, `author: {name: "Test"},`, `author: {name: "Test"}, allowedHosts: ["files.private.example"],`, 1)
	source = strings.Replace(source, `if (!ctx.requestBody.prompt) throw new Error("prompt required");`, `throw new Error("`+detail+`");`, 1)
	source = strings.Replace(source, `taskId: resp.body.id,`, `taskId: (() => { throw new Error("`+detail+`"); })(),`, 1)
	source = strings.Replace(source, `return {taskId: body.id, status: "SUCCESS", progress: "100%", url: body.url};`, `throw new Error("`+detail+`");`, 1)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	a := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://custom.provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.ChannelSetting.Proxy = "http://proxy.private.example:8080"
	a.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/mock-task", nil)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, info.ChannelBaseUrl)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, info.ChannelSetting)
	c.Set("task_request", map[string]any{"prompt": "p"})

	validation := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, validation)
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"upstream"}`))}
	_, submission := a.ParseResponse(c, response, info)
	require.NotNil(t, submission)
	_, polling := a.ParseTaskResultForTask(&model.Task{}, nil, []byte(`{}`))
	require.Error(t, polling)
	for _, message := range []string{validation.Message, submission.Message, polling.Error()} {
		assert.NotContains(t, message, "custom.provider.example")
		assert.NotContains(t, message, "proxy.private.example")
		assert.NotContains(t, message, "files.private.example")
		assert.Contains(t, message, "https://docs.example.org/errors")
		assert.Contains(t, message, "502")
	}
	assert.ErrorContains(t, validation.Error, "files.private.example", "the private cause remains available to server classification")

	t.Run("masking preserves cancellation and capacity classification", func(t *testing.T) {
		cause := errors.Join(errors.New(detail), context.Canceled, pluginruntime.ErrCallAdmissionTimeout)
		masked := a.maskUpstreamError(cause)
		assert.ErrorIs(t, masked, context.Canceled)
		assert.ErrorIs(t, masked, pluginruntime.ErrCallAdmissionTimeout)
		assert.NotContains(t, masked.Error(), "files.private.example")
	})
}
