package service

import (
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

type SubscriptionService struct {
	repo            *repository.SubscriptionRepository
	categoryService *CategoryService
}

func NewSubscriptionService(repo *repository.SubscriptionRepository, categoryService *CategoryService) *SubscriptionService {
	return &SubscriptionService{repo: repo, categoryService: categoryService}
}

func (s *SubscriptionService) Create(subscription *models.Subscription) (*models.Subscription, error) {
	return s.repo.Create(subscription)
}

func (s *SubscriptionService) GetAll() ([]models.Subscription, error) {
	return s.repo.GetAll()
}

func (s *SubscriptionService) GetAllSorted(sortBy, order string) ([]models.Subscription, error) {
	return s.repo.GetAllSorted(sortBy, order)
}

func (s *SubscriptionService) GetByID(id uint) (*models.Subscription, error) {
	return s.repo.GetByID(id)
}

func (s *SubscriptionService) Update(id uint, subscription *models.Subscription) (*models.Subscription, error) {
	return s.repo.Update(id, subscription)
}

func (s *SubscriptionService) Delete(id uint) error {
	return s.repo.Delete(id)
}

func (s *SubscriptionService) Count() int64 {
	return s.repo.Count()
}

func (s *SubscriptionService) GetStats() (*models.Stats, error) {
	activeSubscriptions, err := s.repo.GetActiveSubscriptions()
	if err != nil {
		return nil, err
	}

	cancelledSubscriptions, err := s.repo.GetCancelledSubscriptions()
	if err != nil {
		return nil, err
	}

	upcomingRenewals, err := s.repo.GetUpcomingRenewals(7)
	if err != nil {
		return nil, err
	}

	categoryStats, err := s.repo.GetCategoryStats()
	if err != nil {
		return nil, err
	}

	stats := &models.Stats{
		ActiveSubscriptions:    len(activeSubscriptions),
		CancelledSubscriptions: len(cancelledSubscriptions),
		UpcomingRenewals:       len(upcomingRenewals),
		CategorySpending:       make(map[string]float64),
	}

	// Calculate totals
	for _, sub := range activeSubscriptions {
		stats.TotalMonthlySpend += sub.MonthlyCost()
		stats.TotalAnnualSpend += sub.AnnualCost()
	}

	// Calculate savings from cancelled subscriptions
	for _, sub := range cancelledSubscriptions {
		stats.TotalSaved += sub.AnnualCost()
		stats.MonthlySaved += sub.MonthlyCost()
	}

	// Build category spending map
	for _, cat := range categoryStats {
		stats.CategorySpending[cat.Category] = cat.Amount
	}

	return stats, nil
}

func (s *SubscriptionService) GetAllCategories() ([]models.Category, error) {
	return s.categoryService.GetAll()
}
