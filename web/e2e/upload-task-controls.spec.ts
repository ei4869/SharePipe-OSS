import { expect, test } from "@playwright/test";
import { Buffer } from "node:buffer";

const fixture = Buffer.alloc(8 * 1024 * 1024 + 1024, 0x71);

async function delayNextWorkerPart(page: import("@playwright/test").Page): Promise<{ seen: Promise<void> }> {
  let resolveSeen: (() => void) | undefined;
  const seen = new Promise<void>((resolve) => { resolveSeen = resolve; });
  let delayed = false;
  await page.route("**/api/v1/uploads/*/parts/*", async (route) => {
    if (!delayed && route.request().method() === "PUT") {
      delayed = true;
      resolveSeen?.();
      await new Promise((resolve) => setTimeout(resolve, 1500));
    }
    try { await route.continue(); } catch { /* AbortController may cancel the paused request. */ }
  });
  return { seen };
}

test.beforeEach(() => {
  test.skip(process.env.SHAREPIPE_E2E_UPLOAD_TASKS !== "1", "Set SHAREPIPE_E2E_UPLOAD_TASKS=1 for upload controls acceptance");
});

test("persists pause across refresh and keeps controls responsive after resume", async ({ page, request }) => {
  test.setTimeout(180_000);
  const { seen: partSeen } = await delayNextWorkerPart(page);
  let managementToken = "";
  try {
    await page.goto("/");
    await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({ name: "pause-resume.bin", mimeType: "application/octet-stream", buffer: fixture });
    await page.getByRole("textbox", { name: "分享名称" }).fill("暂停恢复浏览器验收");
    const createdResponse = page.waitForResponse((response) => response.url().endsWith("/api/v1/shares") && response.request().method() === "POST");
    await page.getByRole("button", { name: "开始上传", exact: true }).click();
    const created = await (await createdResponse).json() as { share: { id: string }; managementToken: string; recoveryUrl: string };
    managementToken = created.managementToken;
    await partSeen;
    await page.getByRole("button", { name: "暂停上传", exact: true }).click();
    await expect(page.getByRole("button", { name: "继续上传", exact: true })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe(new URL(created.recoveryUrl).pathname);

    await page.reload();
    await expect(page.getByRole("heading", { name: "继续未完成上传" })).toBeVisible();
    await expect(page.getByRole("button", { name: "继续上传", exact: true })).toBeVisible();
    await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({ name: "pause-resume.bin", mimeType: "application/octet-stream", buffer: fixture });
    const { seen: resumedPartSeen } = await delayNextWorkerPart(page);
    await page.getByRole("button", { name: "继续上传", exact: true }).click();
    await resumedPartSeen;
    await expect(page.getByRole("button", { name: "暂停上传", exact: true })).toBeEnabled({ timeout: 1_000 });
    await page.getByRole("button", { name: "暂停上传", exact: true }).click();
    await expect(page.getByRole("button", { name: "继续上传", exact: true })).toBeEnabled();
    await page.getByRole("button", { name: "继续上传", exact: true }).click();
    await expect(page.getByRole("heading", { name: "分享已创建" })).toBeVisible({ timeout: 120_000 });
  } finally {
    if (managementToken) await request.delete(`/api/v1/manage/${encodeURIComponent(managementToken)}`);
  }
});

test("keeps cancel responsive after resume and invalidates the recovery endpoint", async ({ page, request }) => {
  test.setTimeout(120_000);
  const { seen: partSeen } = await delayNextWorkerPart(page);
  await page.goto("/");
  await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({ name: "cancel.bin", mimeType: "application/octet-stream", buffer: fixture });
  await page.getByRole("textbox", { name: "分享名称" }).fill("取消上传浏览器验收");
  const createdResponse = page.waitForResponse((response) => response.url().endsWith("/api/v1/shares") && response.request().method() === "POST");
  await page.getByRole("button", { name: "开始上传", exact: true }).click();
  const created = await (await createdResponse).json() as { managementToken: string };
  await partSeen;
  await page.getByRole("button", { name: "暂停上传", exact: true }).click();
  const { seen: resumedPartSeen } = await delayNextWorkerPart(page);
  await page.getByRole("button", { name: "继续上传", exact: true }).click();
  await resumedPartSeen;
  await expect(page.getByRole("button", { name: "取消", exact: true })).toBeEnabled({ timeout: 1_000 });
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("heading", { name: "创建临时分享" })).toBeVisible();
  const recovery = await request.get("/api/v1/upload-recovery", { headers: { Authorization: `Bearer ${created.managementToken}` } });
  expect(recovery.status()).toBe(410);
});
