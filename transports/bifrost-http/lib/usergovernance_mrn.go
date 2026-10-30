package lib

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/neria-cloud/meridian-base/framework/configstore"
	"github.com/neria-cloud/meridian-base/framework/configstore/tables"
	"gorm.io/gorm"
)

type protectedUserGovernance struct {
	virtualKeys, modelConfigs, budgets, rateLimits map[string]bool
	virtualKeyNames                                map[string]string
}

func collectProtectedUserGovernance(ctx context.Context, store configstore.ConfigStore, tx *gorm.DB) (protectedUserGovernance, error) {
	p := protectedUserGovernance{
		virtualKeys: map[string]bool{}, modelConfigs: map[string]bool{},
		budgets: map[string]bool{}, rateLimits: map[string]bool{},
		virtualKeyNames: map[string]string{},
	}
	if store == nil || store.DB() == nil {
		return p, nil
	}
	if tx == nil {
		err := store.ExecuteTransaction(ctx, func(db *gorm.DB) error {
			var err error
			p, err = collectProtectedUserGovernance(ctx, store, db)
			return err
		})
		return p, err
	}
	tx = tx.WithContext(ctx)
	var vks []tables.TableVirtualKey
	if err := tx.Where("user_id IS NOT NULL").Find(&vks).Error; err != nil {
		return p, err
	}
	ids := make([]string, 0, len(vks))
	for _, vk := range vks {
		p.virtualKeys[vk.ID] = true
		p.virtualKeyNames[vk.Name] = vk.ID
		ids = append(ids, vk.ID)
		if vk.RateLimitID != nil {
			p.rateLimits[*vk.RateLimitID] = true
		}
	}
	var models []tables.TableModelConfig
	if len(ids) > 0 {
		if err := tx.Where("scope = ? AND scope_id IN ?", "virtual_key", ids).Find(&models).Error; err != nil {
			return p, err
		}
	}
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		p.modelConfigs[model.ID] = true
		modelIDs = append(modelIDs, model.ID)
		if model.RateLimitID != nil {
			p.rateLimits[*model.RateLimitID] = true
		}
		if model.BudgetID != nil {
			p.budgets[*model.BudgetID] = true
		}
	}
	var pcs []tables.TableVirtualKeyProviderConfig
	if len(ids) > 0 {
		if err := tx.Where("virtual_key_id IN ?", ids).Find(&pcs).Error; err != nil {
			return p, err
		}
	}
	pcIDs := make([]uint, 0, len(pcs))
	for _, pc := range pcs {
		pcIDs = append(pcIDs, pc.ID)
		if pc.RateLimitID != nil {
			p.rateLimits[*pc.RateLimitID] = true
		}
	}
	var budgetIDs []string
	if err := tx.Model(&tables.TableBudget{}).
		Where("user_id IS NOT NULL OR virtual_key_id IN ? OR model_config_id IN ? OR provider_config_id IN ?", ids, modelIDs, pcIDs).
		Pluck("id", &budgetIDs).Error; err != nil {
		return p, err
	}
	for _, id := range budgetIDs {
		p.budgets[id] = true
	}
	if tx.Migrator().HasTable("users") && tx.Migrator().HasColumn("users", "rate_limit_id") {
		var rateIDs []string
		if err := tx.Table("users").Where("rate_limit_id IS NOT NULL").Pluck("rate_limit_id", &rateIDs).Error; err != nil {
			return p, err
		}
		for _, id := range rateIDs {
			p.rateLimits[id] = true
		}
	}
	return p, nil
}

func validateUserGovernanceFile(ctx context.Context, config *Config, data *ConfigData) error {
	if data.Governance == nil {
		return nil
	}
	p, err := collectProtectedUserGovernance(ctx, config.ConfigStore, nil)
	if err != nil {
		return err
	}
	for _, vk := range data.Governance.VirtualKeys {
		if vk.UserID != nil {
			return fmt.Errorf("config virtual key %q cannot set user_id", vk.ID)
		}
		if id, protected := p.virtualKeyNames[vk.Name]; protected {
			return fmt.Errorf("config virtual key name %q resolves to protected user governance ID %q", vk.Name, id)
		}
	}
	raw, err := json.Marshal(data.Governance)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	var visit func(any, string) error
	visit = func(value any, kind string) error {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				if err := visit(child, kind); err != nil {
					return err
				}
			}
		case map[string]any:
			sets := map[string]map[string]bool{"virtual_keys": p.virtualKeys, "model_configs": p.modelConfigs, "budgets": p.budgets, "budget": p.budgets, "rate_limits": p.rateLimits, "rate_limit": p.rateLimits}
			id, _ := node["id"].(string)
			if sets[kind][id] {
				return fmt.Errorf("config %s ID %q is protected user governance", kind, id)
			}
			if (kind == "budgets" || kind == "budget") && node["user_id"] != nil {
				return fmt.Errorf("config budget %q cannot set user_id", id)
			}
			if kind == "model_configs" && node["scope"] == "virtual_key" {
				scopeID, _ := node["scope_id"].(string)
				if p.virtualKeys[scopeID] {
					return fmt.Errorf("config model config %q targets protected virtual key %q", id, scopeID)
				}
			}
			for key, child := range node {
				refs := map[string]map[string]bool{"budget_id": p.budgets, "budget_ids": p.budgets, "rate_limit_id": p.rateLimits, "virtual_key_id": p.virtualKeys, "model_config_id": p.modelConfigs}
				if set := refs[key]; set != nil {
					switch ref := child.(type) {
					case string:
						if set[ref] {
							return fmt.Errorf("config %s %q references protected ID %q", kind, id, ref)
						}
					case []any:
						for _, v := range ref {
							if s, ok := v.(string); ok && set[s] {
								return fmt.Errorf("config %s %q references protected ID %q", kind, id, s)
							}
						}
					}
				}
				if err := visit(child, key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value, "governance")
}

func retainUserGovernance[T any](file, current []T, protected map[string]bool, id func(T) string) []T {
	result := append([]T(nil), file...)
	for _, row := range current {
		if protected[id(row)] {
			result = append(result, row)
		}
	}
	return result
}
