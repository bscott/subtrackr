package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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
	categoryRepo := repository.NewCategoryRepository(db)
	categoryService := service.NewCategoryService(categoryRepo)
	subscriptionService := service.NewSubscriptionService(subscriptionRepo, categoryService)

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
		ID uint `json:"id" jsonschema:"required,the subscription ID to retrieve"`
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
		Name             string `json:"name" jsonschema:"required,the subscription name"`
		Cost             float64 `json:"cost" jsonschema:"required,the subscription cost"`
		Schedule         string `json:"schedule" jsonschema:"required,billing schedule: Monthly, Annual, Weekly, Daily, or Quarterly"`
		Status           string `json:"status" jsonschema:"subscription status: Active, Cancelled, Paused, or Trial"`
		OriginalCurrency string `json:"original_currency" jsonschema:"currency code e.g. USD, EUR"`
		PaymentMethod    string `json:"payment_method" jsonschema:"payment method"`
		Account          string `json:"account" jsonschema:"account identifier"`
		URL              string `json:"url" jsonschema:"subscription URL"`
		Notes            string `json:"notes" jsonschema:"additional notes"`
		StartDate        string `json:"start_date" jsonschema:"start date in YYYY-MM-DD format"`
		RenewalDate      string `json:"renewal_date" jsonschema:"renewal date in YYYY-MM-DD format"`
		CategoryID       uint   `json:"category_id" jsonschema:"category ID"`
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
			Account:          input.Account,
			URL:              input.URL,
			Notes:            input.Notes,
			CategoryID:       input.CategoryID,
		}
		if sub.Status == "" {
			sub.Status = "Active"
		}
		if sub.OriginalCurrency == "" {
			sub.OriginalCurrency = "USD"
		}
		if input.StartDate != "" {
			if t, err := time.Parse("2006-01-02", input.StartDate); err == nil {
				sub.StartDate = &t
			}
		}
		if input.RenewalDate != "" {
			if t, err := time.Parse("2006-01-02", input.RenewalDate); err == nil {
				sub.RenewalDate = &t
			}
		}
		created, err := subscriptionService.Create(sub)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create subscription: %w", err)
		}
		return nil, created, nil
	})

	// update_subscription
	type UpdateInput struct {
		ID               uint    `json:"id" jsonschema:"required,the subscription ID to update"`
		Name             string  `json:"name" jsonschema:"new name"`
		Cost             float64 `json:"cost" jsonschema:"new cost"`
		Schedule         string  `json:"schedule" jsonschema:"new schedule: Monthly, Annual, Weekly, Daily, or Quarterly"`
		Status           string  `json:"status" jsonschema:"new status: Active, Cancelled, Paused, or Trial"`
		OriginalCurrency string  `json:"original_currency" jsonschema:"new currency code"`
		PaymentMethod    string  `json:"payment_method" jsonschema:"new payment method"`
		Account          string  `json:"account" jsonschema:"new account"`
		URL              string  `json:"url" jsonschema:"new URL"`
		Notes            string  `json:"notes" jsonschema:"new notes"`
		StartDate        string  `json:"start_date" jsonschema:"new start date in YYYY-MM-DD format"`
		RenewalDate      string  `json:"renewal_date" jsonschema:"new renewal date in YYYY-MM-DD format"`
		CategoryID       uint    `json:"category_id" jsonschema:"new category ID"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_subscription",
		Description: "Update an existing subscription",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input UpdateInput) (*mcp.CallToolResult, *models.Subscription, error) {
		// Get existing subscription to merge fields
		existing, err := subscriptionService.GetByID(input.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("subscription not found: %w", err)
		}

		// Detect which fields were explicitly provided via raw JSON
		var provided map[string]json.RawMessage
		json.Unmarshal(req.Params.Arguments, &provided)

		if _, ok := provided["name"]; ok {
			existing.Name = input.Name
		}
		if _, ok := provided["cost"]; ok {
			existing.Cost = input.Cost
		}
		if _, ok := provided["schedule"]; ok {
			existing.Schedule = input.Schedule
		}
		if _, ok := provided["status"]; ok {
			existing.Status = input.Status
		}
		if _, ok := provided["original_currency"]; ok {
			existing.OriginalCurrency = input.OriginalCurrency
		}
		if _, ok := provided["payment_method"]; ok {
			existing.PaymentMethod = input.PaymentMethod
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
		if _, ok := provided["start_date"]; ok && input.StartDate != "" {
			if t, err := time.Parse("2006-01-02", input.StartDate); err == nil {
				existing.StartDate = &t
			}
		}
		if _, ok := provided["renewal_date"]; ok && input.RenewalDate != "" {
			if t, err := time.Parse("2006-01-02", input.RenewalDate); err == nil {
				existing.RenewalDate = &t
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
		ID uint `json:"id" jsonschema:"required,the subscription ID to delete"`
	}
	type DeleteOutput struct {
		Message string `json:"message"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_subscription",
		Description: "Delete a subscription by ID",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, DeleteOutput, error) {
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
		stats, err := subscriptionService.GetStats()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get stats: %w", err)
		}
		return nil, stats, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
