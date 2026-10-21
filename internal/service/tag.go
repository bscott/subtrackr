package service

import (
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

type TagService struct {
	repo *repository.TagRepository
}

func NewTagService(repo *repository.TagRepository) *TagService {
	return &TagService{repo: repo}
}

// ParseTagsInput splits a comma-separated tag string into individual tag names.
// Returns trimmed, non-empty names; case-insensitive deduplication happens
// downstream in FindOrCreateByNames.
func ParseTagsInput(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// SetSubscriptionTags resolves names → tag rows (creating new ones) and attaches them to the subscription.
func (s *TagService) SetSubscriptionTags(subscriptionID uint, tagNames []string) ([]models.Tag, error) {
	tags, err := s.repo.FindOrCreateByNames(tagNames)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceSubscriptionTags(subscriptionID, tags); err != nil {
		return nil, err
	}
	// Best-effort cleanup of any tags that became orphaned.
	_ = s.repo.DeleteOrphaned()
	return tags, nil
}
