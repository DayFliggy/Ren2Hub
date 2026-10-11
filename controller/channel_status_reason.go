package controller

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

const channelStatusReasonLimit = 500

var channelStatusSecrets = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://[^\s<>"']+`),
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[^\s,;"']+`),
	regexp.MustCompile(`(?i)["']?\b(?:authorization|proxy-authorization|cookie|set-cookie|[a-z_]*(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|password|secret|credential)|token|key)["']?\s*[=:]\s*(?:"[^"]*"|'[^']*'|[^\r\n,;]+)`),
	regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]+|AIza[a-z0-9_-]+|eyJ[a-z0-9_.-]+)\b`),
	regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	regexp.MustCompile(`\b[A-Za-z0-9_-]{40,}\b`),
}

// Only a bounded, redacted message is projected. Upstream response objects can
// contain credentials and request bodies, so their other fields never leave here.
func sanitizeChannelStatusReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if strings.HasPrefix(reason, "{") || strings.HasPrefix(reason, "[") {
		var envelope struct {
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
		}
		if common.Unmarshal([]byte(reason), &envelope) != nil {
			return constant.ChannelStatusReasonUnparsableBody
		}
		var nested struct {
			Message string `json:"message"`
		}
		if len(envelope.Error) > 0 && common.Unmarshal(envelope.Error, &nested) == nil && nested.Message != "" {
			reason = nested.Message
		} else {
			reason = envelope.Message
		}
		if reason == "" {
			return constant.ChannelStatusReasonNoDisplayableText
		}
	}
	if at := strings.IndexAny(reason, "{["); at >= 0 {
		reason = reason[:at] + " [upstream response body hidden]"
	}
	for _, pattern := range channelStatusSecrets {
		reason = pattern.ReplaceAllString(reason, "[hidden]")
	}
	reason = common.MaskSensitiveInfo(reason)
	reason = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, reason)
	reason = strings.Join(strings.Fields(reason), " ")
	runes := []rune(reason)
	if len(runes) > channelStatusReasonLimit {
		reason = string(runes[:channelStatusReasonLimit-1]) + "…"
	}
	return reason
}

func channelStatusSummary(channel *model.Channel) (string, int64) {
	if constant.IsRetiredChannelType(channel.Type) {
		return constant.ErrRetiredChannelType.Error(), 0
	}
	if channel.Status != common.ChannelStatusAutoDisabled {
		return "", 0
	}
	var info struct {
		Reason string `json:"status_reason"`
		Time   int64  `json:"status_time"`
	}
	if common.Unmarshal([]byte(channel.OtherInfo), &info) != nil {
		return "", 0
	}
	if info.Time < 0 || info.Time > 253402300799 {
		info.Time = 0
	}
	return sanitizeChannelStatusReason(info.Reason), info.Time
}

func channelWithSafeStatusInfo(channel *model.Channel) *model.Channel {
	copy := *channel
	copy.Status = channel.EffectiveStatus()
	clearChannelInfo(&copy)
	if copy.OtherInfo == "" {
		return &copy
	}
	var info map[string]json.RawMessage
	if common.Unmarshal([]byte(copy.OtherInfo), &info) != nil {
		copy.OtherInfo = "{}"
		return &copy
	}
	if _, exists := info["status_reason"]; exists {
		reason, when := channelStatusSummary(channel)
		info["status_reason"], _ = common.Marshal(reason)
		info["status_time"], _ = common.Marshal(when)
	}
	encoded, _ := common.Marshal(info) // RawMessage values came from valid JSON.
	copy.OtherInfo = string(encoded)
	return &copy
}
