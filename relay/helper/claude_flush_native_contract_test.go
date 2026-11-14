package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// The invalid in-memory DTO exercises the function's native serialization-error
// contract. It does not claim a supplier JSON response can contain a Go channel.
func TestClaudeDataPreservesNativeFlushOnMarshalFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		response  dto.ClaudeResponse
		canceled  bool
		wantFlush bool
		wantEvent bool
	}{
		{name: "marshal_failure", response: dto.ClaudeResponse{Type: "error", Error: make(chan int)}, wantFlush: true},
		{name: "valid_event", response: dto.ClaudeResponse{Type: "message_stop"}, wantFlush: true, wantEvent: true},
		{name: "already_canceled", response: dto.ClaudeResponse{Type: "message_stop"}, canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			if tc.canceled {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			if err := ClaudeData(c, tc.response); err != nil {
				t.Fatalf("native ClaudeData return changed: %v", err)
			}
			if recorder.Flushed != tc.wantFlush {
				t.Fatalf("flushed=%t, want %t", recorder.Flushed, tc.wantFlush)
			}
			body := recorder.Body.String()
			if tc.wantEvent {
				if !strings.Contains(body, "event: message_stop") || !strings.Contains(body, `"type":"message_stop"`) {
					t.Fatalf("valid event missing from body: %q", body)
				}
			} else if body != "" {
				t.Fatalf("no serialized event expected, got %q", body)
			}
		})
	}
}
