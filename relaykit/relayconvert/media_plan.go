package relayconvert

import (
	"net/url"
	"slices"
	"strings"

	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// MediaRequirement contains only routing metadata, never a URL or media bytes.
type MediaRequirement struct {
	ToolResult bool   `json:"tool_result,omitempty"`
	Kind       string `json:"kind"`
	SourceKind string `json:"source_kind"`
	MIMEType   string `json:"mime_type,omitempty"`
	Path       string `json:"path"`
	Detail     string `json:"detail,omitempty"`
}

// RequiresMediaOwnership distinguishes provider-owned resources from public URLs.
func RequiresMediaOwnership(uri string) bool {
	parsed, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return false
	}
	if strings.EqualFold(parsed.Scheme, "gs") {
		return true
	}
	return strings.EqualFold(parsed.Hostname(), "generativelanguage.googleapis.com") &&
		(strings.Contains(parsed.Path, "/files/") || strings.HasSuffix(parsed.Path, "/files"))
}

// thoughtSignatureSkipValidator is the bypass value Google documents for both
// the Gemini API and Vertex AI; converters attach NewAPI's value instead.
const thoughtSignatureSkipValidator = "skip_thought_signature_validator"

// IsThoughtSignatureBypass reports whether a Gemini thoughtSignature is a
// public bypass value: the one converters attach to cross-protocol history, or
// the one Google documents for clients. Neither is provider-issued state, so
// neither needs an ownership binding.
func IsThoughtSignatureBypass(value string) bool {
	return value == sharedgemini.ThoughtSignatureBypassValue || value == thoughtSignatureSkipValidator
}

func mediaString(value any) string {
	text, _ := value.(string)
	return text
}

func mediaKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return "document"
	}
}

func mediaSource(data, mimeType string, inline bool) (string, string) {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if data == "" {
		return "missing", mimeType
	}
	if strings.HasPrefix(data, "data:") {
		header, _, found := strings.Cut(strings.TrimPrefix(data, "data:"), ",")
		if !found {
			return "missing", mimeType
		}
		mimeType, _, _ = strings.Cut(header, ";")
		return "inline", strings.ToLower(mimeType)
	}
	if RequiresMediaOwnership(data) {
		return "owned", mimeType
	}
	if inline {
		return "inline", mimeType
	}
	parsed, err := url.Parse(data)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "unsupported", mimeType
	}
	return "url", mimeType
}

func (r *RequestRequirements) inspectMedia(part map[string]any, path string) {
	media := MediaRequirement{Path: path}
	var data, mimeType string
	inline := false
	invalidShape := false
	kind := mediaString(part["type"])
	switch {
	case r.Format == types.RelayFormatGemini && (part["inlineData"] != nil || part["fileData"] != nil):
		source, _ := part["inlineData"].(map[string]any)
		inline = source != nil
		if inline {
			data = mediaString(source["data"])
		} else {
			source, _ = part["fileData"].(map[string]any)
			data = mediaString(source["fileUri"])
		}
		mimeType = mediaString(source["mimeType"])
		media.Kind = mediaKind(strings.ToLower(mimeType))
	case r.Format == types.RelayFormatClaude && (kind == "image" || kind == "document"):
		media.Kind = kind
		source, _ := part["source"].(map[string]any)
		data, mimeType = mediaString(source["url"]), mediaString(source["media_type"])
		switch mediaString(source["type"]) {
		case "url":
		case "base64":
			data, inline = mediaString(source["data"]), true
		default:
			invalidShape = true
		}
	case kind == "image_url" || kind == "input_image":
		media.Kind, media.Detail = "image", mediaString(part["detail"])
		data = mediaString(part["image_url"])
		if image, ok := part["image_url"].(map[string]any); ok {
			data, mimeType = mediaString(image["url"]), mediaString(image["mime_type"])
			if media.Detail == "" {
				media.Detail = mediaString(image["detail"])
			}
		}
		if data == "" && r.Format == types.RelayFormatOpenAIResponses {
			data = mediaString(part["url"])
		}
	case kind == "input_audio":
		media.Kind = "audio"
		source, _ := part["input_audio"].(map[string]any)
		if source == nil && r.Format == types.RelayFormatOpenAIResponses {
			source = part
		}
		data, inline = mediaString(source["data"]), true
		if format := mediaString(source["format"]); format != "" {
			mimeType = "audio/" + format
		} else if r.Format == types.RelayFormatOpenAI {
			invalidShape = true
		}
	case kind == "video_url" || kind == "input_video":
		media.Kind = "video"
		data = mediaString(part["video_url"])
		if source, ok := part["video_url"].(map[string]any); ok {
			data, mimeType = mediaString(source["url"]), mediaString(source["mime_type"])
			invalidShape = r.Format == types.RelayFormatOpenAI
			for key := range source {
				if key != "url" {
					r.projectionLoss("media_extension_not_portable", path+".video_url."+key, "conversion does not preserve this video setting", "")
				}
			}
		}
		if data == "" && r.Format == types.RelayFormatOpenAIResponses {
			data = mediaString(part["url"])
		}
	case kind == "file" || kind == "input_file":
		media.Kind = "document"
		source, ok := part["file"].(map[string]any)
		if !ok {
			source = part
		}
		data, mimeType = mediaString(source["file_url"]), mediaString(source["mime_type"])
		if data == "" {
			data, inline = mediaString(source["file_data"]), true
		}
		if r.Format == types.RelayFormatOpenAI {
			_, hasFilename := source["filename"].(string)
			invalidShape = !inline || !hasFilename
		}
	default:
		return
	}
	if r.Format == types.RelayFormatOpenAI {
		switch kind {
		case "image_url", "input_audio", "file", "video_url":
		default:
			invalidShape = true
		}
	}
	media.SourceKind, media.MIMEType = mediaSource(data, mimeType, inline)
	if invalidShape {
		media.SourceKind = "unsupported"
	}
	r.Media = append(r.Media, media)
}

