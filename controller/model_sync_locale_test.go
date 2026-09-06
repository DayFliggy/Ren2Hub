package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpstreamMetadataLocaleURLs(t *testing.T) {
	t.Setenv("SYNC_UPSTREAM_BASE", "https://metadata.example/")
	for _, test := range []struct{ locale, prefix string }{
		{"zh", "/api/i18n/zh-CN"},
		{"zh-CN", "/api/i18n/zh-CN"},
		{" ZH-hans ", "/api/i18n/zh-CN"},
		{"zh-TW", "/api/i18n/zh-TW"},
		{"en", "/api/i18n/en"},
		{"ja", "/api/i18n/ja"},
		{"unknown", "/api"},
	} {
		t.Run(test.locale, func(t *testing.T) {
			models, vendors := getUpstreamURLs(test.locale)
			assert.Equal(t, "https://metadata.example"+test.prefix+"/newapi/models.json", models)
			assert.Equal(t, "https://metadata.example"+test.prefix+"/newapi/vendors.json", vendors)
		})
	}
}
