# Plan: Optional Login Support in Settings

## Overview

Add optional authentication to SubTrackr that can be enabled/disabled from the Settings menu. This must be backward-compatible with existing single-user installations.

---

## Current State Analysis

### What Exists
- **No authentication**: App assumes single user, all routes public
- **API Key auth**: Already exists for `/api/v1/*` routes (external access)
- **Settings infrastructure**: Key-value store in SQLite, well-structured service layer
- **Repository pattern**: Clean separation of concerns ready for extension

### Key Files to Modify
- `internal/handlers/settings.go` - Add login settings handlers
- `internal/service/settings.go` - Add auth settings management
- `internal/middleware/auth.go` - Extend with session-based auth
- `internal/database/migrations.go` - Add user table migration
- `internal/models/` - Add User model
- `templates/settings.html` - Add login configuration section
- `cmd/server/main.go` - Conditional middleware application

---

## Design Decisions

### 1. Authentication Model: **Optional Single-User Auth**

**Rationale**: SubTrackr is designed as a self-hosted personal tool. Multi-user support adds complexity without clear benefit.

**Approach**:
- Single admin account (username + password)
- No user registration - admin sets credentials in settings
- Session-based auth using secure cookies
- Login can be enabled/disabled at any time

### 2. Settings-Based Toggle

**New Settings Keys**:
```
auth_enabled         (bool)   - Master toggle for login requirement
auth_username        (string) - Admin username
auth_password_hash   (string) - bcrypt hash of password
auth_session_secret  (string) - Secret for signing session cookies
```

### 3. State Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    INSTALLATION STATES                       │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  [Existing Install]              [New Install]               │
│        │                              │                      │
│        ▼                              ▼                      │
│  auth_enabled = false           auth_enabled = false         │
│  (no credentials set)           (no credentials set)         │
│        │                              │                      │
│        │  User enables auth           │                      │
│        │  in Settings                 │                      │
│        ▼                              ▼                      │
│  ┌──────────────┐              ┌──────────────┐             │
│  │ Setup Mode   │              │ Setup Mode   │             │
│  │ - Set user   │              │ - Set user   │             │
│  │ - Set pass   │              │ - Set pass   │             │
│  └──────────────┘              └──────────────┘             │
│        │                              │                      │
│        ▼                              ▼                      │
│  auth_enabled = true            auth_enabled = true          │
│  (credentials set)              (credentials set)            │
│        │                              │                      │
│        ▼                              ▼                      │
│  All routes protected           All routes protected         │
│  Login page required            Login page required          │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Impact on Existing Installations

### Zero Breaking Changes Guarantee

| Scenario | Current Behavior | After Update |
|----------|------------------|--------------|
| Fresh install | No auth | No auth (unchanged) |
| Existing install | No auth | No auth (unchanged) |
| User enables auth | N/A | Prompted to set credentials |
| User disables auth | N/A | Returns to open access |

### Migration Strategy

1. **No automatic migration** - auth stays disabled by default
2. **No forced password creation** - user must opt-in
3. **Settings page accessible** - even without auth, settings remain accessible to allow setup
4. **Graceful fallback** - if session expires, redirect to login (not error)

---

## Implementation Approach

### Option A: Settings-First (Recommended)

**Flow**:
1. Add "Security" section to Settings page
2. Toggle "Require Login" is OFF by default
3. When enabled, form expands to set username/password
4. After credentials saved, auth middleware activates
5. User must login on next page navigation

**Pros**:
- All configuration in one place
- No separate setup wizard needed
- Easy to disable if locked out

**Cons**:
- Slight complexity in settings page

### Option B: Environment Variable Override

**Flow**:
1. `AUTH_ENABLED=true` env var forces auth requirement
2. `AUTH_USERNAME` and `AUTH_PASSWORD` env vars set credentials
3. Settings page shows status but cannot override env

**Pros**:
- Familiar for Docker deployments
- Can't be accidentally disabled
- Matches security best practices

**Cons**:
- Requires container restart to change
- Password in plain text in env

### Option C: Hybrid Approach (Best of Both)

**Flow**:
1. Check for `AUTH_ENABLED` env var first (highest priority)
2. If env not set, check database setting
3. UI shows which mode is active
4. If env-controlled, UI is read-only

**New Environment Variables**:
```
AUTH_ENABLED=true|false     # Override toggle (optional)
AUTH_USERNAME=admin         # Only used with AUTH_ENABLED=true
AUTH_PASSWORD=securepass    # Only used with AUTH_ENABLED=true (hashed on first use)
```

---

## Security Considerations

### Password Storage
- **bcrypt** with cost factor 12+
- Never store plain text passwords
- Environment variable passwords hashed on first server start

### Session Management
- **Secure cookies** with HttpOnly, SameSite=Strict
- Session timeout: 24 hours (configurable)
- Session secret auto-generated if not provided
- CSRF protection via SameSite cookies + HTMX headers

### Protected Routes (when auth enabled)
```
Protected:
  /                    - Dashboard
  /subscriptions       - Subscription list
  /analytics           - Analytics
  /calendar            - Calendar
  /api/subscriptions/* - Internal API
  /api/settings/*      - Settings API (except login)

Unprotected:
  /login               - Login page
  /api/auth/login      - Login endpoint
  /api/v1/*            - External API (uses API keys)
  /static/*            - Static assets
```

