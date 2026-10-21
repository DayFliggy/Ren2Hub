package modellab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 补充推断只返回 NewAPI 厂商名，不产生第二套词表。
func TestVendorNameReturnsNewAPIVendorNames(t *testing.T) {
	require.Equal(t, "OpenAI", VendorName("o4-mini"))
	require.Equal(t, "OpenAI", VendorName("ＧＰＴ-4o"))
	require.Equal(t, "Google", VendorName("gemma-3-27b-it"))
	require.Equal(t, "阿里巴巴", VendorName("qwen3-max"))
	require.Equal(t, "Moonshot", VendorName("kimi-k2"))
	require.Equal(t, "OpenAI", VendorName("openai/gpt-4o"), "provider-prefixed catalog entries resolve too")
	require.Empty(t, VendorName("house-model"))
}

func TestVendorIDKeepsLabSlugsForKnownVendors(t *testing.T) {
	require.Equal(t, "alibaba", VendorID("阿里巴巴"))
	require.Equal(t, "zhipuai", VendorID("智谱"))
	require.Equal(t, "openai", VendorID("OpenAI"))
	require.Equal(t, "nvidia", VendorID("NVIDIA"))
	require.Equal(t, "vendor:百度", VendorID("百度"))
	require.Equal(t, "vendor:independent lab", VendorID("Independent Lab"))
}
