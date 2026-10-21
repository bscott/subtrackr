package repository

import (
	"strings"
	"subtrackr/internal/models"

	"gorm.io/gorm"
)

type TagRepository struct {
	db *gorm.DB
}

func NewTagRepository(db *gorm.DB) *TagRepository {
	return &TagRepository{db: db}
}

// FindOrCreateByNames takes a slice of raw tag names and returns the corresponding Tag rows,
// creating any that don't exist yet. Whitespace is trimmed and empty entries are dropped.
// Names are case-preserving but matched case-insensitively (lowercased for lookup).
func (r *TagRepository) FindOrCreateByNames(names []string) ([]models.Tag, error) {
	cleaned := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, n := range names {
		trimmed := strings.TrimSpace(n)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		cleaned = append(cleaned, trimmed)
	}

	if len(cleaned) == 0 {
		return []models.Tag{}, nil
	}

	tags := make([]models.Tag, 0, len(cleaned))
	for _, name := range cleaned {
		var tag models.Tag
		err := r.db.Where("LOWER(name) = LOWER(?)", name).First(&tag).Error
		if err == gorm.ErrRecordNotFound {
			tag = models.Tag{Name: name}
			if err := r.db.Create(&tag).Error; err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// ReplaceSubscriptionTags sets the tags associated with a subscription, replacing any prior set.
func (r *TagRepository) ReplaceSubscriptionTags(subscriptionID uint, tags []models.Tag) error {
	var sub models.Subscription
	if err := r.db.First(&sub, subscriptionID).Error; err != nil {
		return err
	}
	return r.db.Model(&sub).Association("Tags").Replace(tags)
}

// DeleteOrphaned removes tags not referenced by any subscription. Called after edits/deletes
// to keep the tag list tidy.
func (r *TagRepository) DeleteOrphaned() error {
	return r.db.Exec(`
		DELETE FROM tags WHERE id NOT IN (
			SELECT DISTINCT tag_id FROM subscription_tags
		)
	`).Error
}
