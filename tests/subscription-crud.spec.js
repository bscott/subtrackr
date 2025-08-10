// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('Subscription CRUD Operations', () => {
  test('can create a new subscription', async ({ page }) => {
    await page.goto('/subscriptions');

    // Click Add Subscription button
    await page.click('button:has-text("Add Subscription")');

    // Fill out the form
    await page.fill('input[name="name"]', 'Test Subscription');
    await page.fill('input[name="cost"]', '9.99');
    await page.selectOption('select[name="billing_cycle"]', 'Monthly');
    await page.selectOption('select[name="status"]', 'Active');

    // Submit the form
    await page.click('button[type="submit"]');

    // Wait for page reload and check if subscription appears
    await page.waitForLoadState('networkidle');
    await expect(page.getByText('Test Subscription')).toBeVisible();
    await expect(page.getByText('$9.99')).toBeVisible();
  });

  test('can edit an existing subscription', async ({ page }) => {
    await page.goto('/subscriptions');

    // Assuming there's at least one subscription from the previous test
    // Click the first edit button
    await page.click('button:has-text("Edit"):first-of-type');

    // Modify the name
    await page.fill('input[name="name"]', 'Updated Test Subscription');
    await page.fill('input[name="cost"]', '14.99');

    // Submit the form
    await page.click('button[type="submit"]');

    // Wait for page reload and check if changes are saved
    await page.waitForLoadState('networkidle');
    await expect(page.getByText('Updated Test Subscription')).toBeVisible();
    await expect(page.getByText('$14.99')).toBeVisible();
  });

  test('displays correct currency formatting', async ({ page }) => {
    await page.goto('/subscriptions');

    // Check that all prices end with .00 or have proper decimal formatting
    const priceElements = await page.locator('[data-testid="subscription-cost"], .text-sm.font-medium.text-gray-900').all();
    
    for (const element of priceElements) {
      const text = await element.textContent();
      if (text && text.includes('$')) {
        // Should match format like $9.99 or $10.00
        expect(text).toMatch(/\$\d+\.\d{2}/);
      }
    }
  });

  test('annual totals calculation is correct', async ({ page }) => {
    await page.goto('/');

    // Get the annual total from dashboard
    const annualTotalElement = page.locator('[data-testid="annual-total"]');
    if (await annualTotalElement.count() > 0) {
      const annualTotal = await annualTotalElement.textContent();
      
      // Navigate to subscriptions and calculate expected total
      await page.goto('/subscriptions');
      
      const subscriptionElements = await page.locator('[data-testid="subscription-row"]').all();
      let expectedTotal = 0;
      
      for (const row of subscriptionElements) {
        const costText = await row.locator('[data-testid="subscription-cost"]').textContent();
        const billingCycleText = await row.locator('[data-testid="billing-cycle"]').textContent();
        
        if (costText && billingCycleText) {
          const cost = parseFloat(costText.replace('$', ''));
          let annualCost = cost;
          
          if (billingCycleText.includes('Monthly')) {
            annualCost = cost * 12;
          } else if (billingCycleText.includes('Weekly')) {
            annualCost = cost * 52;
          } else if (billingCycleText.includes('Daily')) {
            annualCost = cost * 365;
          }
          
          expectedTotal += annualCost;
        }
      }
      
      // Compare with actual total (allowing for small floating point differences)
      const actualTotal = parseFloat(annualTotal?.replace('$', '') || '0');
      expect(Math.abs(actualTotal - expectedTotal)).toBeLessThan(0.01);
    }
  });
});