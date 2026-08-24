//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

func TestMemoryRepoCRUD(t *testing.T) {
	ctx := context.Background()
	r := env.mem

	// Save
	in := newMemory("deployed service A to staging")
	saved, err := r.Save(ctx, in)
	require.NoError(t, err)
	assert.NotEqual(t, saved.ID.String(), "00000000-0000-0000-0000-000000000000")
	assert.Equal(t, domain.MemoryTypeEpisodic, saved.Type)
	assert.Equal(t, []string{"integration"}, saved.Tags)
	assert.Equal(t, 1, saved.Version)
	assert.False(t, saved.CreatedAt.IsZero())

	// GetByID
	got, err := r.GetByID(ctx, saved.ID)
	require.NoError(t, err)
	assert.Equal(t, saved.ID, got.ID)
	assert.Equal(t, "deployed service A to staging", got.Content)
	assert.Nil(t, got.DeletedAt)

	// List with filter (tag)
	page, err := r.List(ctx, port.MemoryFilter{
		Tags:      []string{"integration"},
		TagsMatch: port.TagsMatchAll,
		Limit:     50,
		Sort:      port.SortByCreatedAt,
		Order:     port.SortDesc,
	})
	require.NoError(t, err)
	require.NotEmpty(t, page.Items)
	found := false
	for _, m := range page.Items {
		if m.ID == saved.ID {
			found = true
		}
	}
	assert.True(t, found, "saved memory must appear in tag-filtered list")

	// Update with expectedVersion → supersedes into a new version row
	got.Content = "deployed service A to staging, v2"
	got.ContentFormat = domain.ContentFormatMarkdown
	updated, err := r.Update(ctx, got, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Version, "content change must bump version")
	assert.NotEqual(t, got.ID, updated.ID, "supersession mints a new row id")
	assert.NotNil(t, updated.ValidFrom)

	// old version still readable via GetByVersion
	old, err := r.GetByVersion(ctx, got.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "deployed service A to staging", old.Content)
	assert.NotNil(t, old.SupersededBy, "old row closed by supersession")

	// stale expectedVersion must conflict
	got2 := *updated
	got2.Content = "stale write"
	_, err = r.Update(ctx, &got2, 1)
	require.Error(t, err)
	var de *domain.Error
	if assert.ErrorAs(t, err, &de) {
		assert.Equal(t, domain.CodeVersionConflict, de.Code)
	}

	// SoftDelete
	require.NoError(t, r.SoftDelete(ctx, updated.ID))
	_, err = r.GetByID(ctx, updated.ID)
	require.Error(t, err, "soft-deleted memory must not be returned")

	// verify deleted_at set on the row
	var deletedAt *time.Time
	require.NoError(t, env.pool.QueryRow(ctx,
		`SELECT deleted_at FROM memories WHERE id = $1`, updated.ID).Scan(&deletedAt))
	assert.NotNil(t, deletedAt, "soft delete sets deleted_at")
}
