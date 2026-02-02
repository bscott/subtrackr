# SubTrackr - Agent Documentation

## Project Overview

SubTrackr is a self-hosted subscription management application built with Go and HTMX. It helps users track subscriptions, visualize spending, and get renewal reminders.

## Architecture

### Tech Stack
- **Backend**: Go 1.21+ with Gin web framework
- **Database**: SQLite (GORM)
- **Frontend**: HTMX + Tailwind CSS
- **Deployment**: Docker & Docker Compose

### Project Structure

```
subtrackr-xyz/
├── cmd/
│   ├── server/          # Main server entry point
│   └── migrate-dates/   # Date migration utility
├── internal/
│   ├── config/          # Configuration management
│   ├── database/        # Database initialization and migrations
│   ├── handlers/        # HTTP request handlers (Gin handlers)
│   ├── middleware/      # HTTP middleware (auth, etc.)
│   ├── models/          # Data models (GORM models)
│   ├── repository/      # Data access layer
│   ├── service/         # Business logic layer
│   └── version/         # Version information
├── templates/           # HTML templates (HTMX)
├── web/static/          # Static assets (JS, CSS, images)
├── tests/               # Playwright E2E tests
└── data/                # SQLite database (gitignored)
```

### Key Components

#### 1. Server Entry Point (`cmd/server/main.go`)
- Initializes database, repositories, services, and handlers
- Sets up Gin router with templates
- Configures routes (web and API)
- Starts HTTP server

#### 2. Handlers (`internal/handlers/`)
- **subscription.go**: CRUD operations for subscriptions
- **settings.go**: SMTP config, Pushover config, notifications, API keys, currency, dark mode
- **category.go**: Category management

#### 3. Services (`internal/service/`)
- Business logic layer
- **subscription.go**: Subscription operations
- **settings.go**: Settings management
- **category.go**: Category operations
- **currency.go**: Currency conversion (Fixer.io integration)
- **email.go**: Email notification service (SMTP)
- **pushover.go**: Pushover notification service

#### 4. Models (`internal/models/`)
- GORM models:
  - `Subscription`: Main subscription entity
  - `Category`: Subscription categories
  - `Settings`: Application settings (key-value store)
  - `SMTPConfig`: Email configuration
  - `PushoverConfig`: Pushover notification configuration
  - `APIKey`: API authentication keys
  - `ExchangeRate`: Currency exchange rates

#### 5. Repository (`internal/repository/`)
- Data access layer using GORM
- Abstracts database operations

### Routing Structure

#### Web Routes (HTMX)
- `/` - Dashboard
- `/dashboard` - Dashboard
- `/subscriptions` - Subscription list
- `/analytics` - Analytics view
- `/settings` - Settings page
- `/form/subscription` - Subscription form modal

#### API Routes (HTMX)
- `/api/subscriptions` - Subscription CRUD
- `/api/stats` - Statistics
- `/api/export/*` - Data export
- `/api/settings/*` - Settings management
- `/api/categories` - Category management

#### Public API Routes (Require API Key)
- `/api/v1/subscriptions` - Subscription CRUD
- `/api/v1/stats` - Statistics
- `/api/v1/export/*` - Data export

### Database Schema

#### Subscriptions
- ID, Name, Cost, OriginalCurrency
- Schedule: Monthly, Annual, Weekly, Daily
- Status: Active, Cancelled, Paused, Trial
- CategoryID (foreign key)
- Dates: StartDate, RenewalDate, CancellationDate
- Additional: PaymentMethod, Account, URL, Notes, Usage

#### Categories
- ID, Name
- CreatedAt, UpdatedAt

#### Settings
- Key-value store for application settings
- Keys: `smtp_config`, `renewal_reminders`, `currency`, etc.

### Key Features

1. **Subscription Management**
   - CRUD operations
   - Multiple schedules (Monthly, Annual, Weekly, Daily)
   - Categories
   - Multi-currency support

2. **Email Notifications**
   - SMTP configuration with TLS/SSL support
   - STARTTLS for ports 2525, 8025, 587, 25, 80
   - Implicit TLS for ports 465, 8465, 443
   - Renewal reminders
   - High cost alerts

3. **Pushover Notifications**
   - Pushover API integration for mobile push notifications
   - User Key and Application Token configuration
   - Renewal reminders (same settings as email)
   - High cost alerts (same threshold as email)
   - Works alongside email notifications

