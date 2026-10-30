package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"subtrackr/internal/config"
	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
	"subtrackr/internal/version"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg := config.Load()

	db, err := database.Initialize(cfg.DatabasePath)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	if err := database.RunMigrations(db); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	subscriptionRepo := repository.NewSubscriptionRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	exchangeRateRepo := repository.NewExchangeRateRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	categoryService := service.NewCategoryService(categoryRepo)
	subscriptionService := service.NewSubscriptionService(subscriptionRepo, categoryService)
	settingsService := service.NewSettingsService(settingsRepo)
	currencyService := service.NewCurrencyService(exchangeRateRepo, settingsRepo)

	server := mcp.NewServer(
		&mcp.Implementation{Name: "subtrackr", Version: version.GetVersion()},
		nil,
	)

	// list_subscriptions
	type ListInput struct{}
	type ListOutput struct {
		Subscriptions []models.Subscription `json:"subscriptions"`
		Count         int                   `json:"count"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_subscriptions",
		Description: "List all subscriptions",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ListInput) (*mcp.CallToolResult, ListOutput, error) {
		subs, err := subscriptionService.GetAll()
		if err != nil {
			return nil, ListOutput{}, err
		}
		return nil, ListOutput{Subscriptions: subs, Count: len(subs)}, nil
	})

	// get_subscription
	type GetInput struct {
		ID uint `json:"id" jsonschema:"the subscription ID to retrieve"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_subscription",
		Description: "Get a subscription by ID",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input GetInput) (*mcp.CallToolResult, *models.Subscription, error) {
		sub, err := subscriptionService.GetByID(input.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("subscription not found: %w", err)
		}
		return nil, sub, nil
	})

	// create_subscription
	type CreateInput struct {
		Name             string  `json:"name" jsonschema:"the subscription name"`
		Cost             float64 `json:"cost" jsonschema:"the subscription cost; 0 or more"`
		Schedule         string  `json:"schedule" jsonschema:"billing schedule: Monthly, Annual, Weekly, Daily, or Quarterly"`
		Status           string  `json:"status,omitempty" jsonschema:"subscription status: Active, Cancelled, Paused, or Trial; defaults to Active"`
		OriginalCurrency string  `json:"original_currency,omitempty" jsonschema:"currency code e.g. USD, EUR; defaults to the configured currency"`
		PaymentMethod    string  `json:"payment_method,omitempty" jsonschema:"payment method"`
		Autopay          *bool   `json:"autopay,omitempty" jsonschema:"whether payment is charged automatically; omit when unknown"`
		Account          string  `json:"account,omitempty" jsonschema:"account identifier"`
		URL              string  `json:"url,omitempty" jsonschema:"subscription URL"`
		Notes            string  `json:"notes,omitempty" jsonschema:"additional notes"`
		StartDate        string  `json:"start_date,omitempty" jsonschema:"start date in YYYY-MM-DD format"`
		RenewalDate      string  `json:"renewal_date,omitempty" jsonschema:"renewal date in YYYY-MM-DD format"`
		CategoryID       uint    `json:"category_id,omitempty" jsonschema:"category ID; omit or 0 for uncategorized"`

		CancellationNoticeDays int `json:"cancellation_notice_days,omitempty" jsonschema:"days of cancellation notice the provider requires before renewal; renewal reminders count down to renewal date minus this; 0 = none"`

		ToCancel             *bool `json:"to_cancel,omitempty" jsonschema:"remind the user to cancel before the deadline instead of sending renewal reminders; applies to Active and Trial subscriptions; defaults to false"`
		ToCancelReminderDays *int  `json:"to_cancel_reminder_days,omitempty" jsonschema:"days before the deadline to start the to-cancel reminder: 3, 5, or 14; defaults to 3"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_subscription",
		Description: "Create a new subscription",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input CreateInput) (*mcp.CallToolResult, *models.Subscription, error) {
		sub := &models.Subscription{
			Name:             input.Name,
			Cost:             input.Cost,
			Schedule:         input.Schedule,
			Status:           input.Status,
			OriginalCurrency: input.OriginalCurrency,
			PaymentMethod:    input.PaymentMethod,
			Autopay:          input.Autopay,
			Account:          input.Account,
			URL:              input.URL,
			Notes:            input.Notes,
			CategoryID:       input.CategoryID,
			ReminderEnabled:  true,

			CancellationNoticeDays: models.ClampCancellationNoticeDays(input.CancellationNoticeDays),
		}
		if sub.Status == "" {
			sub.Status = "Active"
		}
		if input.ToCancel != nil {
			sub.ToCancel = *input.ToCancel
		}
		if input.ToCancelReminderDays != nil {
			if err := validateToCancelReminderDays(*input.ToCancelReminderDays); err != nil {
				return nil, nil, err
			}
			sub.ToCancelReminderDays = *input.ToCancelReminderDays
		}
		if sub.OriginalCurrency == "" {
			sub.OriginalCurrency = settingsService.GetCurrency()
		}
		for _, err := range []error{
			validateName(sub.Name), validateCost(sub.Cost), validateSchedule(sub.Schedule), validateStatus(sub.Status),
		} {
			if err != nil {
				return nil, nil, err
			}
		}
		var err error
		if sub.StartDate, err = parseOptionalDate("start_date", input.StartDate); err != nil {
			return nil, nil, err
		}
		if sub.RenewalDate, err = parseOptionalDate("renewal_date", input.RenewalDate); err != nil {
			return nil, nil, err
		}
		created, err := subscriptionService.Create(sub)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create subscription: %w", err)
		}
		return nil, created, nil
	})

	// update_subscription
	type UpdateInput struct {
		ID               uint    `json:"id" jsonschema:"the subscription ID to update"`
		Name             string  `json:"name,omitempty" jsonschema:"new name"`
		Cost             float64 `json:"cost,omitempty" jsonschema:"new cost; 0 or more"`
		Schedule         string  `json:"schedule,omitempty" jsonschema:"new schedule: Monthly, Annual, Weekly, Daily, or Quarterly"`
		Status           string  `json:"status,omitempty" jsonschema:"new status: Active, Cancelled, Paused, or Trial"`
		OriginalCurrency string  `json:"original_currency,omitempty" jsonschema:"new currency code; empty resets it to the configured currency"`
		PaymentMethod    string  `json:"payment_method,omitempty" jsonschema:"new payment method; empty clears it"`
		Autopay          *bool   `json:"autopay,omitempty" jsonschema:"new autopay state; null means unknown"`
		Account          string  `json:"account,omitempty" jsonschema:"new account; empty clears it"`
		URL              string  `json:"url,omitempty" jsonschema:"new URL; empty clears it"`
		Notes            string  `json:"notes,omitempty" jsonschema:"new notes; empty clears them"`
		StartDate        string  `json:"start_date,omitempty" jsonschema:"new start date in YYYY-MM-DD format; empty clears it"`
		RenewalDate      string  `json:"renewal_date,omitempty" jsonschema:"new renewal date in YYYY-MM-DD format; empty clears it, and an Active subscription then gets a recalculated date"`
		CategoryID       uint    `json:"category_id,omitempty" jsonschema:"new category ID; 0 makes it uncategorized"`

		CancellationNoticeDays int `json:"cancellation_notice_days,omitempty" jsonschema:"new cancellation notice period in days; 0 clears it"`

		ToCancel             *bool `json:"to_cancel,omitempty" jsonschema:"whether to remind the user to cancel before the deadline instead of sending renewal reminders; setting status to Cancelled clears it"`
		ToCancelReminderDays *int  `json:"to_cancel_reminder_days,omitempty" jsonschema:"days before the deadline to start the to-cancel reminder: 3, 5, or 14"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_subscription",
		Description: "Update an existing subscription. Only the fields sent change; omitted fields keep their values.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input UpdateInput) (*mcp.CallToolResult, *models.Subscription, error) {
		// Get existing subscription to merge fields
		existing, err := subscriptionService.GetByID(input.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("subscription not found: %w", err)
		}

		// Detect which fields were explicitly provided via raw JSON
		var provided map[string]json.RawMessage
		if err := json.Unmarshal(req.Params.Arguments, &provided); err != nil {
			return nil, nil, fmt.Errorf("invalid arguments: %w", err)
		}

		if _, ok := provided["name"]; ok {
			if err := validateName(input.Name); err != nil {
				return nil, nil, err
			}
			existing.Name = input.Name
		}
		if _, ok := provided["cost"]; ok {
			if err := validateCost(input.Cost); err != nil {
				return nil, nil, err
			}
			existing.Cost = input.Cost
		}
		if _, ok := provided["schedule"]; ok {
			if err := validateSchedule(input.Schedule); err != nil {
				return nil, nil, err
			}
			existing.Schedule = input.Schedule
		}
		if _, ok := provided["status"]; ok {
			if err := validateStatus(input.Status); err != nil {
				return nil, nil, err
			}
			existing.Status = input.Status
		}
		if _, ok := provided["original_currency"]; ok {
			if input.OriginalCurrency == "" {
				existing.OriginalCurrency = settingsService.GetCurrency()
			} else {
				existing.OriginalCurrency = input.OriginalCurrency
			}
		}
		if _, ok := provided["payment_method"]; ok {
			existing.PaymentMethod = input.PaymentMethod
		}
		if _, ok := provided["autopay"]; ok {
			existing.Autopay = input.Autopay
		}
		if _, ok := provided["account"]; ok {
			existing.Account = input.Account
		}
		if _, ok := provided["url"]; ok {
			existing.URL = input.URL
		}
		if _, ok := provided["notes"]; ok {
			existing.Notes = input.Notes
		}
		if _, ok := provided["category_id"]; ok {
			existing.CategoryID = input.CategoryID
		}
		if _, ok := provided["cancellation_notice_days"]; ok {
			if clamped := models.ClampCancellationNoticeDays(input.CancellationNoticeDays); clamped != existing.CancellationNoticeDays {
				existing.CancellationNoticeDays = clamped
				// The reminder anchor moved: clear renewal-reminder dedup state so
				// the windows for the new anchor can fire this cycle.
				existing.LastReminderSent = nil
				existing.LastReminderRenewalDate = nil
				existing.LastReminderWindow = -1
			}
		}
		if input.ToCancel != nil {
			existing.ToCancel = *input.ToCancel
		}
		if input.ToCancelReminderDays != nil {
			if err := validateToCancelReminderDays(*input.ToCancelReminderDays); err != nil {
				return nil, nil, err
			}
			existing.ToCancelReminderDays = *input.ToCancelReminderDays
		}
		if _, ok := provided["start_date"]; ok {
			if existing.StartDate, err = parseOptionalDate("start_date", input.StartDate); err != nil {
				return nil, nil, err
			}
		}
		if _, ok := provided["renewal_date"]; ok {
			if existing.RenewalDate, err = parseOptionalDate("renewal_date", input.RenewalDate); err != nil {
				return nil, nil, err
			}
		}

		updated, err := subscriptionService.Update(input.ID, existing)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to update subscription: %w", err)
		}
		return nil, updated, nil
	})

	// delete_subscription
	type DeleteInput struct {
		ID uint `json:"id" jsonschema:"the subscription ID to delete"`
	}
	type DeleteOutput struct {
		Message string `json:"message"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_subscription",
		Description: "Delete a subscription by ID",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, DeleteOutput, error) {
		if _, err := subscriptionService.GetByID(input.ID); err != nil {
			return nil, DeleteOutput{}, fmt.Errorf("subscription not found: %w", err)
		}
		if err := subscriptionService.Delete(input.ID); err != nil {
			return nil, DeleteOutput{}, fmt.Errorf("failed to delete subscription: %w", err)
		}
		return nil, DeleteOutput{Message: "Subscription " + strconv.Itoa(int(input.ID)) + " deleted"}, nil
	})

	// get_stats
	type StatsInput struct{}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stats",
		Description: "Get subscription statistics including total spending, counts, and category breakdown",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input StatsInput) (*mcp.CallToolResult, *models.Stats, error) {
		stats, err := subscriptionService.GetStats(currencyService, settingsService.GetCurrency())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get stats: %w", err)
		}
		return nil, stats, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name must not be empty")
	}
	return nil
}

func validateCost(cost float64) error {
	if cost < 0 {
		return errors.New("cost must not be negative")
	}
	return nil
}

func validateSchedule(schedule string) error {
	switch schedule {
	case "Monthly", "Annual", "Weekly", "Daily", "Quarterly":
		return nil
	}
	return fmt.Errorf("invalid schedule %q: must be Monthly, Annual, Weekly, Daily, or Quarterly", schedule)
}

func validateStatus(status string) error {
	switch status {
	case "Active", "Cancelled", "Paused", "Trial":
		return nil
	}
	return fmt.Errorf("invalid status %q: must be Active, Cancelled, Paused, or Trial", status)
}

func validateToCancelReminderDays(days int) error {
	if models.ValidToCancelReminderDays(days) {
		return nil
	}
	return fmt.Errorf("invalid to_cancel_reminder_days %d: must be 3, 5, or 14", days)
}

// parseOptionalDate parses a YYYY-MM-DD date. An empty value means no date.
func parseOptionalDate(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q: must be YYYY-MM-DD", field, value)
	}
	return &t, nil
}
