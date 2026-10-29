import { expect, test } from "@playwright/test";

const projectId = "7a88fb47-b4e2-43b8-a06c-a5ce950dc53a";
const successfulRunId = "run_cf02f26ab6f6a99f05f42d96e952636a";

test("shows the configured successful agent run and its native Trace link", async ({
  page,
}) => {
  await page.goto("/auth/sign-in");
  await page.fill('input[name="email"]', "demo@langfuse.com");
  await page.fill('input[type="password"]', "password");
  await page.click('button[data-testid="submit-email-password-sign-in-form"]');
  await expect(page).toHaveURL("/");

  await page.goto(`/project/${projectId}/browser-runs`);
  await expect(page.locator('[data-testid="page-header-title"]')).toHaveText(
    "Agent runs",
  );
  await expect(
    page.getByRole("button", { name: "New agent run" }),
  ).toBeVisible();
  await expect(page.getByText(successfulRunId)).toBeVisible();
  await page.getByText(successfulRunId).click();
  await expect(page.getByText("Durable status: succeeded")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Case evidence", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/^Dry run /).first()).toBeVisible();
  await expect(
    page.locator('img[alt^="Browser evidence"]').first(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "View Langfuse Trace" }),
  ).toHaveAttribute(
    "href",
    `/project/${projectId}/traces/agenttrace-trace-${successfulRunId}`,
  );
  await page.getByRole("link", { name: "View Langfuse Trace" }).click();
  await expect(page.getByText("AgentTrace evidence")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Case evidence", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/^Dry run /).first()).toBeVisible();
});
