# Plan: Optional Login Support in Settings

## Overview

Add optional authentication to SubTrackr that can be enabled/disabled from the Settings menu. This must be backward-compatible with existing single-user installations.

---

## Confirmed Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Login toggle location | Settings page | All config in one place |
| Default state | **OFF** | No breaking changes for existing/new installs |
| Scope | Single-user auth | Self-hosted personal tool, no multi-user needed |

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
auth_enabled            (bool)   - Master toggle for login requirement
auth_username           (string) - Admin username
auth_password_hash      (string) - bcrypt hash of password
auth_session_secret     (string) - Secret for signing session cookies
auth_reset_token        (string) - Temporary password reset token (cleared after use)
auth_reset_token_expiry (string) - Reset token expiration timestamp
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

## Implementation Approach (Confirmed: Settings-First)

**Flow**:
1. Add "Security" section to Settings page
2. Toggle "Require Login" is **OFF by default**
3. **Prerequisite check**: SMTP must be configured before login can be enabled
4. When user enables toggle, form expands to set username/password
5. After credentials saved, auth middleware activates
6. User must login on next page navigation

**SMTP Prerequisite Requirement**:
- Login toggle is disabled/grayed out until SMTP is configured and tested
- Shows message: "Configure email settings above to enable password recovery"
- This ensures users always have a "Forgot Password" recovery path
- Prevents lockout scenarios where user has no way to reset password

**Benefits**:
- All configuration in one place (no env vars required)
- No separate setup wizard needed
- Easy to disable if locked out (just toggle off)
- Zero impact on existing installations until user opts in
- **Password recovery always available** via email

**Optional: Environment Variable Override** (for advanced users)

For Docker deployments where UI access isn't preferred:
```
AUTH_ENABLED=true|false     # Override toggle (optional)
AUTH_USERNAME=admin         # Only used with AUTH_ENABLED=true
AUTH_PASSWORD=securepass    # Hashed on first server start
```
When env vars are set, Settings UI shows read-only status.

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
1. **Forgot Password email** (primary): Click "Forgot Password" on login page, receive reset link via SMTP
2. **Environment override**: Set `AUTH_PASSWORD=newpassword` and restart
3. **Database direct edit**: Delete `auth_password_hash` row from settings table
4. **Data directory backup/restore**: Restore from backup without auth

**Note**: SMTP is required before enabling login, ensuring option #1 is always available.

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
│              │                          │                   │
│              │    [Forgot Password?]    │                   │
│              └──────────────────────────┘                   │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Forgot Password Page Design

```
┌─────────────────────────────────────────────────────────────┐
│                                                              │
│                      SubTrackr Logo                          │
│                                                              │
│              ┌──────────────────────────┐                   │
│              │ Reset Your Password      │                   │
│              │                          │                   │
│              │ A reset link will be     │                   │
│              │ sent to your configured  │                   │
│              │ email address.           │                   │
│              │                          │                   │
│              │   [  Send Reset Link  ]  │                   │
│              │                          │                   │
│              │   [Back to Login]        │                   │
│              └──────────────────────────┘                   │
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

### Phase 3: Login Page & Password Reset
10. Create login.html template
11. Implement login form with HTMX
12. Add error handling (wrong password, etc.)
13. Add redirect after login
14. Create forgot-password.html template
15. Implement password reset email sending (uses existing EmailService)
16. Create reset-password.html template for setting new password
17. Handle reset token generation, validation, and expiration

### Phase 4: Route Protection
18. Apply auth middleware conditionally
19. Handle redirect to login for protected routes
20. Ensure API keys still work independently

### Phase 5: Testing & Edge Cases
21. Test existing installations (no regression)
22. Test enable/disable flow
23. Test password reset flow via email
24. Test lockout recovery
25. Test session timeout
26. Update documentation

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
