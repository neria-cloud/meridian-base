package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/neria-cloud/meridian-base/core/schemas"
	"github.com/neria-cloud/meridian-base/framework/configstore"
	"github.com/neria-cloud/meridian-base/framework/configstore/tables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userGovernanceLoggerMRN struct{ testLogger }

func (*userGovernanceLoggerMRN) Fatal(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

func TestUserGovernanceLoaderMRN(t *testing.T) {
	previous := logger
	logger = &userGovernanceLoggerMRN{}
	defer func() { logger = previous }()
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprint(present), func(t *testing.T) {
			ctx := context.Background()
			store := createTestSQLiteConfigStore(t, t.TempDir())
			db := store.DB()
			require.NoError(t, db.Exec("CREATE TABLE users (id varchar(255) PRIMARY KEY, rate_limit_id varchar(255))").Error)
			require.NoError(t, db.Exec("INSERT INTO users VALUES ('u1', 'user-rate')").Error)
			userID, vkID, modelID := "u1", "user-vk", "user-model"
			vk := tables.TableVirtualKey{ID: vkID, Name: vkID, Value: *schemas.NewSecretVar("sk-bf-user"), UserID: &userID}
			require.NoError(t, store.CreateVirtualKey(ctx, &vk))
			model := tables.TableModelConfig{ID: modelID, ModelName: "*", Scope: "virtual_key", ScopeID: &vkID}
			require.NoError(t, store.CreateModelConfig(ctx, &model))
			for _, b := range []tables.TableBudget{
				{ID: "user-budget", UserID: &userID, MaxLimit: 10, ResetDuration: "1d"},
				{ID: "model-budget", ModelConfigID: &modelID, MaxLimit: 10, ResetDuration: "1d"},
			} {
				require.NoError(t, store.CreateBudget(ctx, &b))
			}
			rate := tables.TableRateLimit{ID: "user-rate"}
			require.NoError(t, store.CreateRateLimit(ctx, &rate))
			ordinary := tables.TableVirtualKey{ID: "ordinary", Name: "ordinary", Value: *schemas.NewSecretVar("sk-bf-ordinary")}
			require.NoError(t, store.CreateVirtualKey(ctx, &ordinary))
			gov := &configstore.GovernanceConfig{}
			if present {
				gov.VirtualKeys = []tables.TableVirtualKey{}
				gov.ModelConfigs = []tables.TableModelConfig{}
				gov.Budgets = []tables.TableBudget{}
				gov.RateLimits = []tables.TableRateLimit{}
			}
			data := &ConfigData{SourceOfTruth: SourceOfTruthConfigJSON, Governance: gov}
			config := &Config{ConfigStore: store}
			loadGovernanceConfig(ctx, config, data)
			require.NotNil(t, config.GovernanceConfig)
			protected, err := collectProtectedUserGovernance(ctx, store, nil)
			require.NoError(t, err)
			require.True(t, protected.virtualKeys[vkID])
			require.True(t, protected.modelConfigs[modelID])
			require.True(t, protected.budgets["user-budget"])
			require.True(t, protected.budgets["model-budget"])
			require.True(t, protected.rateLimits["user-rate"])
			wantVKs := 2
			if present {
				wantVKs = 1
			}
			require.Len(t, config.GovernanceConfig.VirtualKeys, wantVKs)
			require.Len(t, config.GovernanceConfig.ModelConfigs, 1)
			require.Len(t, config.GovernanceConfig.Budgets, 2)
			require.Len(t, config.GovernanceConfig.RateLimits, 1)
			var count int64
			require.NoError(t, db.Model(&tables.TableVirtualKey{}).Count(&count).Error)
			require.Equal(t, int64(wantVKs), count)
			for _, collision := range []struct {
				id  string
				gov configstore.GovernanceConfig
			}{
				{vkID, configstore.GovernanceConfig{VirtualKeys: []tables.TableVirtualKey{{ID: vkID}}}},
				{modelID, configstore.GovernanceConfig{ModelConfigs: []tables.TableModelConfig{{ID: modelID}}}},
				{"user-budget", configstore.GovernanceConfig{Budgets: []tables.TableBudget{{ID: "user-budget"}}}},
				{"user-rate", configstore.GovernanceConfig{RateLimits: []tables.TableRateLimit{{ID: "user-rate"}}}},
				{"new-user-vk", configstore.GovernanceConfig{VirtualKeys: []tables.TableVirtualKey{{ID: "new-user-vk", UserID: &userID}}}},
			} {
				func() {
					defer func() {
						r := recover()
						require.NotNil(t, r)
						require.True(t, strings.Contains(fmt.Sprint(r), collision.id), fmt.Sprint(r))
					}()
					loadGovernanceConfig(ctx, &Config{ConfigStore: store}, &ConfigData{SourceOfTruth: SourceOfTruthConfigJSON, Governance: &collision.gov})
				}()
			}
		})
	}
}

func TestUserGovernanceLoaderBeforeOverlayMRN(t *testing.T) {
	store := createTestSQLiteConfigStore(t, t.TempDir())
	_, err := collectProtectedUserGovernance(context.Background(), store, nil)
	require.NoError(t, err)
	require.NoError(t, store.DB().Exec("CREATE TABLE users (id varchar(255) PRIMARY KEY)").Error)
	_, err = collectProtectedUserGovernance(context.Background(), store, nil)
	require.NoError(t, err)
}

func TestUserGovernanceLoaderRejectsVirtualKeyNameCollisionMRN(t *testing.T) {
	previous := logger
	logger = &userGovernanceLoggerMRN{}
	defer func() { logger = previous }()
	for _, source := range []string{SourceOfTruthConfigJSON, SourceOfTruthSplit} {
		for _, idField := range []string{``, `"id":"",`, `"id":"different-id",`} {
			t.Run(source+"/"+idField, func(t *testing.T) {
				ctx := context.Background()
				store := createTestSQLiteConfigStore(t, t.TempDir())
				userID := "u1"
				vk := tables.TableVirtualKey{ID: "protected-id", Name: "protected-name", Value: *schemas.NewSecretVar("sk-bf-original"), UserID: &userID, IsActive: new(true)}
				require.NoError(t, store.CreateVirtualKey(ctx, &vk))
				before, err := store.GetVirtualKey(ctx, vk.ID)
				require.NoError(t, err)
				var file configstore.GovernanceConfig
				require.NoError(t, json.Unmarshal([]byte(`{"virtual_keys":[{`+idField+`"name":"protected-name","value":"sk-bf-replaced","is_active":false}]}`), &file))
				var failure any
				func() {
					defer func() { failure = recover() }()
					loadGovernanceConfig(ctx, &Config{ConfigStore: store}, &ConfigData{SourceOfTruth: source, Governance: &file})
				}()
				assert.Equal(t, `invalid user governance configuration: config virtual key name "protected-name" resolves to protected user governance ID "protected-id"`, failure)
				after, err := store.GetVirtualKey(ctx, vk.ID)
				require.NoError(t, err)
				assert.Equal(t, before.Value.GetValue(), after.Value.GetValue())
				assert.Equal(t, before.IsActiveValue(), after.IsActiveValue())
				assert.Equal(t, before.UserID, after.UserID)
			})
		}
	}
}
