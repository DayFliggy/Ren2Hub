package billing

import "strings"

// LegacyImagePriceRatio preserves NewAPI's historical DALL-E request pricing.
// It is shared by conversion previews and execution, outside protocol DTOs.
func LegacyImagePriceRatio(model, size, quality string) float64 {
	if !strings.HasPrefix(model, "dall-e") {
		return 1
	}
	sizeRatio, qualityRatio := 1.0, 1.0
	switch size {
	case "256x256":
		sizeRatio = 0.4
	case "512x512":
		sizeRatio = 0.45
	case "1024x1792", "1792x1024":
		sizeRatio = 2
	}
	if model == "dall-e-3" && quality == "hd" {
		qualityRatio = 2
		if size == "1024x1792" || size == "1792x1024" {
			qualityRatio = 1.5
		}
	}
	return sizeRatio * qualityRatio
}
