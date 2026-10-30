package configstore

import (
	"context"
	"fmt"

	"github.com/neria-cloud/meridian-base/core/schemas"
	"github.com/neria-cloud/meridian-base/framework/migrator"
	"gorm.io/gorm"
)

func migrationAddUserGovernanceColumns(ctx context.Context, db *gorm.DB, logger schemas.Logger) error {
	const name = "mrn_add_user_governance_columns"
	logger.Info("[configstore] starting migration %s", name)
	m := migrator.New(db, migrator.DefaultOptions, []*migrator.Migration{{
		ID: name,
		Migrate: func(tx *gorm.DB) error {
			tx = tx.WithContext(ctx)
			for _, column := range []struct{ table, name, kind string }{
				{"governance_virtual_keys", "user_id", "varchar(255)"},
				{"governance_budgets", "user_id", "varchar(255)"},
				{"governance_budgets", "calendar_aligned_override", "boolean"},
			} {
				if !tx.Migrator().HasColumn(column.table, column.name) {
					if err := tx.Exec("ALTER TABLE " + column.table + " ADD COLUMN " + column.name + " " + column.kind).Error; err != nil {
						return err
					}
				}
			}
			for _, table := range []string{"governance_virtual_keys", "governance_budgets"} {
				index := "idx_" + table + "_user_id"
				if !tx.Migrator().HasIndex(table, index) {
					if err := tx.Exec("CREATE INDEX " + index + " ON " + table + "(user_id)").Error; err != nil {
						return err
					}
				}
			}
			return nil
		},
	}})
	if err := m.Migrate(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
