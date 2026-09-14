package router

import (
	"bytes"
	"context"
	"encoding/pem"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayOfficialRealtimeSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_realtime.py"}, {node, "verify_realtime.mjs"}} {
		for _, scenario := range []string{"interleaved_completed", "interleaved_interrupted", "provider_error", "client_close"} {
			t.Run(runtime.script+"/"+scenario, func(t *testing.T) {
				gatewayURL, key, accepted, sends := setupGatewayRealtime(t)
				target, err := url.Parse(gatewayURL)
				require.NoError(t, err)
				// The SDK requires WSS; trust only this test server's certificate.
				secureGateway := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(target))
				t.Cleanup(secureGateway.Close)
				certificate := filepath.Join(t.TempDir(), "gateway-ca.pem")
				require.NoError(t, os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secureGateway.Certificate().Raw}), 0600))
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
				command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+secureGateway.URL, "GATEWAY_SDK_KEY="+key,
					"GATEWAY_SDK_CA="+certificate, "GATEWAY_REALTIME_SCENARIO="+scenario)
				var output bytes.Buffer
				command.Stdout, command.Stderr = &output, &output
				require.NoError(t, command.Start())
				waited := false
				t.Cleanup(func() {
					cancel()
					if !waited {
						_ = command.Wait()
					}
				})
				var upstream *websocket.Conn
				select {
				case upstream = <-accepted:
					t.Cleanup(func() { _ = upstream.Close() })
				case <-ctx.Done():
					err := command.Wait()
					waited = true
					t.Fatalf("SDK did not open the local realtime upstream: %v\n%s", err, output.String())
				}
				gatewayRealtimeSDKConversation(t, upstream, scenario)
				err = command.Wait()
				waited = true
				require.NoError(t, err, "%s", output.String())
				t.Log(output.String())
				awaitRealtimeLeasesReleased(t)
				assert.EqualValues(t, 1, sends.Load(), "SDK connection must not replay after a committed upgrade")
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				if scenario == "interleaved_completed" {
					assert.Equal(t, 15, logs[0].PromptTokens)
					assert.Equal(t, 27, logs[0].CompletionTokens)
					assert.Contains(t, logs[0].Content, "realtime usage: provider")
				}
				if scenario == "interleaved_interrupted" {
					assert.Greater(t, logs[0].CompletionTokens, 20, "the unfinished response must retain its delivered estimate")
					assert.Contains(t, logs[0].Content, "realtime usage: provider_and_estimated")
				}
				var failures int64
				require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
				if scenario == "interleaved_interrupted" || scenario == "provider_error" {
					assert.Positive(t, failures)
				} else {
					assert.Zero(t, failures)
				}
			})
		}
	}
}

func gatewayRealtimeSDKConversation(t *testing.T, upstream *websocket.Conn, scenario string) {
	t.Helper()
	write := func(event string) {
		t.Helper()
		require.NoError(t, upstream.SetWriteDeadline(time.Now().Add(3*time.Second)))
		require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(event)))
	}
	read := func(eventType string) []byte {
		t.Helper()
		require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
		_, body, err := upstream.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, eventType, gjson.GetBytes(body, "type").String())
		return body
	}
	write(`{"type":"session.created","event_id":"session-created","session":{"id":"sdk-session","object":"realtime.session","type":"realtime","model":"gpt-4o-mini","output_modalities":["text"]}}`)
	update := read("session.update")
	assert.Equal(t, "isolated realtime sdk", gjson.GetBytes(update, "session.instructions").String())
	assert.Equal(t, "square", gjson.GetBytes(update, "session.tools.0.name").String())
	input := read("conversation.item.create")
	assert.Equal(t, "hello", gjson.GetBytes(input, "item.content.0.text").String())
	for range 2 {
		request := read("response.create")
		assert.Equal(t, "none", gjson.GetBytes(request, "response.conversation").String())
	}
	write(`{"type":"session.updated","event_id":"session-updated","session":{"id":"sdk-session","object":"realtime.session","type":"realtime","instructions":"isolated realtime sdk","output_modalities":["text"]}}`)
	write(`{"type":"response.created","event_id":"created-a","response":{"id":"response-a","object":"realtime.response","status":"in_progress","output":[]}}`)
	write(`{"type":"response.created","event_id":"created-b","response":{"id":"response-b","object":"realtime.response","status":"in_progress","output":[]}}`)
	write(`{"type":"response.output_text.delta","event_id":"text-a1","response_id":"response-a","item_id":"item-a","output_index":0,"content_index":0,"delta":"hello"}`)
	if scenario == "client_close" {
		require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
		_, _, err := upstream.ReadMessage()
		require.Error(t, err, "client SDK close must terminate the upstream connection")
		return
	}
	if scenario == "provider_error" {
		write(`{"type":"error","event_id":"provider-error","error":{"type":"server_error","code":"upstream_failure","message":"isolated upstream failure","param":null}}`)
		return
	}
	write(`{"type":"response.output_item.added","event_id":"item-b-added","response_id":"response-b","output_index":0,"item":{"id":"item-b","type":"function_call","status":"in_progress","call_id":"call-b","name":"square","arguments":""}}`)
	write(`{"type":"response.function_call_arguments.delta","event_id":"args-b1","response_id":"response-b","item_id":"item-b","output_index":0,"call_id":"call-b","delta":"{\"value\":"}`)
	write(`{"type":"response.output_text.done","event_id":"text-a-done","response_id":"response-a","item_id":"item-a","output_index":0,"content_index":0,"text":"hello"}`)
	write(`{"type":"response.done","event_id":"done-a","response":{"id":"response-a","object":"realtime.response","status":"completed","output":[{"id":"item-a","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`)
	if scenario == "interleaved_interrupted" {
		require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
		return
	}
	write(`{"type":"response.function_call_arguments.delta","event_id":"args-b2","response_id":"response-b","item_id":"item-b","output_index":0,"call_id":"call-b","delta":"7}"}`)
	write(`{"type":"response.function_call_arguments.done","event_id":"args-b-done","response_id":"response-b","item_id":"item-b","output_index":0,"call_id":"call-b","name":"square","arguments":"{\"value\":7}"}`)
	write(`{"type":"response.output_item.done","event_id":"item-b-done","response_id":"response-b","output_index":0,"item":{"id":"item-b","type":"function_call","status":"completed","call_id":"call-b","name":"square","arguments":"{\"value\":7}"}}`)
	write(`{"type":"response.done","event_id":"done-b","response":{"id":"response-b","object":"realtime.response","status":"completed","output":[{"id":"item-b","type":"function_call","status":"completed","call_id":"call-b","name":"square","arguments":"{\"value\":7}"}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}}`)
	toolResult := read("conversation.item.create")
	assert.Equal(t, "function_call_output", gjson.GetBytes(toolResult, "item.type").String())
	assert.Equal(t, "call-b", gjson.GetBytes(toolResult, "item.call_id").String())
	assert.Equal(t, "49", gjson.GetBytes(toolResult, "item.output").String())
	require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
}
