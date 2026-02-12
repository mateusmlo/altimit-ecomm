package repository

import (
	"context"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/mateusmlo/altimit-ecomm/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIdempotencyCreate(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewIdempotencyRepository(testDB)
	ctx := context.Background()

	key := newTestIdempotencyKey()

	err := repo.Create(ctx, key)
	require.NoError(t, err)

	fetched, err := repo.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	assert.Equal(t, key.Key, fetched.Key)
	assert.JSONEq(t, string(key.Response), string(fetched.Response))
}

func TestIdempotencyGetByKey_NotFound(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewIdempotencyRepository(testDB)
	ctx := context.Background()

	_, err := repo.GetByKey(ctx, "nonexistent-key")

	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestIdempotencyExists_True(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewIdempotencyRepository(testDB)
	ctx := context.Background()

	key := newTestIdempotencyKey()
	require.NoError(t, repo.Create(ctx, key))

	exists, err := repo.Exists(ctx, key.Key)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestIdempotencyExists_False(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewIdempotencyRepository(testDB)
	ctx := context.Background()

	exists, err := repo.Exists(ctx, "nonexistent-key")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestIdempotencyDelete(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewIdempotencyRepository(testDB)
	ctx := context.Background()

	key := newTestIdempotencyKey()
	require.NoError(t, repo.Create(ctx, key))

	err := repo.Delete(ctx, key.Key)
	require.NoError(t, err)

	exists, err := repo.Exists(ctx, key.Key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestIdempotencyDeleteOlderThan(t *testing.T) {
	cleanupTables(t, testDB)
	repo := NewIdempotencyRepository(testDB)
	ctx := context.Background()

	now := time.Now().UTC()

	// Insert directly with controlled timestamps
	keys := []models.IdempotencyKey{
		{Key: "recent", Response: sonic.NoCopyRawMessage(`{}`), CreatedAt: now.Add(-1 * time.Hour)},
		{Key: "old", Response: sonic.NoCopyRawMessage(`{}`), CreatedAt: now.Add(-25 * time.Hour)},
		{Key: "very-old", Response: sonic.NoCopyRawMessage(`{}`), CreatedAt: now.Add(-49 * time.Hour)},
	}
	for i := range keys {
		require.NoError(t, testDB.Create(&keys[i]).Error)
	}

	// Delete keys older than 24 hours
	err := repo.DeleteOlderThan(ctx, 24*time.Hour)
	require.NoError(t, err)

	// Only "recent" should remain
	exists, err := repo.Exists(ctx, "recent")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = repo.Exists(ctx, "old")
	require.NoError(t, err)
	assert.False(t, exists)

	exists, err = repo.Exists(ctx, "very-old")
	require.NoError(t, err)
	assert.False(t, exists)
}
