package types

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestErrorProjectionSanitizesEveryStringWithoutChangingClassification(t *testing.T) {
	err := WithOpenAIError(OpenAIError{Message: "rejected", Type: "api_key:TYPE_SECRET", Param: "api_key:PARAM_SECRET", Code: "api_key:CODE_SECRET", Metadata: json.RawMessage(`{"api_key":"META_SECRET","nested":[{"url":"https://upstream.invalid/private","token":"TOKEN_SECRET","retry":2,"active":true}],"note":"api_key:NOTE_SECRET"}`)}, 502)
	projected := err.ToOpenAIError("https://upstream.invalid")
	raw, marshalErr := json.Marshal(projected)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, s := range []string{"TYPE_SECRET", "PARAM_SECRET", "CODE_SECRET", "META_SECRET", "TOKEN_SECRET", "NOTE_SECRET", "upstream.invalid"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("projection leaked %s: %s", s, raw)
		}
	}
	if err.GetErrorCode() != "api_key:CODE_SECRET" {
		t.Fatal("internal code changed")
	}
	var metadata map[string]any
	if json.Unmarshal(projected.Metadata, &metadata) != nil {
		t.Fatal("metadata must remain JSON")
	}
	nested := metadata["nested"].([]any)[0].(map[string]any)
	if nested["retry"] != float64(2) || nested["active"] != true {
		t.Fatalf("metadata types lost: %#v", nested)
	}
	if metadata["api_key"] != "***" || nested["token"] != "***" || metadata["note"] != "api_key:***" {
		t.Fatalf("unexpected redaction: %#v", metadata)
	}
	if strings.Contains(err.Error(), "META_SECRET") || strings.Contains(err.Error(), "TOKEN_SECRET") {
		t.Fatal("plain Error() leaks metadata before client projection")
	}
	if strings.Contains(err.ToClaudeError().Type, "CODE_SECRET") {
		t.Fatal("Claude type leaked original code")
	}
	if err.RelayError.(OpenAIError).Code != "api_key:CODE_SECRET" {
		t.Fatal("projection mutated original code")
	}
}

func TestErrorProjectionRetainsAllAttemptSecretsAndOriginalCause(t *testing.T) {
	cause := errors.New("FIRST_SECRET SECOND_SECRET https://first.internal/path https://second.internal/path")
	err := NewError(cause, ErrorCodeDoRequestFailed)
	err.BindSensitiveContext([]string{"https://first.internal"}, "FIRST_SECRET")
	err.BindSensitiveContext([]string{"https://second.internal"}, "SECOND_SECRET")
	for _, text := range []string{err.Error(), err.MaskSensitiveError(), err.ToOpenAIError().Message, err.ToClaudeError().Message} {
		for _, secret := range []string{"FIRST_SECRET", "SECOND_SECRET", "first.internal", "second.internal"} {
			if strings.Contains(text, secret) {
				t.Errorf("leaked earlier attempt: %s", text)
			}
		}
	}
	if !errors.Is(err, cause) || err.GetErrorCode() != ErrorCodeDoRequestFailed {
		t.Fatal("internal cause/classification changed")
	}
}

func TestErrorProjectionCountTokenCodeCannotDisableSanitization(t *testing.T) {
	err := WithOpenAIError(OpenAIError{Message: "api_key:COUNT_SECRET", Code: string(ErrorCodeCountTokenFailed)}, 400)
	for _, text := range []string{err.Error(), err.MaskSensitiveError(), err.ToOpenAIError().Message, err.ToClaudeError().Message} {
		if strings.Contains(text, "COUNT_SECRET") {
			t.Errorf("count-token code bypasses sanitization: %s", text)
		}
	}
	if err.GetErrorCode() != ErrorCodeCountTokenFailed {
		t.Fatal("classification must remain unchanged")
	}
}

func TestErrorProjectionSanitizesJSONMetadataStringsAndNestedCode(t *testing.T) {
	for _, metadata := range []string{`"api_key:RAW_SECRET"`, `["api_key:RAW_SECRET",{"password":"PASSWORD_SECRET"}]`, `{"token":{"value":"RAW_SECRET"},"password":["PASSWORD_SECRET"]}`} {
		err := WithOpenAIError(OpenAIError{Message: "rejected", Code: map[string]any{"detail": "api_key:CODE_SECRET", "retry": float64(3)}, Metadata: json.RawMessage(metadata)}, 500)
		result := err.ToOpenAIError()
		raw, e := json.Marshal(result)
		if e != nil {
			t.Fatal(e)
		}
		for _, secret := range []string{"RAW_SECRET", "PASSWORD_SECRET", "CODE_SECRET"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("leaked %s: %s", secret, raw)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("metadata prose leaked %s: %s", secret, err.Error())
			}
		}
		if result.Code.(map[string]any)["retry"] != float64(3) {
			t.Fatal("numeric code metadata changed")
		}
	}
}
