# Exchange-rate response fixtures

Response bodies shaped like what real rate sources send, used by
`TestRateSourceResponseFixtures` in `../../currency_regression_test.go`.

`{{TIMESTAMP}}` and `{{DATE}}` are replaced with a recent time when a test
loads a fixture, so the rates are not stale. Fixer.io fixtures follow the
formats in Fixer's API documentation; the ECB fixture follows a keyless
Fixer-compatible mirror that serves ECB reference rates (it adds extra fields,
which must be ignored, and omits currencies the ECB does not publish).

Add a fixture whenever a new source shape or failure is found, with its
expected outcome in the test table.
