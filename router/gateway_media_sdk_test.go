package router

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGatewayOfficialMediaSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_media.py"}, {node, "verify_media.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			url, key, _ := setupGatewaySDKWithUpstream(t, gatewayMediaSDKUpstream)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+url, "GATEWAY_SDK_KEY="+key)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Log(string(output))
		})
	}
}

func gatewayMediaSDKUpstream(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/audio/transcriptions" {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("stream") == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"transcript.text.done\",\"text\":\"hello\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}\n\n")
		} else {
			_, _ = io.WriteString(w, `{"text":"hello"}`)
		}
		return
	}
	var request map[string]any
	if err := common.DecodeJson(r.Body, &request); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/embeddings":
		vector := any([]float64{1, 0})
		if request["encoding_format"] == "base64" {
			vector = "AACAPwAAAAA="
		}
		body, _ := common.Marshal(map[string]any{"object": "list", "model": "gpt-4o-mini", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}, map[string]any{"object": "embedding", "index": 1, "embedding": vector}}, "usage": map[string]int{"prompt_tokens": 10, "total_tokens": 10}})
		_, _ = w.Write(body)
	case "/v1/images/generations":
		if request["stream"] != true {
			_, _ = io.WriteString(w, `{"created":1710000000,"data":[{"b64_json":"AAECAw=="}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"image_generation.partial_image\",\"partial_image_index\":0,\"b64_json\":\"AAECAw==\",\"created_at\":1710000000,\"background\":\"opaque\",\"output_format\":\"png\",\"quality\":\"low\",\"size\":\"1024x1024\"}\n\n")
		if request["prompt"] == "interrupt" {
			return
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"AAECAw==\",\"created_at\":1710000000,\"background\":\"opaque\",\"output_format\":\"png\",\"quality\":\"low\",\"size\":\"1024x1024\",\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"total_tokens\":30,\"input_tokens_details\":{\"text_tokens\":10,\"image_tokens\":0}}}\n\n")
	case "/v1/audio/speech":
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte{0, 1, 2, 3})
	default:
		http.Error(w, "unexpected media path", 404)
	}
}
