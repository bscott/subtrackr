package service

import (
	"sort"
	"strconv"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"time"
)

// ParseReminderWindows parses a comma-separated list of days-before values (e.g. "7,3,0")
// into a sorted, deduplicated descending list of positive integers. Invalid entries are skipped.
// If the input is empty and fallbackDays > 0, returns [fallbackDays] for backward compatibility.
func ParseReminderWindows(csv string, fallbackDays int) []int {
	seen := make(map[int]bool)
	out := make([]int, 0, 4)
	for _, part := range strings.Split(csv, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		v, err := strconv.Atoi(trimmed)
		if err != nil || v < 0 {
			continue
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) == 0 && fallbackDays > 0 {
		return []int{fallbackDays}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

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

// Duplicate creates a copy of an existing subscription with " (Copy)" appended to the name.
// Reminder-tracking fields (LastReminderSent, etc.) are intentionally not copied so the new
// subscription starts with a clean reminder state. Tag associations are preserved when
// tagService is provided (handler calls SetSubscriptionTags after Create).
func (s *SubscriptionService) Duplicate(id uint) (*models.Subscription, []string, error) {
	original, err := s.repo.GetByID(id)
	if err != nil {
		return nil, nil, err
	}

	tagNames := make([]string, len(original.Tags))
	for i, t := range original.Tags {
		tagNames[i] = t.Name
	}

	dup := *original
	dup.ID = 0
	dup.Name = original.Name + " (Copy)"
	dup.Tags = nil
	dup.LastReminderSent = nil
	dup.LastReminderRenewalDate = nil
	dup.LastReminderWindow = -1
	dup.LastCancellationReminderSent = nil
	dup.LastCancellationReminderDate = nil
	dup.LastCancellationReminderWindow = -1
	dup.CreatedAt = time.Time{}
	dup.UpdatedAt = time.Time{}

	created, err := s.repo.Create(&dup)
	if err != nil {
		return nil, nil, err
	}
	return created, tagNames, nil
}

func (s *SubscriptionService) Count() int64 {
	return s.repo.Count()
}

func (s *SubscriptionService) GetStats(currencyService *CurrencyService, displayCurrency string) (*models.Stats, error) {
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

	stats := &models.Stats{
		ActiveSubscriptions:        len(activeSubscriptions),
		CancelledSubscriptions:     len(cancelledSubscriptions),
		UpcomingRenewals:           len(upcomingRenewals),
		CategorySpending:           make(map[string]float64),
		ConversionComplete:         true,
		TotalsByCurrency:           make(map[string]models.CurrencyTotals),
		CategorySpendingByCurrency: make(map[string]map[string]float64),
	}

	currencies := make(map[string]struct{})
	for i := range activeSubscriptions {
		sub := &activeSubscriptions[i]
		currency := subscriptionCurrency(sub, displayCurrency)
		currencies[currency] = struct{}{}
		addActiveCurrencyTotals(stats, currency, sub)
	}
	for i := range cancelledSubscriptions {
		sub := &cancelledSubscriptions[i]
		currency := subscriptionCurrency(sub, displayCurrency)
		currencies[currency] = struct{}{}
		addCancelledCurrencyTotals(stats, currency, sub)
	}

	rates := make(map[string]float64, len(currencies))
	for currency := range currencies {
		rate, err := currencyService.GetExchangeRate(currency, displayCurrency)
		if err != nil {
			stats.ConversionComplete = false
			break
		}
		rates[currency] = rate
	}

	if !stats.ConversionComplete {
		return stats, nil
	}

	for i := range activeSubscriptions {
		sub := &activeSubscriptions[i]
		rate := rates[subscriptionCurrency(sub, displayCurrency)]
		stats.TotalMonthlySpend += sub.MonthlyCost() * rate
		stats.TotalAnnualSpend += sub.AnnualCost() * rate
		stats.CategorySpending[sub.Category.Name] += sub.MonthlyCost() * rate
	}
	for i := range cancelledSubscriptions {
		sub := &cancelledSubscriptions[i]
		rate := rates[subscriptionCurrency(sub, displayCurrency)]
		stats.TotalSaved += sub.AnnualCost() * rate
		stats.MonthlySaved += sub.MonthlyCost() * rate
	}

	return stats, nil
}

func subscriptionCurrency(subscription *models.Subscription, displayCurrency string) string {
	if subscription.OriginalCurrency == "" {
		return displayCurrency
	}
	return subscription.OriginalCurrency
}

func addActiveCurrencyTotals(stats *models.Stats, currency string, subscription *models.Subscription) {
	totals := stats.TotalsByCurrency[currency]
	totals.TotalMonthlySpend += subscription.MonthlyCost()
	totals.TotalAnnualSpend += subscription.AnnualCost()
	totals.ActiveSubscriptions++
	stats.TotalsByCurrency[currency] = totals

	if stats.CategorySpendingByCurrency[subscription.Category.Name] == nil {
		stats.CategorySpendingByCurrency[subscription.Category.Name] = make(map[string]float64)
	}
	stats.CategorySpendingByCurrency[subscription.Category.Name][currency] += subscription.MonthlyCost()
}

func addCancelledCurrencyTotals(stats *models.Stats, currency string, subscription *models.Subscription) {
	totals := stats.TotalsByCurrency[currency]
	totals.TotalSaved += subscription.AnnualCost()
	totals.MonthlySaved += subscription.MonthlyCost()
	totals.CancelledSubscriptions++
	stats.TotalsByCurrency[currency] = totals
}

func (s *SubscriptionService) GetAllCategories() ([]models.Category, error) {
	return s.categoryService.GetAll()
}

// GetSubscriptionsNeedingReminders returns subscriptions that need renewal reminders.
// `windows` is a list of "days before renewal" thresholds (e.g. [7,3,0]). A subscription
// fires when daysUntil first crosses below the smallest matching window that hasn't yet
// fired for the current renewal date. Returns a map of subscription to days until renewal.
//
// For backward compatibility, callers passing a single integer should wrap it in []int{n}.
func (s *SubscriptionService) GetSubscriptionsNeedingReminders(windows []int) (map[*models.Subscription]int, error) {
	if len(windows) == 0 {
		return make(map[*models.Subscription]int), nil
	}

	maxWindow := 0
	for _, w := range windows {
		if w > maxWindow {
			maxWindow = w
		}
	}

	subscriptions, err := s.repo.GetUpcomingRenewals(maxWindow)
	if err != nil {
		return nil, err
	}

	// Sort windows ascending so we find the smallest matching window first
	sortedWindows := append([]int{}, windows...)
	sort.Ints(sortedWindows)

	result := make(map[*models.Subscription]int)
	for i := range subscriptions {
		sub := &subscriptions[i]
		if sub.RenewalDate == nil || !sub.ReminderEnabled {
			continue
		}

		daysUntil := int(time.Until(*sub.RenewalDate).Hours() / 24)
		if daysUntil < 0 || daysUntil > maxWindow {
			continue
		}

		// Find smallest configured window >= daysUntil
		matched := -1
		for _, w := range sortedWindows {
			if daysUntil <= w {
				matched = w
				break
			}
		}
		if matched < 0 {
			continue
		}

		// Skip if a reminder has already been fired for this exact renewal date and either:
		//   a) we don't know which window (legacy state where LastReminderWindow == -1) — preserve
		//      the historical "one reminder per renewal date" behavior, OR
		//   b) the same-or-smaller window has already been fired (multi-window state).
		sameRenewal := sub.LastReminderRenewalDate != nil &&
			sub.LastReminderRenewalDate.Equal(*sub.RenewalDate)
		if sameRenewal && sub.LastReminderSent != nil {
			if sub.LastReminderWindow < 0 || sub.LastReminderWindow <= matched {
				continue
			}
		}

		result[sub] = daysUntil
	}

	return result, nil
}

// GetSubscriptionsNeedingCancellationReminders returns subscriptions needing cancellation reminders.
// Same multi-window semantics as GetSubscriptionsNeedingReminders.
func (s *SubscriptionService) GetSubscriptionsNeedingCancellationReminders(windows []int) (map[*models.Subscription]int, error) {
	if len(windows) == 0 {
		return make(map[*models.Subscription]int), nil
	}

	maxWindow := 0
	for _, w := range windows {
		if w > maxWindow {
			maxWindow = w
		}
	}

	subscriptions, err := s.repo.GetUpcomingCancellations(maxWindow)
	if err != nil {
		return nil, err
	}

	sortedWindows := append([]int{}, windows...)
	sort.Ints(sortedWindows)

	result := make(map[*models.Subscription]int)
	for i := range subscriptions {
		sub := &subscriptions[i]
		if sub.CancellationDate == nil || !sub.ReminderEnabled {
			continue
		}

		daysUntil := int(time.Until(*sub.CancellationDate).Hours() / 24)
		if daysUntil < 0 || daysUntil > maxWindow {
			continue
		}

		matched := -1
		for _, w := range sortedWindows {
			if daysUntil <= w {
				matched = w
				break
			}
		}
		if matched < 0 {
			continue
		}

		sameDate := sub.LastCancellationReminderDate != nil &&
			sub.LastCancellationReminderDate.Equal(*sub.CancellationDate)
		if sameDate && sub.LastCancellationReminderSent != nil {
			if sub.LastCancellationReminderWindow < 0 || sub.LastCancellationReminderWindow <= matched {
				continue
			}
		}

		result[sub] = daysUntil
	}

	return result, nil
}
