package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestMCPServerOverStdio builds the real subtrackr-mcp binary and drives it
// the way an MCP client would: over stdio with newline-delimited JSON-RPC.
// It checks the advertised tools and a full create/get/list/update/stats/delete
// cycle against a private SQLite database, including the error paths a client
// relies on (unknown ID, missing required input).
func TestMCPServerOverStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the MCP binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "subtrackr-mcp")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run(), "build cmd/mcp")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Subscriptions reference a category through an enforced foreign key and
	// the MCP server has no category tool, so seed one the way the web UI does.
	dbPath := filepath.Join(dir, "mcp.db")
	db, err := database.Initialize(dbPath)
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	category, err := repository.NewCategoryRepository(db).Create(&models.Category{Name: "MCP test"})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	var stderr lockedBuffer
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "DATABASE_PATH="+dbPath, "GIN_MODE=release")
	cmd.Stderr = &stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "subtrackr-e2e", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	require.NoError(t, err, "connect over stdio: %s", stderr.String())
	defer session.Close()

	require.Equal(t, "subtrackr", session.InitializeResult().ServerInfo.Name)

	// Tool catalogue.
	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		require.NotEmpty(t, tool.Description, "tool %s has a description", tool.Name)
		require.NotNil(t, tool.InputSchema, "tool %s has an input schema", tool.Name)
	}
	sort.Strings(names)
	require.Equal(t, []string{
		"create_subscription", "delete_subscription", "get_stats",
		"get_subscription", "list_subscriptions", "update_subscription",
	}, names)

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err, "call %s", name)
		return result
	}
	decode := func(result *mcp.CallToolResult, into any) {
		t.Helper()
		require.False(t, result.IsError, "tool reported an error: %s", textOf(result))
		raw, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, into))
	}
	type subscription struct {
		ID                     uint       `json:"id"`
		Name                   string     `json:"name"`
		Cost                   float64    `json:"cost"`
		Schedule               string     `json:"schedule"`
		Status                 string     `json:"status"`
		OriginalCurrency       string     `json:"original_currency"`
		PaymentMethod          string     `json:"payment_method"`
		Autopay                *bool      `json:"autopay"`
		Account                string     `json:"account"`
		URL                    string     `json:"url"`
		Notes                  string     `json:"notes"`
		StartDate              *time.Time `json:"start_date"`
		RenewalDate            *time.Time `json:"renewal_date"`
		CategoryID             uint       `json:"category_id"`
		CancellationNoticeDays int        `json:"cancellation_notice_days"`
		ReminderEnabled        bool       `json:"reminder_enabled"`
		ToCancel               bool       `json:"to_cancel"`
		ToCancelReminderDays   int        `json:"to_cancel_reminder_days"`
		Category               struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"category"`
	}
	callError := func(name string, args map[string]any) string {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return err.Error()
		}
		require.True(t, result.IsError, "%s %v must fail", name, args)
		return textOf(result)
	}

	// Empty database.
	var list struct {
		Subscriptions []subscription `json:"subscriptions"`
		Count         int            `json:"count"`
	}
	decode(call("list_subscriptions", map[string]any{}), &list)
	require.Equal(t, 0, list.Count)

	// Only the identifying and core fields are required.
	schemas := map[string]map[string]any{}
	for _, tool := range tools.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(raw, &schema))
		schemas[tool.Name] = schema
		properties, _ := schema["properties"].(map[string]any)
		for field, property := range properties {
			description, _ := property.(map[string]any)["description"].(string)
			require.NotEmpty(t, description, "%s.%s has a description", tool.Name, field)
			require.False(t, strings.HasPrefix(description, "required"), "%s.%s description leaks a tag prefix: %q", tool.Name, field, description)
		}
	}
	required := func(tool string) []any {
		fields, _ := schemas[tool]["required"].([]any)
		return fields
	}
	require.ElementsMatch(t, []any{"name", "cost", "schedule"}, required("create_subscription"))
	require.ElementsMatch(t, []any{"id"}, required("update_subscription"))
	require.ElementsMatch(t, []any{"id"}, required("get_subscription"))
	require.ElementsMatch(t, []any{"id"}, required("delete_subscription"))

	fullCreate := map[string]any{
		"name": "MCP Streaming", "cost": 15.99, "schedule": "Monthly", "status": "", "original_currency": "",
		"payment_method": "", "autopay": nil, "account": "", "url": "", "notes": "from mcp",
		"start_date": "", "renewal_date": "", "category_id": category.ID, "cancellation_notice_days": 0,
	}
	var created subscription
	decode(call("create_subscription", fullCreate), &created)
	require.NotZero(t, created.ID)
	require.Equal(t, "MCP Streaming", created.Name)
	require.Equal(t, "Active", created.Status, "status defaults to Active")
	require.Equal(t, "USD", created.OriginalCurrency, "currency defaults to the configured currency")
	require.True(t, created.ReminderEnabled, "reminders default on")

	// Missing required input is reported as an error, not a crash.
	noName := map[string]any{}
	for k, v := range fullCreate {
		noName[k] = v
	}
	delete(noName, "name")
	callError("create_subscription", noName)

	// Get and list.
	var fetched subscription
	decode(call("get_subscription", map[string]any{"id": created.ID}), &fetched)
	require.Equal(t, created.ID, fetched.ID)
	decode(call("list_subscriptions", map[string]any{}), &list)
	require.Equal(t, 1, list.Count)
	require.Len(t, list.Subscriptions, 1)

	// Update only the cost; the rest stays.
	var updated subscription
	decode(call("update_subscription", map[string]any{"id": created.ID, "cost": 19.99}), &updated)
	require.Equal(t, 19.99, updated.Cost)
	require.Equal(t, "MCP Streaming", updated.Name)
	require.Equal(t, "from mcp", updated.Notes)
	require.Equal(t, "Monthly", updated.Schedule)

	// Stats reflect the stored data.
	var stats struct {
		TotalMonthlySpend   float64 `json:"total_monthly_spend"`
		TotalAnnualSpend    float64 `json:"total_annual_spend"`
		ActiveSubscriptions int     `json:"active_subscriptions"`
	}
	decode(call("get_stats", map[string]any{}), &stats)
	require.Equal(t, 1, stats.ActiveSubscriptions)
	require.InDelta(t, 19.99, stats.TotalMonthlySpend, 0.01)
	require.InDelta(t, 19.99*12, stats.TotalAnnualSpend, 0.05)

	// Delete, then the record is gone and a second delete / get fails.
	var deleted struct {
		Message string `json:"message"`
	}
	decode(call("delete_subscription", map[string]any{"id": created.ID}), &deleted)
	require.Contains(t, deleted.Message, "deleted")
	gone := call("get_subscription", map[string]any{"id": created.ID})
	require.True(t, gone.IsError, "get after delete must fail: %s", textOf(gone))
	decode(call("list_subscriptions", map[string]any{}), &list)
	require.Equal(t, 0, list.Count)
	unknown := call("get_subscription", map[string]any{"id": 999999})
	require.True(t, unknown.IsError)

	again := call("delete_subscription", map[string]any{"id": created.ID})
	require.True(t, again.IsError, "second delete must fail: %s", textOf(again))

	// The MCP server has no category tool, so a client that sends no category gets
	// an uncategorized subscription: 0 over the API, NULL in the database.
	t.Run("create without a category", func(t *testing.T) {
		for name, args := range map[string]map[string]any{
			"omitted":    {"name": "MCP No Category", "cost": 1, "schedule": "Monthly"},
			"explicit 0": {"name": "MCP Zero Category", "cost": 1, "schedule": "Monthly", "category_id": 0},
		} {
			var sub, stored subscription
			decode(call("create_subscription", args), &sub)
			require.Zero(t, sub.CategoryID, name)
			decode(call("get_subscription", map[string]any{"id": sub.ID}), &stored)
			require.Zero(t, stored.CategoryID, name)
			require.Empty(t, stored.Category.Name, name)
			var nullRows int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM subscriptions WHERE id = ? AND category_id IS NULL", sub.ID).Scan(&nullRows).Error)
			require.EqualValues(t, 1, nullRows, name)
			call("delete_subscription", map[string]any{"id": sub.ID})
		}
	})

	t.Run("category that does not exist is rejected", func(t *testing.T) {
		require.Contains(t, callError("create_subscription", map[string]any{
			"name": "MCP Bad Category", "cost": 1, "schedule": "Monthly", "category_id": 999999,
		}), "category 999999 not found")
	})

	t.Run("invalid values are rejected instead of ignored", func(t *testing.T) {
		base := func(field string, value any) map[string]any {
			args := map[string]any{"name": "MCP Invalid", "cost": 1, "schedule": "Monthly"}
			args[field] = value
			return args
		}
		for _, field := range []struct {
			name  string
			value any
			want  string
		}{
			{"name", " ", "name"},
			{"cost", -1, "cost"},
			{"schedule", "Hourly", "schedule"},
			{"schedule", "", "schedule"},
			{"status", "Gone", "status"},
			{"start_date", "soon", "start_date"},
			{"renewal_date", "2026-13-01", "renewal_date"},
		} {
			require.Contains(t, callError("create_subscription", base(field.name, field.value)), field.want)
		}
		decode(call("list_subscriptions", map[string]any{}), &list)
		require.Equal(t, 0, list.Count, "a rejected create stores nothing")
	})

	t.Run("to-cancel fields are optional and validated", func(t *testing.T) {
		var plain, marked subscription
		decode(call("create_subscription", map[string]any{"name": "MCP Plain", "cost": 1, "schedule": "Monthly"}), &plain)
		require.False(t, plain.ToCancel)
		require.Equal(t, 3, plain.ToCancelReminderDays)
		decode(call("create_subscription", map[string]any{
			"name": "MCP Marked", "cost": 1, "schedule": "Monthly", "to_cancel": true, "to_cancel_reminder_days": 14,
		}), &marked)
		require.True(t, marked.ToCancel)
		require.Equal(t, 14, marked.ToCancelReminderDays)

		for _, days := range []int{0, 4, 7, -3} {
			require.Contains(t, callError("create_subscription", map[string]any{
				"name": "MCP Bad Days", "cost": 1, "schedule": "Monthly", "to_cancel_reminder_days": days,
			}), "to_cancel_reminder_days")
			require.Contains(t, callError("update_subscription", map[string]any{"id": marked.ID, "to_cancel_reminder_days": days}), "to_cancel_reminder_days")
		}

		var updated subscription
		decode(call("update_subscription", map[string]any{"id": marked.ID, "notes": "kept"}), &updated)
		require.True(t, updated.ToCancel, "omitted fields keep their values")
		require.Equal(t, 14, updated.ToCancelReminderDays)

		decode(call("update_subscription", map[string]any{"id": marked.ID, "to_cancel_reminder_days": 5}), &updated)
		require.Equal(t, 5, updated.ToCancelReminderDays)

		decode(call("update_subscription", map[string]any{"id": marked.ID, "to_cancel": false}), &updated)
		require.False(t, updated.ToCancel)
		require.Equal(t, 5, updated.ToCancelReminderDays)

		decode(call("update_subscription", map[string]any{"id": marked.ID, "to_cancel": true}), &updated)
		require.True(t, updated.ToCancel)
		decode(call("update_subscription", map[string]any{"id": marked.ID, "status": "Cancelled"}), &updated)
		require.False(t, updated.ToCancel, "Cancelled clears the intent")

		call("delete_subscription", map[string]any{"id": plain.ID})
		call("delete_subscription", map[string]any{"id": marked.ID})
	})

	t.Run("partial update keeps omitted fields and applies explicit empties", func(t *testing.T) {
		var sub subscription
		decode(call("create_subscription", map[string]any{
			"name": "MCP Full", "cost": 12.5, "schedule": "Annual", "status": "Paused", "original_currency": "EUR",
			"payment_method": "Visa", "autopay": true, "account": "me@example.test", "url": "https://example.test",
			"notes": "keep me", "start_date": "2026-01-15", "renewal_date": "2030-01-15",
			"category_id": category.ID, "cancellation_notice_days": 14,
		}), &sub)

		update := func(args map[string]any) subscription {
			t.Helper()
			args["id"] = sub.ID
			var out subscription
			decode(call("update_subscription", args), &out)
			return out
		}

		out := update(map[string]any{"cost": 13})
		require.Equal(t, 13.0, out.Cost)
		require.Equal(t, "MCP Full", out.Name)
		require.Equal(t, "Annual", out.Schedule)
		require.Equal(t, "Paused", out.Status)
		require.Equal(t, "EUR", out.OriginalCurrency)
		require.Equal(t, "Visa", out.PaymentMethod)
		require.NotNil(t, out.Autopay)
		require.True(t, *out.Autopay)
		require.Equal(t, "me@example.test", out.Account)
		require.Equal(t, "https://example.test", out.URL)
		require.Equal(t, "keep me", out.Notes)
		require.Equal(t, "2026-01-15", out.StartDate.Format("2006-01-02"))
		require.Equal(t, "2030-01-15", out.RenewalDate.Format("2006-01-02"))
		require.Equal(t, category.ID, out.CategoryID)
		require.Equal(t, "MCP test", out.Category.Name)
		require.Equal(t, 14, out.CancellationNoticeDays)
		require.True(t, out.ReminderEnabled)

		// Explicit zero, false and empty values are applied, not mistaken for omitted.
		out = update(map[string]any{"cost": 0})
		require.Zero(t, out.Cost)
		out = update(map[string]any{"autopay": false})
		require.NotNil(t, out.Autopay)
		require.False(t, *out.Autopay)
		out = update(map[string]any{"autopay": nil})
		require.Nil(t, out.Autopay)
		out = update(map[string]any{"cancellation_notice_days": 0})
		require.Zero(t, out.CancellationNoticeDays)
		out = update(map[string]any{"notes": "", "url": "", "account": "", "payment_method": ""})
		require.Empty(t, out.Notes)
		require.Empty(t, out.URL)
		require.Empty(t, out.Account)
		require.Empty(t, out.PaymentMethod)
		out = update(map[string]any{"start_date": "", "renewal_date": ""})
		require.Nil(t, out.StartDate)
		require.Nil(t, out.RenewalDate, "a Paused subscription has no calculated renewal date")
		out = update(map[string]any{"original_currency": ""})
		require.Equal(t, "USD", out.OriginalCurrency, "empty currency resets to the configured one")
		out = update(map[string]any{"category_id": 0})
		require.Zero(t, out.CategoryID)
		require.Empty(t, out.Category.Name)
		require.Equal(t, "MCP Full", out.Name, "clearing fields left the name alone")

		// Back onto a category.
		out = update(map[string]any{"category_id": category.ID})
		require.Equal(t, category.ID, out.CategoryID)

		// Values that would corrupt the row are refused, and leave it unchanged.
		for _, args := range []map[string]any{
			{"id": sub.ID, "name": ""},
			{"id": sub.ID, "schedule": ""},
			{"id": sub.ID, "status": ""},
			{"id": sub.ID, "cost": -5},
			{"id": sub.ID, "start_date": "not-a-date"},
			{"id": sub.ID, "category_id": 999999},
		} {
			callError("update_subscription", args)
		}
		var unchanged subscription
		decode(call("get_subscription", map[string]any{"id": sub.ID}), &unchanged)
		require.Equal(t, "MCP Full", unchanged.Name)
		require.Equal(t, category.ID, unchanged.CategoryID)
		require.Nil(t, unchanged.StartDate)

		callError("update_subscription", map[string]any{"id": 999999, "cost": 1})
		call("delete_subscription", map[string]any{"id": sub.ID})
	})

	t.Run("deleting a tagged subscription keeps shared tags", func(t *testing.T) {
		var first, second subscription
		decode(call("create_subscription", map[string]any{"name": "MCP Tagged A", "cost": 1, "schedule": "Monthly"}), &first)
		decode(call("create_subscription", map[string]any{"name": "MCP Tagged B", "cost": 1, "schedule": "Monthly"}), &second)
		tagRepo := repository.NewTagRepository(db)
		tags, err := tagRepo.FindOrCreateByNames([]string{"shared"})
		require.NoError(t, err)
		require.NoError(t, tagRepo.ReplaceSubscriptionTags(first.ID, tags))
		require.NoError(t, tagRepo.ReplaceSubscriptionTags(second.ID, tags))

		decode(call("delete_subscription", map[string]any{"id": first.ID}), &deleted)

		var links, tagRows int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM subscription_tags WHERE subscription_id = ?", first.ID).Scan(&links).Error)
		require.Zero(t, links)
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM subscription_tags WHERE subscription_id = ?", second.ID).Scan(&links).Error)
		require.EqualValues(t, 1, links)
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM tags WHERE id = ?", tags[0].ID).Scan(&tagRows).Error)
		require.EqualValues(t, 1, tagRows)

		var survivor struct {
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
		}
		decode(call("get_subscription", map[string]any{"id": second.ID}), &survivor)
		require.Len(t, survivor.Tags, 1)
		require.Equal(t, "shared", survivor.Tags[0].Name)
		call("delete_subscription", map[string]any{"id": second.ID})
	})
}

// lockedBuffer collects the server's stderr, which is written from the copying
// goroutine while the test may read it for a failure message.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func textOf(result *mcp.CallToolResult) string {
	var out string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			out += text.Text
		}
	}
	return out
}
