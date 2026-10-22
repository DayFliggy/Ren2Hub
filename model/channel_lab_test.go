package model

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useChannelLabDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lab.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Model{}, &Vendor{}))
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous; require.NoError(t, sqlDB.Close()) })
	return db
}

// 渠道分类与定价页使用同一份 NewAPI 厂商词表：原生 defaultVendorRules 能认出
// ernie、doubao 等裸模型名，modellab 只补充原生规则漏掉的名称。
func TestChannelLabsUseNewAPIVendorVocabulary(t *testing.T) {
	db := useChannelLabDB(t)
	azure := Vendor{Name: "Azure", Status: 1}
	require.NoError(t, db.Create(&azure).Error)
	require.NoError(t, db.Create(&Model{ModelName: "gpt-4o", VendorID: azure.Id, Status: 1, NameRule: NameRuleExact}).Error)
	mapping := `{"public":"claude-sonnet-4"}`
	cyclic := `{"a":"b","b":"a"}`
	nonString := `{"a":1}`
	channels := []Channel{
		{Id: 1, Name: "baidu", Key: "k1", Models: "ernie-4.0"},
		{Id: 2, Name: "doubao", Key: "k2", Models: "doubao-pro-32k"},
		{Id: 3, Name: "explicit", Key: "k3", Models: "gpt-4o"},
		{Id: 4, Name: "supplement", Key: "k4", Models: "o4-mini"},
		{Id: 5, Name: "mapped", Key: "k5", Models: "public", ModelMapping: &mapping},
		{Id: 6, Name: "mixed", Key: "k6", Models: "ernie-4.0,claude-sonnet-4"},
		{Id: 7, Name: "cycle", Key: "k7", Models: "a", ModelMapping: &cyclic},
		{Id: 8, Name: "unknown", Key: "k8", Models: "house-model"},
		{Id: 9, Name: "qwen", Key: "k9", Models: "qwen-max"},
		{Id: 10, Name: "non-string mapping", Key: "k10", Models: "a", ModelMapping: &nonString},
	}
	require.NoError(t, db.Create(&channels).Error)
	labs, err := ChannelLabs(context.Background(), []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	require.NoError(t, err)

	require.Equal(t, "百度", labs[1].Name)
	require.Equal(t, "default", labs[1].Source)
	require.Equal(t, "字节跳动", labs[2].Name)
	require.Equal(t, "bytedance-seed", labs[2].ID, "known vendors keep the lab slug the console uses for icons")
	require.Equal(t, "Azure", labs[3].Name, "explicit model metadata wins over name inference")
	require.Equal(t, "vendor", labs[3].Source)
	require.Equal(t, "OpenAI", labs[4].Name, "modellab supplements o4, which the native rules miss")
	require.Equal(t, "openai", labs[4].ID)
	require.Equal(t, "Anthropic", labs[5].Name)
	require.Equal(t, "mapping", labs[5].Source)
	require.Equal(t, "mixed", labs[6].ID)
	require.Equal(t, "invalid_mapping", labs[7].Source)
	require.Equal(t, "unknown", labs[8].ID)
	require.Equal(t, "阿里巴巴", labs[9].Name)
	require.Equal(t, "alibaba", labs[9].ID)
	require.Equal(t, "invalid_mapping", labs[10].Source, "a mapping the relay cannot decode only invalidates its own channel")

	counts, total, err := ChannelLabCounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 10, total)
	unknown := 0
	for _, item := range counts {
		if item.ID == "unknown" {
			unknown = item.Count
		}
	}
	require.Equal(t, 3, unknown, "cycle, unknown model and undecodable mapping share the unknown bucket")
}

func TestPricingDefaultVendorUsesNativeRulesThenSupplement(t *testing.T) {
	metaMap := map[string]*Model{}
	vendorMap := map[int]*Vendor{}
	abilities := []AbilityWithChannel{
		{Ability: Ability{Model: "ernie-4.0"}},
		{Ability: Ability{Model: "mistral-large"}},
		{Ability: Ability{Model: "o4-mini"}},
		{Ability: Ability{Model: "house-model"}},
	}
	initDefaultVendorMapping(metaMap, vendorMap, abilities)
	vendorName := func(model string) string {
		if vendor := vendorMap[metaMap[model].VendorID]; vendor != nil {
			return vendor.Name
		}
		return ""
	}
	require.Equal(t, "百度", vendorName("ernie-4.0"))
	require.Equal(t, "Mistral", vendorName("mistral-large"))
	require.Equal(t, "OpenAI", vendorName("o4-mini"))
	require.Zero(t, metaMap["house-model"].VendorID)
}
