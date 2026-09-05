import { expect, test } from "@playwright/test";

test("登录路由直接进入规划页", async ({ page }) => {
  await page.route("**/api/v2/planning/sessions", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: "[]",
    }),
  );

  await page.goto("/login");

  await expect(page).toHaveURL(/\/planning$/);
  await expect(page.getByRole("heading", { name: "AI 测试规划" })).toBeVisible();
});

test("可进入回归编排并导航到定位调试", async ({ page }) => {
  await page.route("**/api/v2/projects", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify([{ id: 1, name: "示例项目", description: null }]),
    }),
  );
  await page.route("**/api/v2/cases?**", (route) =>
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
  await page.route("**/api/v2/execution-batches?**", (route) =>
    route.fulfill({ contentType: "application/json", body: "[]" }),
  );
  await page.route("**/api/v2/executions?**", (route) =>
    route.fulfill({ contentType: "application/json", body: "[]" }),
  );

  await page.goto("/regression");

  await expect(page.getByRole("heading", { name: "项目回归编排" })).toBeVisible();
  await expect(page.getByRole("button", { name: /启动回归/ })).toBeDisabled();

  await page.getByRole("button", { name: /定位调试/ }).click();
  await expect(page).toHaveURL(/\/locator-debug$/);
  await expect(page.getByRole("heading", { name: "定位调试" })).toBeVisible();
});
