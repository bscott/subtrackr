# SubTrackr UI Translations

Each `<code>.json` file in this folder is a flat dictionary of dotted-key → translated
string for one language. `en.json` is the canonical source — every translatable key
lives there first, and other languages fall back to English for any key they're missing.

Currently shipped:
- `en.json` — English (canonical)
- `es.json` — Español
- `de.json` — Deutsch
- `nl.json` — Nederlands
- `zh.json` — 中文

## Translation quality note

The non-English translations were produced by an AI-assisted initial pass and have not
been reviewed by native speakers. If you spot a phrase that sounds awkward, stilted,
or wrong, **please send a PR** — refinements are small and very welcome. You don't
need to fix everything, even single-key tweaks are worth merging.

## Adding a new language

1. Copy `en.json` to `<code>.json` (use ISO 639-1, e.g. `fr.json`, `pt.json`).
2. Translate each value. Keep the keys identical to `en.json`. The first key,
   `lang.name`, should be the language's name in that language (e.g. `"Français"`).
3. Leave any key you don't want to translate yet — it will fall back to English.
4. Restart the server. The language will appear in **Settings → Appearance → Language**.

## Editing an existing translation

1. Find the offending key in the relevant JSON file.
2. Edit the value. Save.
3. Restart the server (or just refresh the page — templates are re-read on each request).
4. Verify via the language selector that the new wording renders correctly.

## What gets translated

The translation pass currently covers the primary user surfaces:

- Top nav and mobile menu
- Dashboard (stat cards, headings)
- Subscriptions list (column headers, row actions, empty state)
- Subscription form (every label, helper text, dropdown option)
- Settings page (every section: Appearance, Notifications, Data Management, SMTP,
  Pushover, Webhook, Security, Currency, Date Format, Categories, API Keys, About)
- Analytics page
- Calendar page heading + subscribe button
- Login / Forgot Password / Reset Password
- Error page

A few things stay English by design:

- Proper-noun service names (SMTP, Pushover, Webhook, SubTrackr, SQLite, API, JSON, CSV)
- Language self-names in the language selector (English, Español, Deutsch, Nederlands, 中文)
- The in-page API documentation reference under Settings (developer-facing; standard
  practice to keep English)

## Coverage tests

`tests/i18n-coverage.spec.js` loads every primary page in every supported language and
asserts both:

- expected translated strings appear, AND
- known English-only sentinels do **not** leak through

If you add a new translatable string in a template, please:

1. Add the key to `en.json`.
2. Add it to every other language file (the fallback works, but coverage is the goal).
3. Run `npx playwright test tests/i18n-coverage.spec.js --workers=1` — if you've added
   a sentinel that should be translated, add a check for it to the spec.

## File layout reminder

Translations are flat JSON, not nested:

```json
{
  "settings.title": "Settings",
  "settings.appearance": "Appearance"
}
```

**not**

```json
{
  "settings": { "title": "Settings", "appearance": "Appearance" }
}
```

The flat structure keeps lookups O(1) and lets you grep for a string across the whole
catalog with a single command:

```bash
grep '"settings.title"' web/locales/*.json
```
