import { expect, test } from "@playwright/test";

test("未登录用户会被路由保护送到登录页", async ({ page }) => {
  await page.route("**/api/v1/auth/me", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ detail: "Unauthorized" }),
    }),
  );

  await page.goto("/reports");

  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "AI Web Testing" })).toBeVisible();
});

test("已登录用户可进入回归编排并导航到定位调试", async ({ page }) => {
  await page.route("**/api/v1/auth/me", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        id: 1,
        email: "smoke@example.com",
        display_name: "Smoke",
      }),
    }),
  );
  await page.route("**/api/v1/projects", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify([{ id: 1, name: "示例项目", description: null }]),
    }),
  );
  await page.route("**/api/v1/cases?**", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
        total_pages: 0,
        has_next: false,
        has_prev: false,
      }),
    }),
  );
  await page.route("**/api/v1/execution-batches?**", (route) =>
    route.fulfill({ contentType: "application/json", body: "[]" }),
  );
  await page.route("**/api/v1/executions?**", (route) =>
    route.fulfill({ contentType: "application/json", body: "[]" }),
  );

  await page.goto("/regression");

  await expect(page.getByRole("heading", { name: "项目回归编排" })).toBeVisible();
  await expect(page.getByRole("button", { name: /启动回归/ })).toBeDisabled();

  await page.getByRole("button", { name: /定位调试/ }).click();
  await expect(page).toHaveURL(/\/locator-debug$/);
  await expect(page.getByRole("heading", { name: "定位调试" })).toBeVisible();
});