func planMediaConversion(req RequestRequirements, target types.RelayFormat) ([]types.ConversionDiagnostic, error) {
	var diagnostics []types.ConversionDiagnostic
	for _, media := range req.Media {
		canConvert := media.SourceKind == "inline" || media.SourceKind == "url"
		if media.ToolResult && media.SourceKind != "owned" {
			// Chat tool content and Gemini function-response parts have no media
			// conversion path; native forwarding retains their original semantics.
			canConvert = canConvert && req.Format != types.RelayFormatOpenAI && req.Format != types.RelayFormatGemini
			// Gemini tool results carry images and documents, never audio or video.
			audioVideo := slices.Contains([]string{"audio", "video"}, media.Kind) || slices.Contains([]string{"audio", "video"}, mediaKind(media.MIMEType))
			canConvert = canConvert && !(target == types.RelayFormatGemini && audioVideo)
			if target == types.RelayFormatGemini && media.MIMEType != "" {
				canConvert = canConvert && sharedgemini.IsSupportedToolResultMimeType(media.MIMEType)
			}
		}
		if media.Kind == "image" {
			canConvert = canConvert && (media.MIMEType == "" || strings.HasPrefix(media.MIMEType, "image/"))
		} else if media.ToolResult && req.Format == types.RelayFormatClaude && target == types.RelayFormatOpenAIResponses {
			// This direct converter preserves document sources without routing through Chat.
			canConvert = canConvert && media.Kind == "document"
		} else {
			// Claude and Gemini route through Chat, whose existing converters
			// only preserve images from these protocols.
			canConvert = canConvert && req.Format != types.RelayFormatClaude && req.Format != types.RelayFormatGemini
			switch target {
			case types.RelayFormatOpenAIResponses:
				canConvert = canConvert && req.Format == types.RelayFormatOpenAI
			case types.RelayFormatOpenAI:
				canConvert = canConvert && req.Format == types.RelayFormatOpenAIResponses &&
					(media.Kind != "document" || media.SourceKind == "inline")
			case types.RelayFormatGemini:
				if media.MIMEType != "" {
					canConvert = canConvert && sharedgemini.IsSupportedMimeType(media.MIMEType)
				}
			case types.RelayFormatClaude:
				canConvert = canConvert && media.Kind == "document" && media.MIMEType == "application/pdf"
			default:
				canConvert = false
			}
		}
		if !canConvert {
			diagnostic := types.ConversionDiagnostic{Code: "media_conversion_unavailable", Path: media.Path,
				Message: "the selected converter cannot preserve this media type or source", Severity: types.ConversionDiagnosticError, From: req.Format, To: target}
			if media.SourceKind == "owned" {
				diagnostic.Code = "native_state_required"
				diagnostic.Message = "provider-owned media requires the original protocol and owner"
			}
			diagnostics = append(diagnostics, diagnostic)
			if media.ToolResult && media.SourceKind != "owned" && req.Format == types.RelayFormatOpenAIResponses && target == types.RelayFormatGemini {
				// The direct converter omits tool media Gemini cannot carry with
				// its own error diagnostic, so the policy decides: safe and
				// strict reject before send, allow sends without the media.
				continue
			}
			return diagnostics, &types.ConversionLossError{Diagnostics: diagnostics}
		}
		if media.Detail != "" && target != types.RelayFormatOpenAI && target != types.RelayFormatOpenAIResponses {
			diagnostics = append(diagnostics, types.ConversionDiagnostic{Code: "media_detail_not_portable", Path: media.Path + ".detail",
				Message: "the selected converter does not preserve explicit image detail", Severity: types.ConversionDiagnosticWarning, From: req.Format, To: target})
		}
	}
	return diagnostics, nil
}
