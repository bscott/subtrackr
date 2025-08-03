# Migration Guide for SubTrackr v0.3.0

## Overview

SubTrackr v0.3.0 introduces a new dynamic categories system that replaces the previous hardcoded category strings with a flexible database-driven approach. This guide will help you migrate your existing installation to v0.3.0.

## What's New

- **Dynamic Categories**: Categories are now stored in a separate database table
- **Category Management UI**: Add, edit, and delete categories from the Settings page
- **Foreign Key Relationships**: Subscriptions now reference categories by ID
- **Additional Schedule Options**: Support for Weekly and Daily subscription schedules

## Migration Steps

### 1. Backup Your Data

Before upgrading, make sure to backup your existing data:

```bash
# From the SubTrackr settings page, use the "Create Backup" button
# Or use the API:
curl -H "Authorization: Bearer YOUR_API_KEY" \
  http://localhost:8080/api/backup > subtrackr_backup.json
```

### 2. Update to v0.3.0

```bash
# Pull the latest changes
git pull origin v0.3.0

# Or download the v0.3.0 release
```

### 3. Restart SubTrackr

When you restart SubTrackr after updating:

1. The database schema will automatically migrate
2. Default categories will be created: Entertainment, Productivity, Storage, Software, Fitness, Education, Food, Travel, Business, Other
3. Existing subscriptions will be mapped to the new category system

### 4. Verify Migration

After restarting:

1. Check that all your subscriptions are still visible
2. Verify that categories have been properly assigned
3. Visit Settings → Categories to manage your categories

## API Changes

If you're using the SubTrackr API, note these changes:

### Creating/Updating Subscriptions

**Before (v0.2.x):**
```json
{
  "name": "Netflix",
  "cost": 15.99,
  "schedule": "Monthly",
  "status": "Active",
  "category": "Entertainment"
}
```

**After (v0.3.0):**
```json
{
  "name": "Netflix",
  "cost": 15.99,
  "schedule": "Monthly",
  "status": "Active",
  "category_id": 1
}
```

### Getting Category IDs

To get the list of available categories and their IDs:

```bash
curl http://localhost:8080/api/categories
```

## New Features

### Schedule Options

v0.3.0 adds support for Weekly and Daily schedules in addition to Monthly and Annual:

- **Weekly**: Billed every 7 days
- **Daily**: Billed every day

### Category Management

- Add custom categories for better organization
- Edit category names
- Delete unused categories (only if no subscriptions are using them)

## Troubleshooting

### Issue: Categories not showing after upgrade

**Solution**: The categories should be automatically created on first run. If not, manually create them in Settings → Categories.

### Issue: API calls failing with category errors

**Solution**: Update your API calls to use `category_id` instead of `category`. Get the category IDs from the `/api/categories` endpoint.

### Issue: Cannot delete a category

**Solution**: Categories with active subscriptions cannot be deleted. First reassign or delete the subscriptions using that category.

## Need Help?

If you encounter any issues during migration:

1. Check the server logs for error messages
2. Restore from your backup if needed
3. Report issues at: https://github.com/bscott/subtrackr/issues