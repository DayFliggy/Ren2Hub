package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetMissingModelsExcludesNamesCoveredByRuleRecords(t *testing.T) {
	enabled := []string{"exact-hit", "prefix-a", "x-prefix-a", "has-mid-x", "end-tail", "uncovered"}
	tests := []struct {
		name    string
		records []Model
		deleted []string
		want    []string
	}{
		{
			name: "no metadata leaves every enabled name missing",
			want: enabled,
		},
		{
			name: "exact, prefix, contains and suffix records cover only matching names",
			records: []Model{
				{ModelName: "exact-hit", NameRule: NameRuleExact, Status: 1},
				{ModelName: "prefix-", NameRule: NameRulePrefix, Status: 1},
				{ModelName: "-mid-", NameRule: NameRuleContains, Status: 1},
				{ModelName: "-tail", NameRule: NameRuleSuffix, Status: 1},
			},
			want: []string{"x-prefix-a", "uncovered"},
		},
		{
			name:    "a hidden rule still covers its names",
			records: []Model{{ModelName: "prefix-", NameRule: NameRulePrefix, Status: 0}},
			want:    []string{"exact-hit", "x-prefix-a", "has-mid-x", "end-tail", "uncovered"},
		},
		{
			name:    "a soft-deleted rule covers nothing",
			records: []Model{{ModelName: "prefix-", NameRule: NameRulePrefix, Status: 1}},
			deleted: []string{"prefix-"},
			want:    enabled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := useOpsTestDB(t, "missing.db")
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &Model{}))
			require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&Channel{
				Id: 1, Name: "primary", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "k1", Group: "default",
			}).Error)
			abilities := []Ability{{Group: "default", Model: "disabled-only", ChannelId: 1, Enabled: false}}
			for _, name := range enabled {
				abilities = append(abilities, Ability{Group: "default", Model: name, ChannelId: 1, Enabled: true})
			}
			require.NoError(t, db.Create(&abilities).Error)
			for i := range test.records {
				require.NoError(t, test.records[i].Insert())
			}
			for _, name := range test.deleted {
				require.NoError(t, db.Where("model_name = ?", name).Delete(&Model{}).Error)
			}

			missing, err := GetMissingModels()
			require.NoError(t, err)
			assert.ElementsMatch(t, test.want, missing)
		})
	}
}
