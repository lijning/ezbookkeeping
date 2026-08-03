package datastore

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

func TestSyncImportRecordStructs(t *testing.T) {
	originalUserStore := Container.UserStore
	originalTokenStore := Container.TokenStore
	originalUserDataStore := Container.UserDataStore

	t.Cleanup(func() {
		Container.UserStore = originalUserStore
		Container.TokenStore = originalTokenStore
		Container.UserDataStore = originalUserDataStore
	})

	config := &settings.Config{
		DatabaseConfig: &settings.DatabaseConfig{
			DatabaseType: settings.Sqlite3DbType,
			DatabasePath: filepath.Join(t.TempDir(), "ezbookkeeping.db"),
		},
	}

	require.NoError(t, InitializeDataStore(config))
	require.NoError(t, Container.UserDataStore.SyncStructs(
		new(models.ImportBatch),
		new(models.ImportMatchGroup),
		new(models.ImportMatchGroupMember),
		new(models.ImportSourceRecord),
	))
}
