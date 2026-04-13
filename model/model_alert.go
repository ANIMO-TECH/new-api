package model

import "strings"

func GetAlertModelsForRuntimeModel(runtimeModelName string) ([]*Model, error) {
	if strings.TrimSpace(runtimeModelName) == "" {
		return nil, nil
	}

	var models []*Model
	if err := DB.Find(&models).Error; err != nil {
		return nil, err
	}

	matched := make([]*Model, 0)
	for _, item := range models {
		if item == nil {
			continue
		}
		switch item.NameRule {
		case NameRuleExact:
			if item.ModelName == runtimeModelName {
				matched = append(matched, item)
			}
		case NameRulePrefix:
			if strings.HasPrefix(runtimeModelName, item.ModelName) {
				matched = append(matched, item)
			}
		case NameRuleSuffix:
			if strings.HasSuffix(runtimeModelName, item.ModelName) {
				matched = append(matched, item)
			}
		case NameRuleContains:
			if strings.Contains(runtimeModelName, item.ModelName) {
				matched = append(matched, item)
			}
		}
	}

	return matched, nil
}
