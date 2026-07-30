import { expect, test } from "@playwright/test";
import { Buffer } from "node:buffer";

test("loopback trusted upload creates a pair code and QR for another browser", async ({ browser, page, request }, testInfo) => {
  test.skip(process.env.SHAREPIPE_E2E_TRUSTED_PAIRING !== "1", "Set SHAREPIPE_E2E_TRUSTED_PAIRING=1 for this stateful local acceptance test");
  test.setTimeout(120_000);

  const runID = process.env.SHAREPIPE_E2E_RUN_ID ?? Date.now().toString();
  const shareName = `可信设备与配对验收 ${runID}`;
  const fileName = `pairing-${runID}.txt`;
  const fixture = Buffer.from(`SharePipe trusted pairing acceptance ${runID}\n`, "utf8");
  let managementToken = "";
  const browserErrors: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") browserErrors.push(message.text()); });
  page.on("pageerror", (error) => browserErrors.push(error.message));

  try {
    await page.goto("/");
    await expect(page.getByText("可信设备", { exact: true })).toBeVisible();
    await expect(page.getByText("本机", { exact: true })).toBeVisible();
    await expect(page.getByRole("textbox", { name: "邀请码", exact: true })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "配对码接收文件", exact: true }).first()).toBeVisible();

    await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({
      name: fileName,
      mimeType: "text/plain",
      buffer: fixture,
    });
    await page.getByRole("textbox", { name: "分享名称", exact: true }).fill(shareName);
    const creationResponsePromise = page.waitForResponse(
      (response) => response.url().endsWith("/api/v1/shares") && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "开始上传", exact: true }).click();
    const creationResponse = await creationResponsePromise;
    expect(creationResponse.status()).toBe(201);
    const creation = await creationResponse.json() as {
      publicUrl: string;
      managementToken: string;
    };
    managementToken = creation.managementToken;

    await expect(page.getByRole("heading", { name: "分享已创建", exact: true })).toBeVisible();
    const code = (await page.locator(".pair-code-card strong").textContent())?.trim() ?? "";
    expect(code).toMatch(/^\d{6}$/u);
    await expect(page.getByLabel("公开分享链接二维码")).toBeVisible();
    await expect.poll(() => page.getByLabel("公开分享链接二维码").evaluate((canvas: HTMLCanvasElement) => canvas.toDataURL("image/png").length)).toBeGreaterThan(100);
    await page.screenshot({ path: testInfo.outputPath("trusted-upload-success-desktop.png"), fullPage: true });

    const receiver = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const receiverPage = await receiver.newPage();
    try {
      await receiverPage.goto("/pair");
      await expect(receiverPage.getByRole("heading", { name: "配对码接收文件", exact: true })).toBeVisible();
      await receiverPage.screenshot({ path: testInfo.outputPath("pair-entry-mobile.png"), fullPage: true });
      await receiverPage.getByLabel("六位配对码").fill(code);
      await receiverPage.getByRole("button", { name: "接收文件", exact: true }).click();
      await expect(receiverPage.getByRole("heading", { name: shareName, exact: true })).toBeVisible();
      expect(new URL(receiverPage.url()).pathname).not.toBe(new URL(creation.publicUrl).pathname);
      await expect(receiverPage.getByRole("button").filter({ hasText: fileName }).first()).toBeVisible();
      await receiverPage.screenshot({ path: testInfo.outputPath("paired-share-mobile.png"), fullPage: true });
    } finally {
      await receiver.close();
    }

    await page.goto(`/m/${encodeURIComponent(managementToken)}`);
    await expect(page.getByRole("heading", { name: "快速配对", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "重新生成", exact: true }).click();
    await expect(page.getByText("已生成新的六位配对码", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "关闭配对码", exact: true }).click();
    await expect(page.getByText("快速配对已关闭。", { exact: true })).toBeVisible();

    await page.goto("/admin");
    await page.getByRole("button", { name: "可信设备", exact: true }).click();
    await expect(page.getByRole("heading", { name: "可信设备", exact: true })).toBeVisible();
    await expect(page.locator(".trusted-device-list").getByText("本机", { exact: true }).first()).toBeVisible();
    expect(browserErrors).toEqual([]);
  } finally {
    if (managementToken) {
      const revoked = await request.delete(`/api/v1/manage/${encodeURIComponent(managementToken)}`);
      expect(revoked.ok()).toBeTruthy();
    }
  }
});
