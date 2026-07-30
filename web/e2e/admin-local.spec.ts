import { expect, test } from "@playwright/test";

test("trusts loopback admin and exposes security and storage settings", async ({ browser }) => {
  const administratorBaseURL = process.env.SHAREPIPE_E2E_ADMIN_BASE_URL;
  test.skip(!administratorBaseURL, "SHAREPIPE_E2E_ADMIN_BASE_URL is required");

  const context = await browser.newContext();
  const page = await context.newPage();
  try {
    const response = await page.goto(`${administratorBaseURL}/admin`);
    expect(response?.status()).toBe(200);
    await expect(page.getByRole("heading", { name: "概览", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "高级安全" }).click();
    await expect(page.getByRole("heading", { name: "高级安全设置" })).toBeVisible();
    await expect(page.getByText("本机可信管理模式")).toBeVisible();
    await page.getByRole("button", { name: "存储连接" }).click();
    await expect(page.getByRole("heading", { name: "存储连接" })).toBeVisible();
    await expect(page.getByText("本地磁盘", { exact: true })).toBeVisible();
  } finally {
    await context.close();
  }
});
