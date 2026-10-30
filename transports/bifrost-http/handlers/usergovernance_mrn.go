package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/neria-cloud/meridian-base/framework/configstore"
	"github.com/neria-cloud/meridian-base/framework/configstore/tables"
	"gorm.io/gorm"
	"strings"
)

func (r *UpdateVirtualKeyRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateVirtualKeyRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = UpdateVirtualKeyRequest(decoded)
	_, r.userIDSet = fields["user_id"]
	return nil
}

func validateUserOwnershipUpdate(vk *tables.TableVirtualKey, req *UpdateVirtualKeyRequest) error {
	if req.userIDSet && (req.UserID == nil || vk.UserID == nil || *req.UserID != *vk.UserID) {
		return fmt.Errorf("user_id is immutable")
	}
	if vk.UserID != nil && (optionalJSONStringHasValue(req.TeamID) || optionalJSONStringHasValue(req.CustomerID)) {
		return fmt.Errorf("user_id is exclusive with team_id/customer_id")
	}
	return nil
}

var errVirtualKeyUserNotFound = fmt.Errorf("virtual key owner: %w", configstore.ErrNotFound)

func lockVirtualKeyUser(ctx context.Context, tx *gorm.DB, id string) error {
	tx = tx.WithContext(ctx)
	if tx.Dialector.Name() == "sqlite" {
		if err := tx.Exec("UPDATE users SET id = id WHERE id = ?", id).Error; err != nil {
			return err
		}
	}
	var user struct{ ID string }
	if err := dbForUpdate(tx).Table("users").Select("id").Where("id = ?", id).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errVirtualKeyUserNotFound
		}
		return err
	}
	return nil
}

func executeUserVKTransaction(ctx context.Context, store configstore.ConfigStore, userID *string, fn func(*gorm.DB) error) error {
	for attempt := 0; ; attempt++ {
		err := store.ExecuteTransaction(ctx, fn)
		if userID == nil || attempt == 4 || err == nil || (!strings.Contains(err.Error(), "database is locked") && !strings.Contains(err.Error(), "database table is locked")) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
}
