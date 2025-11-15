# Release Notes - v0.4.8

## 🎉 New Features

### Renewal Reminder Emails
- **Automatic renewal reminders**: SubTrackr now sends email reminders for upcoming subscription renewals
- **Configurable reminder window**: Set how many days in advance to receive reminders (default: 7 days)
- **Daily scheduler**: Background process checks for upcoming renewals daily and sends reminder emails
- **Smart filtering**: Only active subscriptions with renewal dates are included in reminders
- **Email template**: Beautiful HTML email template with subscription details and renewal date

### Improved Subscription Notes Display
- **Hover tooltip**: Subscription notes are now displayed in a compact tooltip on hover instead of a separate row
- **Eye icon indicator**: Small eye icon appears next to edit/delete buttons when a subscription has notes
- **Auto-sizing tooltip**: Tooltip width automatically adjusts to match the note text length
- **Better UX**: Cleaner table layout with notes accessible on demand

## 🔧 Technical Improvements

- Added `SendRenewalReminder()` method to EmailService
- Added `GetSubscriptionsNeedingReminders()` method to SubscriptionService
- Implemented background scheduler with daily checks
- Added comprehensive test suite for renewal reminder functionality
- Improved template structure for better maintainability

## 🧪 Testing

- Added 13 test cases covering renewal reminder functionality
- Tests cover edge cases, boundary conditions, and error scenarios
- All tests passing

## 📝 How to Use

### Renewal Reminders
1. Configure SMTP settings in Settings page
2. Enable "Renewal Reminders" toggle in Settings
3. Set "Reminder Days" (how many days before renewal to send reminder)
4. Ensure subscriptions have renewal dates set
5. Reminders will be sent automatically via email

### Subscription Notes
- Notes are now visible via hover tooltip on the eye icon
- Tooltip appears when hovering over the eye icon in the Actions column
- No changes needed - works automatically with existing notes

## 🐛 Bug Fixes

- Fixed subscription notes display taking up unnecessary table space
- Improved tooltip positioning and sizing

## 📦 Files Changed

- `cmd/server/main.go` - Added renewal reminder scheduler
- `internal/service/email.go` - Added SendRenewalReminder method
- `internal/service/subscription.go` - Added GetSubscriptionsNeedingReminders method
- `internal/service/renewal_reminder_test.go` - Comprehensive test suite
- `templates/subscriptions.html` - Updated notes display with tooltip
- `templates/subscription-list.html` - Updated notes display with tooltip

## 🔄 Migration Notes

No database migrations required. This is a feature addition that works with existing data.

## ⚠️ Breaking Changes

None. All changes are backward compatible.

