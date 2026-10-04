package model

// GetMissingModels returns enabled channel model names that no metadata record
// covers. A name is covered by an exact record with that name or by any
// prefix/contains/suffix rule record, whatever the rule's status: creating an
// exact record for it would take precedence over the rule and could publish a
// model the rule hides.
func GetMissingModels() ([]string, error) {
	// 1. 获取所有已启用模型（去重）
	models := GetEnabledModels()
	if len(models) == 0 {
		return []string{}, nil
	}

	// 2. 查询同名记录与全部名称规则记录
	var records []Model
	if err := DB.Select("model_name", "name_rule").
		Where("model_name IN ? OR name_rule <> ?", models, NameRuleExact).
		Find(&records).Error; err != nil {
		return nil, err
	}

	// 3. 收集未被任何记录覆盖的模型
	missing := make([]string, 0)
	for _, name := range models {
		covered := false
		for i := range records {
			if records[i].MatchesName(name) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, name)
		}
	}
	return missing, nil
}
