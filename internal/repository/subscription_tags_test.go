package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/models"
)

// Create ignores Subscription.Tags. A tag id from a backup or another
// instance can belong to a different tag here, so tags are only ever attached
// by name, after creation.
func TestCreateDoesNotLinkTagsByID(t *testing.T) {
	db := setupIntegrityTestDB(t)
	subscriptions := NewSubscriptionRepository(db)
	tags := NewTagRepository(db)

	personal, err := tags.FindOrCreateByNames([]string{"personal"})
	require.NoError(t, err)

	sub := newUncategorized("Netflix")
	sub.Tags = []models.Tag{
		{ID: personal[0].ID, Name: "work"}, // an id that means "personal" here
		{Name: "streaming"},                // no id: GORM would insert it
	}
	created, err := subscriptions.Create(sub)
	require.NoError(t, err)

	var links int64
	require.NoError(t, db.Table("subscription_tags").Where("subscription_id = ?", created.ID).Count(&links).Error)
	assert.Zero(t, links, "Create must not attach tags")

	var all []models.Tag
	require.NoError(t, db.Order("id").Find(&all).Error)
	require.Len(t, all, 1, "Create must not insert tags")
	assert.Equal(t, "personal", all[0].Name, "Create must not rename an existing tag")
}
