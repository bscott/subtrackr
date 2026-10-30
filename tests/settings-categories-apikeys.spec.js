// @ts-check
// Settings page: category management and API keys.
const { test, expect } = require('./helpers/settings-server');
const { seedSubscription } = require('./helpers/subscriptions');

test.describe.configure({ mode: 'serial' });

async function categoryByName(request, name) {
  const categories = await (await request.get('/api/categories')).json();
  return categories.find((c) => c.name === name);
}

test.describe('categories', () => {
  test('creates, renames and deletes a category', async ({ page, request }) => {
    const name = `E2E category ${Date.now()}`;
    const renamed = `${name} renamed`;
    try {
      await page.goto('/settings');
      await page.locator('#category_name').fill(name);
      await page.locator('#add-category-form button[type="submit"]').click();
      const list = page.locator('#categories-list');
      await expect(list.getByText(name, { exact: true })).toBeVisible();
      await expect(page.locator('#category_name')).toHaveValue('');
      const created = await categoryByName(request, name);
      expect(created).toBeTruthy();

      const row = list.locator('> div', { has: page.locator(`#category-name-${created.id}`) });
      await row.getByRole('button', { name: 'Edit' }).click();
      const editForm = page.locator(`#edit-category-form-${created.id}`);
      await expect(editForm).toBeVisible();
      await editForm.locator('input[name="name"]').fill(renamed);
      await editForm.getByRole('button', { name: 'Save' }).click();
      await expect(page.locator(`#category-name-${created.id}`)).toHaveText(renamed);
      await expect(page.locator(`#category-name-${created.id}`)).toBeVisible();

      await page.reload();
      await expect(page.locator(`#category-name-${created.id}`)).toHaveText(renamed);
      expect((await categoryByName(request, renamed))?.id).toBe(created.id);

      // Cancelling the confirm keeps the category.
      page.once('dialog', (dialog) => dialog.dismiss());
      await row.getByRole('button', { name: 'Delete' }).click();
      await expect(page.locator(`#category-name-${created.id}`)).toBeVisible();
      expect(await categoryByName(request, renamed)).toBeTruthy();

      page.once('dialog', (dialog) => {
        expect(dialog.message()).toBe('Delete this category?');
        dialog.accept();
      });
      await row.getByRole('button', { name: 'Delete' }).click();
      await expect(page.locator(`#category-name-${created.id}`)).toHaveCount(0);
      expect(await categoryByName(request, renamed)).toBeUndefined();
    } finally {
      for (const n of [name, renamed]) {
        const leftover = await categoryByName(request, n);
        if (leftover) await request.delete(`/api/categories/${leftover.id}`);
      }
    }
  });

  test('cancel edit restores the category name', async ({ page, request }) => {
    const created = await (await request.post('/api/categories', { data: { name: `E2E cancel ${Date.now()}` } })).json();
    try {
      await page.goto('/settings');
      const nameLabel = page.locator(`#category-name-${created.id}`);
      const editForm = page.locator(`#edit-category-form-${created.id}`);
      const row = page.locator('#categories-list > div', { has: nameLabel });
      await row.getByRole('button', { name: 'Edit' }).click();
      await expect(nameLabel).toBeHidden();
      await editForm.locator('input[name="name"]').fill('not saved');
      await editForm.getByRole('button', { name: 'Cancel' }).click();
      await expect(editForm).toBeHidden();
      await expect(nameLabel).toHaveText(created.name);
      expect((await categoryByName(request, created.name))?.id).toBe(created.id);
    } finally {
      await request.delete(`/api/categories/${created.id}`);
    }
  });

  test('a category that has subscriptions cannot be deleted', async ({ page, request }) => {
    const category = await (await request.post('/api/categories', { data: { name: `E2E in use ${Date.now()}` } })).json();
    const sub = await seedSubscription(request, { category_id: String(category.id) });
    try {
      await page.goto('/settings');
      const row = page.locator('#categories-list > div', { has: page.locator(`#category-name-${category.id}`) });
      const alert = new Promise((resolve) => {
        page.on('dialog', (dialog) => {
          if (dialog.type() === 'confirm') return dialog.accept();
          resolve(dialog.message());
          return dialog.accept();
        });
      });
      await row.getByRole('button', { name: 'Delete' }).click();
      expect(await alert).toContain('cannot delete category with active subscriptions');
      await expect(page.locator(`#category-name-${category.id}`)).toBeVisible();
      expect(await categoryByName(request, category.name)).toBeTruthy();
    } finally {
      await request.delete(`/api/subscriptions/${sub.id}`);
      await request.delete(`/api/categories/${category.id}`);
    }
  });
});

test.describe('API keys', () => {
  test('creates a key that is shown once, works, and can be deleted after confirming', async ({ page, request }) => {
    const name = `E2E key ${Date.now()}`;
    let keyID = '';
    try {
      await page.goto('/settings');
      const list = page.locator('#api-keys-list');
      await page.locator('#api_key_name').fill(name);
      await page.locator('form[hx-post="/api/settings/apikeys"] button[type="submit"]').click();

      const row = list.locator('> div', { hasText: name });
      await expect(row).toBeVisible();
      await expect(row.getByText('New', { exact: true })).toBeVisible();
      await expect(row).toContainText("You won't be able to see it again!");
      const key = (await row.locator('code').textContent())?.trim() ?? '';
      expect(key).toMatch(/^sk_[0-9a-f]{64}$/);
      keyID = ((await row.locator('button[hx-delete]').getAttribute('hx-delete')) ?? '').split('/').pop() ?? '';

      const withKey = { headers: { Authorization: `Bearer ${key}` } };
      expect((await request.get('/api/v1/subscriptions', withKey)).status()).toBe(200);

      // After a reload the key value is gone for good, but its usage is shown.
      await page.reload();
      await expect(row).toBeVisible();
      await expect(row.locator('code')).toHaveCount(0);
      await expect(row.getByText('New', { exact: true })).toHaveCount(0);
      await expect(row).toContainText('Usage: 1 requests');
      expect(await page.content()).not.toContain(key);

      // Dismissing the confirm keeps the key.
      page.once('dialog', (dialog) => dialog.dismiss());
      await row.locator('button[hx-delete]').click();
      await expect(row).toBeVisible();
      expect((await request.get('/api/v1/subscriptions', withKey)).status()).toBe(200);

      page.once('dialog', (dialog) => {
        expect(dialog.message()).toBe('Are you sure you want to delete this API key?');
        dialog.accept();
      });
      await row.locator('button[hx-delete]').click();
      await expect(row).toHaveCount(0);
      expect((await request.get('/api/v1/subscriptions', withKey)).status()).toBe(401);
      keyID = '';
    } finally {
      if (keyID) await request.delete(`/api/settings/apikeys/${keyID}`);
    }
  });
});
