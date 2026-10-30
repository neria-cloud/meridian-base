package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"

	"github.com/neria-cloud/meridian-base/core/schemas"
	"github.com/neria-cloud/meridian-base/framework/configstore"
	"github.com/neria-cloud/meridian-base/framework/configstore/tables"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

type userGovernanceManagerMRN struct {
	GovernanceManager
	store configstore.ConfigStore
}

func (m userGovernanceManagerMRN) ReloadVirtualKey(ctx context.Context, id string) (*tables.TableVirtualKey, error) {
	return m.store.GetVirtualKey(ctx, id)
}

func TestUserVirtualKeyHandlersMRN(t *testing.T) {
	SetLogger(&mockLogger{})
	ctx := context.Background()
	store, err := configstore.NewConfigStore(ctx, &configstore.Config{Enabled: true, Type: configstore.ConfigStoreTypeSQLite, Config: &configstore.SQLiteConfig{Path: filepath.Join(t.TempDir(), "config.db")}}, &mockLogger{})
	require.NoError(t, err)
	defer store.Close(ctx)
	require.NoError(t, store.DB().Exec("CREATE TABLE users (id varchar(255) PRIMARY KEY)").Error)
	require.NoError(t, store.DB().Exec("INSERT INTO users (id) VALUES ('u1'), ('u2')").Error)
	h := GovernanceHandler{configStore: store, governanceManager: userGovernanceManagerMRN{store: store}}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"name":"valid","user_id":"u1"}`, 200},
		{`{"name":"mixed","user_id":"u1","team_id":"team"}`, 400},
		{`{"name":"mixed2","user_id":"u1","customer_id":"customer"}`, 400},
		{`{"name":"empty","user_id":""}`, 400},
		{`{"name":"missing","user_id":"missing"}`, 404},
	} {
		var req fasthttp.RequestCtx
		req.Init(&fasthttp.Request{}, nil, nil)
		req.Request.SetBodyString(tc.body)
		h.createVirtualKey(&req)
		require.Equal(t, tc.status, req.Response.StatusCode(), string(req.Response.Body()))
	}
	vks, err := store.GetVirtualKeys(ctx)
	require.NoError(t, err)
	require.Len(t, vks, 1)
	require.Equal(t, "u1", *vks[0].UserID)
	vk := vks[0]
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{}`, 200}, {`{"user_id":"u1"}`, 200}, {`{"user_id":null}`, 400}, {`{"user_id":""}`, 400}, {`{"user_id":"u2"}`, 400},
		{`{"team_id":"team"}`, 400}, {`{"customer_id":"customer"}`, 400},
	} {
		var req fasthttp.RequestCtx
		req.Init(&fasthttp.Request{}, nil, nil)
		req.SetUserValue("vk_id", vk.ID)
		req.Request.SetBodyString(tc.body)
		h.updateVirtualKey(&req)
		require.Equal(t, tc.status, req.Response.StatusCode(), string(req.Response.Body()))
		persisted, err := store.GetVirtualKey(ctx, vk.ID)
		require.NoError(t, err)
		require.Equal(t, "u1", *persisted.UserID)
	}
	plain := tables.TableVirtualKey{ID: "plain", Name: "plain", Value: *schemas.NewSecretVar("plain-secret")}
	require.NoError(t, store.CreateVirtualKey(ctx, &plain))
	var req fasthttp.RequestCtx
	req.Init(&fasthttp.Request{}, nil, nil)
	req.SetUserValue("vk_id", plain.ID)
	req.Request.SetBodyString(`{"user_id":"u1"}`)
	h.updateVirtualKey(&req)
	require.Equal(t, 400, req.Response.StatusCode())
	var dto UpdateVirtualKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"user_id":null}`), &dto))
	require.True(t, dto.userIDSet)
}

type heldUserTransactionMRN struct {
	configstore.ConfigStore
	written chan struct{}
	release chan struct{}
}

func (s heldUserTransactionMRN) ExecuteTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return s.ConfigStore.ExecuteTransaction(ctx, func(tx *gorm.DB) error {
		if err := fn(tx); err != nil {
			return err
		}
		close(s.written)
		<-s.release
		return nil
	})
}

func TestUserVirtualKeyDeleteSerializationMRN(t *testing.T) {
	SetLogger(&mockLogger{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := configstore.NewConfigStore(ctx, &configstore.Config{Enabled: true, Type: configstore.ConfigStoreTypeSQLite, Config: &configstore.SQLiteConfig{Path: filepath.Join(t.TempDir(), "race.db")}}, &mockLogger{})
	require.NoError(t, err)
	defer store.Close(context.Background())
	require.NoError(t, store.DB().Exec("CREATE TABLE users (id varchar(255) PRIMARY KEY)").Error)
	require.NoError(t, store.DB().Exec("INSERT INTO users VALUES ('creator-first'), ('delete-first')").Error)
	held := heldUserTransactionMRN{ConfigStore: store, written: make(chan struct{}), release: make(chan struct{})}
	h := GovernanceHandler{configStore: held, governanceManager: userGovernanceManagerMRN{store: store}}
	created := make(chan int, 1)
	go func() {
		var req fasthttp.RequestCtx
		req.Init(&fasthttp.Request{}, nil, nil)
		req.Request.SetBodyString(`{"name":"race","user_id":"creator-first"}`)
		h.createVirtualKey(&req)
		created <- req.Response.StatusCode()
	}()
	select {
	case <-held.written:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	deleted := make(chan error, 1)
	denied := errors.New("user owns virtual keys")
	go func() {
		deleted <- store.ExecuteTransaction(ctx, func(tx *gorm.DB) error {
			if err := lockVirtualKeyUser(ctx, tx, "creator-first"); err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&tables.TableVirtualKey{}).Where("user_id = ?", "creator-first").Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return denied
			}
			return tx.Exec("DELETE FROM users WHERE id = ?", "creator-first").Error
		})
	}()
	close(held.release)
	require.Equal(t, 200, <-created)
	require.ErrorIs(t, <-deleted, denied)

	locked, release := make(chan struct{}), make(chan struct{})
	go func() {
		deleted <- store.ExecuteTransaction(ctx, func(tx *gorm.DB) error {
			if err := lockVirtualKeyUser(ctx, tx, "delete-first"); err != nil {
				return err
			}
			close(locked)
			<-release
			return tx.Exec("DELETE FROM users WHERE id = ?", "delete-first").Error
		})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	h.configStore = store
	go func() {
		var req fasthttp.RequestCtx
		req.Init(&fasthttp.Request{}, nil, nil)
		req.Request.SetBodyString(`{"name":"deleted","user_id":"delete-first"}`)
		h.createVirtualKey(&req)
		created <- req.Response.StatusCode()
	}()
	close(release)
	require.NoError(t, <-deleted)
	require.Equal(t, 404, <-created)
}