### Lockout Recovery

**Problem**: User forgets password, locked out of app

**Solutions** (in order of preference):
1. **Environment override**: Set `AUTH_PASSWORD=newpassword` and restart
2. **Database direct edit**: Delete `auth_password_hash` row from settings table
3. **Data directory backup/restore**: Restore from backup without auth

---

## Database Changes

### No New Tables Required

Using existing `settings` table for auth configuration:

```sql
-- New settings rows (only created when auth enabled)
INSERT INTO settings (key, value) VALUES
  ('auth_enabled', 'true'),
  ('auth_username', 'admin'),
  ('auth_password_hash', '$2a$12$...'),  -- bcrypt hash
  ('auth_session_secret', 'random-64-char-string');
```

**Why not a users table?**
- Single-user design doesn't need it
- Simpler migration path
- Settings table already handles typed values well
- Avoids foreign key complexity

---

## UI/UX Design

### Settings Page Addition

```
┌─────────────────────────────────────────────────────────────┐
│ Settings                                                     │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│ ▼ Data Management                                           │
│   [Export] [Backup] [Clear Data]                            │
│                                                              │
│ ▼ Email Notifications                                       │
│   [...existing SMTP settings...]                            │
│                                                              │
│ ▼ Security  ← NEW SECTION                                   │
│   ┌─────────────────────────────────────────────────────┐  │
│   │ Require Login                          [Toggle OFF] │  │
│   │                                                      │  │
│   │ ┌─ When enabled: ────────────────────────────────┐  │  │
│   │ │ Username: [________________]                    │  │  │
│   │ │ Password: [________________]                    │  │  │
│   │ │ Confirm:  [________________]                    │  │  │
│   │ │                                                 │  │  │
│   │ │ Session Timeout: [24] hours                     │  │  │
│   │ │                                                 │  │  │
│   │ │ [Save Credentials]                              │  │  │
│   │ └─────────────────────────────────────────────────┘  │  │
│   │                                                      │  │
│   │ ⓘ When login is required, you'll need to sign in    │  │
│   │   to access SubTrackr. API keys still work for      │  │
│   │   external integrations.                            │  │
│   └─────────────────────────────────────────────────────┘  │
│                                                              │
│ ▼ Appearance                                                │
│   Dark Mode [Toggle]                                        │
│                                                              │
│ ▼ Currency                                                  │
│   [...currency options...]                                  │
│                                                              │
│ ▼ API Keys                                                  │
│   [...existing API key management...]                       │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Login Page Design

```
┌─────────────────────────────────────────────────────────────┐
│                                                              │
│                      SubTrackr Logo                          │
│                                                              │
│              ┌──────────────────────────┐                   │
│              │ Username                 │                   │
│              │ [____________________]   │                   │
│              │                          │                   │
│              │ Password                 │                   │
│              │ [____________________]   │                   │
│              │                          │                   │
│              │ [ ] Remember me          │                   │
│              │                          │                   │
│              │      [  Sign In  ]       │                   │
│              └──────────────────────────┘                   │
│                                                              │
│         Forgot password? Check the docs for help.           │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Implementation Steps

### Phase 1: Backend Foundation
1. Add bcrypt dependency for password hashing
2. Create auth settings methods in SettingsService
3. Implement session management (cookie-based)
4. Create login/logout handlers
5. Create auth middleware that checks session

### Phase 2: Settings UI
6. Add Security section to settings.html
7. Implement credential form with HTMX
8. Add toggle state management
9. Handle auth enable/disable flow

### Phase 3: Login Page
10. Create login.html template
11. Implement login form with HTMX
12. Add error handling (wrong password, etc.)
13. Add redirect after login

### Phase 4: Route Protection
14. Apply auth middleware conditionally
15. Handle redirect to login for protected routes
16. Ensure API keys still work independently

### Phase 5: Testing & Edge Cases
17. Test existing installations (no regression)
18. Test enable/disable flow
19. Test lockout recovery
20. Test session timeout
21. Update documentation

---

## Open Questions

1. **Session storage**: In-memory (simple, lost on restart) vs SQLite (persistent)?
   - Recommendation: In-memory with "Remember me" extending cookie life

2. **Multiple failed login attempts**: Rate limiting?
   - Recommendation: Simple delay after 5 failed attempts

3. **Password requirements**: Minimum complexity?
   - Recommendation: Minimum 8 characters, no complexity rules (user's choice)

4. **HTTPS requirement**: Should auth require HTTPS?
   - Recommendation: Warn but allow HTTP (self-hosted often behind reverse proxy)

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| User locked out | Medium | High | Env var override, clear docs |
| Session hijacking | Low | Medium | Secure cookies, HTTPS warning |
| Brute force attack | Low | Medium | Rate limiting after failures |
| Regression in existing installs | Low | High | Comprehensive testing |
| Complexity creep | Medium | Medium | Keep single-user, no roles |

---

## Success Criteria

- [ ] Existing installations work unchanged after update
- [ ] Auth can be enabled from Settings with zero config files
- [ ] Login page is functional and styled consistently
- [ ] Sessions persist across server restarts (Remember me)
- [ ] Lockout recovery is documented and tested
- [ ] API keys continue working independently
- [ ] No performance impact when auth is disabled
