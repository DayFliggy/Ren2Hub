package gemini

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ResolveVeoParameters produces the same bounded values for pricing and wire
// output. Explicit invalid values never become an implicit provider default.
func ResolveVeoParameters(req relaycommon.TaskSubmitReq) (*VeoParameters, error) {
	params := &VeoParameters{DurationSeconds: req.Duration, SampleCount: 1, Resolution: SizeToVeoResolution(req.Size)}
	if params.DurationSeconds == 0 && req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, fmt.Errorf("invalid Veo seconds")
		}
		params.DurationSeconds = seconds
	} else if params.DurationSeconds == 0 {
		params.DurationSeconds = 8
	}
	seen := make(map[string]bool)
	for field := range req.Metadata {
		name := strings.ToLower(field)
		switch name {
		case "durationseconds", "samplecount", "resolution":
			if seen[name] {
				return nil, fmt.Errorf("duplicate Veo parameter aliases: %s", name)
			}
			seen[name] = true
		}
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, params); err != nil {
		return nil, fmt.Errorf("invalid Veo metadata: %w", err)
	}
	if params.DurationSeconds < 1 || params.DurationSeconds > relaycommon.MaxTaskDurationSeconds {
		return nil, fmt.Errorf("Veo durationSeconds must be between 1 and %d", relaycommon.MaxTaskDurationSeconds)
	}
	if params.SampleCount != 1 {
		return nil, fmt.Errorf("Veo task submission supports one output")
	}
	params.Resolution = strings.ToLower(strings.TrimSpace(params.Resolution))
	switch params.Resolution {
	case "720p", "1080p", "4k":
	default:
		return nil, fmt.Errorf("unsupported Veo resolution")
	}
	if params.AspectRatio == "" && req.Size != "" {
		params.AspectRatio = SizeToVeoAspectRatio(req.Size)
	}
	return params, nil
}

// SizeToVeoResolution converts a "WxH" size string to a Veo resolution label.
func SizeToVeoResolution(size string) string {
	parts := strings.SplitN(strings.ToLower(size), "x", 2)
	if len(parts) != 2 {
		return "720p"
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	maxDim := w
	if h > maxDim {
		maxDim = h
	}
	if maxDim >= 3840 {
		return "4k"
	}
	if maxDim >= 1920 {
		return "1080p"
	}
	return "720p"
}

// SizeToVeoAspectRatio converts a "WxH" size string to a Veo aspect ratio.
func SizeToVeoAspectRatio(size string) string {
	parts := strings.SplitN(strings.ToLower(size), "x", 2)
	if len(parts) != 2 {
		return "16:9"
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w <= 0 || h <= 0 {
		return "16:9"
	}
	if h > w {
		return "9:16"
	}
	return "16:9"
}

// VeoResolutionRatio returns the pricing multiplier for the given resolution.
// Standard resolutions (720p, 1080p) return 1.0.
// 4K returns a model-specific multiplier based on Google's official pricing.
func VeoResolutionRatio(modelName, resolution string) float64 {
	if resolution != "4k" {
		return 1.0
	}
	// 4K multipliers derived from Vertex AI official pricing (video+audio base):
	//   veo-3.1-generate:      $0.60 / $0.40 = 1.5
	//   veo-3.1-fast-generate: $0.35 / $0.15 ≈ 2.333
	// Veo 3.0 models do not support 4K; return 1.0 as fallback.
	if strings.Contains(modelName, "3.1-fast-generate") {
		return 2.333333
	}
	if strings.Contains(modelName, "3.1-generate") || strings.Contains(modelName, "3.1") {
		return 1.5
	}
	return 1.0
}
