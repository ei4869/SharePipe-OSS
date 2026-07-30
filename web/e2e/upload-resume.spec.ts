import { expect, test } from "@playwright/test";
import { Buffer } from "node:buffer";

test("recovers an interrupted multipart upload after a full page refresh", async ({ page, request }) => {
  test.skip(process.env.SHAREPIPE_E2E_RESUME !== "1", "Set SHAREPIPE_E2E_RESUME=1 for the stateful recovery acceptance test");
  test.setTimeout(300_000);
  const invitationCode = process.env.SHAREPIPE_E2E_INVITE_CODE;
  if (!invitationCode) throw new Error("SHAREPIPE_E2E_INVITE_CODE is required");

  const runID = process.env.SHAREPIPE_E2E_RUN_ID ?? Date.now().toString();
  const fileName = `resume-${runID}.bin`;
  const fixture = Buffer.alloc(8 * 1024 * 1024 + 1024, 0x6b);
  fixture.write("SharePipe resumable upload", 0, "utf8");
  let managementToken = "";
  let interrupted = false;

  await page.route("**/*", async (route) => {
    const outgoing = route.request();
    const url = new URL(outgoing.url());
    const isSecondWorkerPart = outgoing.method() === "PUT" && /\/api\/v1\/uploads\/[^/]+\/parts\/2$/u.test(url.pathname);
    const isSecondDirectPart = outgoing.method() === "PUT" && url.searchParams.get("partNumber") === "2";
    if (!interrupted && (isSecondWorkerPart || isSecondDirectPart)) {
      interrupted = true;
      await route.abort("failed");
      return;
    }
    await route.continue();
  });

  try {
    await page.goto("/");
    await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({
      name: fileName,
      mimeType: "application/octet-stream",
      buffer: fixture,
    });
    await page.getByRole("textbox", { name: "邀请码" }).fill(invitationCode);
    await page.getByRole("textbox", { name: "分享名称" }).fill(`断点续传验收 ${runID}`);
    const creationPromise = page.waitForResponse(
      (response) => response.url().endsWith("/api/v1/shares") && response.request().method() === "POST",
    );
    const firstPartAcknowledged = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.ok() && (
        /\/api\/v1\/uploads\/[^/]+\/parts\/1$/u.test(url.pathname)
        || /\/api\/v1\/uploads\/[^/]+\/s3-parts\/1\/ack$/u.test(url.pathname)
      );
    });
    await page.getByRole("button", { name: "开始上传", exact: true }).click();
    const creation = await (await creationPromise).json() as { managementToken: string; recoveryUrl: string };
    managementToken = creation.managementToken;
    await firstPartAcknowledged;
    await expect.poll(() => interrupted).toBeTruthy();
    await expect.poll(() => new URL(page.url()).pathname).toBe(new URL(creation.recoveryUrl).pathname);

    await page.reload();
    await expect(page.getByRole("heading", { name: "继续未完成上传" })).toBeVisible();
    await expect(page.getByText(/请重新选择同一原文件或原文件夹/)).toBeVisible();
    await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({
      name: fileName,
      mimeType: "application/octet-stream",
      buffer: fixture,
    });

    const resumedParts: number[] = [];
    page.on("request", (outgoing) => {
      if (outgoing.method() !== "PUT") return;
      const url = new URL(outgoing.url());
      const workerMatch = /\/api\/v1\/uploads\/[^/]+\/parts\/(\d+)$/u.exec(url.pathname);
      const partNumber = workerMatch?.[1] ?? url.searchParams.get("partNumber");
      if (partNumber) resumedParts.push(Number(partNumber));
    });
    await page.getByRole("button", { name: "继续未完成上传", exact: true }).click();
    await expect(page.getByRole("heading", { name: "分享已创建" })).toBeVisible({ timeout: 120_000 });
    expect(resumedParts).toContain(2);
    expect(resumedParts).not.toContain(1);
  } finally {
    if (managementToken) {
      const revoked = await request.delete(`/api/v1/manage/${encodeURIComponent(managementToken)}`);
      expect(revoked.ok()).toBeTruthy();
    }
  }
});
