package configstore

import (
	"context"
	"testing"

	"github.com/neria-cloud/meridian-base/framework/configstore/tables"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserGovernanceMigrationMRN(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "upgrade"}[upgrade], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			require.NoError(t, triggerMigrations(context.Background(), db, testMigrationLogger))
			if upgrade {
				require.NoError(t, db.Exec("DROP INDEX idx_governance_virtual_keys_user_id").Error)
				require.NoError(t, db.Exec("DROP INDEX idx_governance_budgets_user_id").Error)
				require.NoError(t, db.Exec("ALTER TABLE governance_virtual_keys DROP COLUMN user_id").Error)
				require.NoError(t, db.Exec("ALTER TABLE governance_budgets DROP COLUMN user_id").Error)
				require.NoError(t, db.Exec("ALTER TABLE governance_budgets DROP COLUMN calendar_aligned_override").Error)
				require.NoError(t, db.Exec("DELETE FROM migrations WHERE id = ?", "mrn_add_user_governance_columns").Error)
			}
			for i := 0; i < 2; i++ {
				require.NoError(t, triggerMigrations(context.Background(), db, testMigrationLogger))
				require.True(t, db.Migrator().HasColumn("governance_virtual_keys", "user_id"))
				require.True(t, db.Migrator().HasColumn("governance_budgets", "user_id"))
				require.True(t, db.Migrator().HasColumn("governance_budgets", "calendar_aligned_override"))
				require.True(t, db.Migrator().HasIndex("governance_virtual_keys", "idx_governance_virtual_keys_user_id"))
				require.True(t, db.Migrator().HasIndex("governance_budgets", "idx_governance_budgets_user_id"))
			}
		})
	}
}

func TestUserGovernanceBudgetAndHashesMRN(t *testing.T) {
	user, empty, customer := "user-1", "", "customer-1"
	budget := tables.TableBudget{ID: "budget", MaxLimit: 10, ResetDuration: "1d"}
	before, err := GenerateBudgetHash(budget)
	require.NoError(t, err)
	require.Equal(t, "c31e2d947abb3d044f21b3ea259d8e723689a1af0153e780aeca1b16df420158", before)
	vk := tables.TableVirtualKey{Name: "key"}
	vkBefore, err := GenerateVirtualKeyHash(vk)
	require.NoError(t, err)
	require.Equal(t, "667cb7f06bdc8ba2693fb30d6f8cfe16cf3708ab6bc148a597e4350b15fcfa96", vkBefore)
	budget.UserID = &user
	require.NoError(t, budget.BeforeSave(nil))
	after, err := GenerateBudgetHash(budget)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
	budget.CalendarAlignedOverride = new(false)
	aligned, err := GenerateBudgetHash(budget)
	require.NoError(t, err)
	require.NotEqual(t, after, aligned)
	budget.CalendarAlignedOverride = new(true)
	alignedTrue, err := GenerateBudgetHash(budget)
	require.NoError(t, err)
	require.NotEqual(t, aligned, alignedTrue)
	budget.CustomerID = &customer
	require.Error(t, budget.BeforeSave(nil))
	budget.CustomerID = nil
	budget.UserID = &empty
	require.Error(t, budget.BeforeSave(nil))
	vk.UserID = &user
	vkAfter, err := GenerateVirtualKeyHash(vk)
	require.NoError(t, err)
	require.NotEqual(t, vkBefore, vkAfter)
}
