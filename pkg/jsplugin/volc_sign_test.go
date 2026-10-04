package jsplugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// utils.volcSignV4 reads PascalCase fields, as the jimeng builtin passes them.
// The published d.ts declares camelCase names that the host ignores.
func TestJimengPascalCaseVolcSignV4SignsTheQueryRequest(t *testing.T) {
	fixed := time.Date(2026, 10, 3, 8, 30, 0, 0, time.UTC)
	source, err := os.ReadFile(filepath.Join("..", "..", "plugins", "tasks", "jimeng", "plugin.js"))
	require.NoError(t, err)
	engine, err := Compile(string(source), Options{Now: func() time.Time { return fixed }})
	require.NoError(t, err)

	value, err := engine.Call(t.Context(), "buildQueryRequest", map[string]any{
		"baseUrl": "https://visual.volcengineapi.com", "apiKey": "test-ak|test-sk", "taskId": "task-1", "action": "text_to_video",
	})
	require.NoError(t, err)
	request, ok := value.(map[string]any)
	require.True(t, ok)
	headers, ok := request["headers"].(map[string]any)
	require.True(t, ok)

	url := "https://visual.volcengineapi.com/?Action=CVSync2AsyncGetResult&Version=2022-08-31"
	body := `{"req_key":"jimeng_vgfm_t2v_l20","task_id":"task-1"}`
	assert.Equal(t, url, request["url"])
	assert.Equal(t, body, request["body"])
	want, err := signVolcV4(volcSignRequest{
		Method: "POST", URL: url, Headers: map[string]string{"Content-Type": "application/json"}, Body: body,
		AccessKey: "test-ak", SecretKey: "test-sk", Region: "cn-north-1", Service: "cv",
	}, func() time.Time { return fixed })
	require.NoError(t, err)
	require.NotEmpty(t, want["Authorization"])
	for name, signed := range want {
		assert.Equal(t, signed, headers[name], name)
	}
	assert.Contains(t, headers["Authorization"], "Credential=test-ak/20261003/cn-north-1/cv/request")
}

func TestVolcSignV4IgnoresCamelCaseFields(t *testing.T) {
	engine, err := Compile(`export function buildQueryRequest(ctx) {
		return utils.volcSignV4({method: "POST", url: "https://visual.volcengineapi.com/", accessKey: "ak", secretKey: "sk"});
	}`, Options{})
	require.NoError(t, err)
	_, err = engine.Call(t.Context(), "buildQueryRequest", map[string]any{})
	require.ErrorContains(t, err, "invalid Volcengine signing URL")
}