4. **Currency Support**
   - USD, EUR, GBP, JPY, RUB, SEK, PLN, INR, CHF, BRL, COP, BDT
   - Optional Fixer.io integration for real-time rates
   - Automatic conversion display
   - BDT (Bangladeshi Taka) with ৳ symbol

5. **API Access**
   - API key authentication
   - RESTful endpoints
   - JSON responses

5. **Data Management**
   - CSV/JSON export
   - Backup functionality
   - Clear all data option

### Development Guidelines

#### Code Style
- Follow Go standard formatting (`go fmt`)
- Use meaningful variable and function names
- Add comments for exported functions
- Keep functions focused and small

#### Error Handling
- Return errors from functions, don't panic
- Log errors appropriately
- Provide user-friendly error messages in handlers

#### Testing
- Unit tests in `*_test.go` files
- E2E tests in `tests/` using Playwright
- Test API endpoints with `test-api.sh`

#### Database Migrations
- Migrations in `internal/database/migrations.go`
- Use GORM AutoMigrate for schema changes
- Test migrations on sample data

#### Frontend
- Use HTMX for dynamic updates
- Tailwind CSS for styling
- Dark mode support via class-based switching
- Mobile-responsive design

### Recent Changes

#### v0.5.3 - Sort Persistence and PWA Support
- Remember sorting preference (#85) - localStorage persistence
- Fix Tab and PWA icon missing (#84) - favicon, apple-touch-icon, manifest.json
- Input validation for sort parameters
- PWA meta tags on all HTML templates

#### v0.5.2 - Currency Improvements
- Enhanced currency support and conversion display

#### v0.5.1 - Dark Classic Theme and Calendar Fixes
- Dark classic theme option
- Calendar view improvements

#### v0.5.0 - Optional Login Support
- Optional authentication system
- Beautiful theme options

### Release Workflow

This project uses versioned branches for releases. See `CLAUDE.md` for the complete workflow.

**Quick Reference:**
1. Create versioned branch: `git checkout -b vX.Y.Z`
2. Track work with beads: `bd create`, `bd update`, `bd close`
3. Create draft release: `gh release create vX.Y.Z --draft`
4. Run code review agent before committing
5. Commit, push, create PR: `gh pr create`
6. Comment on GitHub issues: `gh issue comment`
7. Monitor CI: `gh run watch`
8. Merge PR: `gh pr merge --merge --delete-branch`
9. Publish release: `gh release edit vX.Y.Z --draft=false`

### Common Tasks

#### Adding a New Feature
1. Create/update model in `internal/models/`
2. Add repository methods in `internal/repository/`
3. Add service logic in `internal/service/`
4. Create handler in `internal/handlers/`
5. Add routes in `cmd/server/main.go`
6. Update templates if needed
7. Add tests

#### Adding a New Schedule Type
1. Update `Subscription.Schedule` validation in `internal/models/subscription.go`
2. Update `AnnualCost()` and `MonthlyCost()` methods
3. Update frontend templates to include new option
4. Update date calculation logic if needed

#### Adding a New Currency
1. Add currency code to `SupportedCurrencies` in `internal/service/currency.go`
2. Add currency symbol mapping in `GetCurrencySymbol()` in `internal/service/settings.go`
3. Add currency option to currency selection in `templates/settings.html`
4. Update exchange rate handling if using Fixer.io

#### Adding a New Notification Method
1. Create notification config model in `internal/models/settings.go`
2. Create notification service in `internal/service/` (e.g., `pushover.go`)
3. Add config save/get methods to `SettingsService`
4. Add handlers in `internal/handlers/settings.go`
5. Add UI in `templates/settings.html`
6. Update subscription handler to send notifications
7. Update renewal reminder scheduler in `cmd/server/main.go`

### Environment Variables

- `PORT` - Server port (default: 8080)
- `DATABASE_PATH` - SQLite database path (default: ./data/subtrackr.db)
- `GIN_MODE` - Gin mode: debug/release (default: debug)
- `FIXER_API_KEY` - Fixer.io API key for currency conversion (optional)

### Building and Running

```bash
# Development
go run cmd/server/main.go

# Build
go build -o subtrackr cmd/server/main.go

# Docker
docker-compose up -d --build
```

### Testing

```bash
# Run Go tests
go test ./...

# Run E2E tests
npm test

# Test API
./test-api.sh
```


## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
