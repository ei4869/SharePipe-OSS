import { expect, test } from "@playwright/test";
import { Buffer } from "node:buffer";

test("uploads through a browser to the configured S3 backend", async ({ browser, page, request }, testInfo) => {
	test.setTimeout(600_000);
	const invitationCode = process.env.SHAREPIPE_E2E_INVITE_CODE;
	const runID = process.env.SHAREPIPE_E2E_RUN_ID;
	if (!invitationCode || !runID) {
		throw new Error("SHAREPIPE_E2E_INVITE_CODE and SHAREPIPE_E2E_RUN_ID are required");
	}

	const shareName = `S3 Playwright 直传验收 ${runID}`;
	const fileName = `r2-multipart-${runID}.bin`;
	const defaultFixtureBytes = 8 * 1024 * 1024 + 257;
	const fixtureBytes = Number(process.env.SHAREPIPE_E2E_FIXTURE_BYTES ?? defaultFixtureBytes);
	if (!Number.isSafeInteger(fixtureBytes) || fixtureBytes < defaultFixtureBytes || fixtureBytes > 128 * 1024 * 1024) {
		throw new Error("SHAREPIPE_E2E_FIXTURE_BYTES must be an integer between 8 MiB + 257 bytes and 128 MiB");
	}
	const fixture = Buffer.alloc(fixtureBytes, 0x5a);
	fixture.write("SharePipe-R2-start", 0, "utf8");
	fixture.write("SharePipe-R2-boundary", 8 * 1024 * 1024 - 8, "utf8");
	fixture.write("SharePipe-R2-end", fixture.length - 16, "utf8");
	const browserErrors: string[] = [];
	let managementToken = "";
	const expectedTrustedDeviceName = process.env.SHAREPIPE_E2E_EXPECT_TRUSTED_DEVICE_NAME?.trim() ?? "";
	const storageStatePath = process.env.SHAREPIPE_E2E_STORAGE_STATE?.trim() ?? "";
	page.on("console", (message) => {
		if (message.type() !== "error") return;
		const text = message.text();
		// Cloudflare Browser Insights may inject its beacon into proxied HTML. The
		// production CSP intentionally blocks that third-party script.
		if (text.includes("static.cloudflareinsights.com/beacon.min.js") && text.includes("Content Security Policy")) {
			return;
		}
		browserErrors.push(text);
	});
	page.on("pageerror", (error) => browserErrors.push(error.message));

	try {
	await page.goto("/");
	await expect(page).toHaveTitle(/SharePipe/);
	await expect(page.getByRole("heading", { name: "创建临时分享", exact: true })).toBeVisible();
	await expect(page.getByRole("link", { name: "管理后台", exact: true })).toHaveCount(0);
	const expectedAdministratorStatus = Number(process.env.SHAREPIPE_E2E_EXPECT_ADMIN_STATUS ?? "200");
	const publicAdministrator = await request.get("/admin", { maxRedirects: 0 });
	expect(publicAdministrator.status()).toBe(expectedAdministratorStatus);
	await page.locator('input[type="file"]:not([webkitdirectory])').setInputFiles({
		name: fileName,
		mimeType: "application/octet-stream",
		buffer: fixture,
	});
	await page.getByRole("textbox", { name: "邀请码", exact: true }).fill(invitationCode);
	await page.getByRole("textbox", { name: "分享名称", exact: true }).fill(shareName);
	const creationResponsePromise = page.waitForResponse(
		(response) => response.url().endsWith("/api/v1/shares") && response.request().method() === "POST",
	);
	await page.getByRole("button", { name: "开始上传", exact: true }).click();
	const creationResponse = await creationResponsePromise;
	expect(creationResponse.status()).toBe(201);
	const creation = (await creationResponse.json()) as {
		share: { id: string; status: string; storageBackend: string };
		publicToken: string;
		managementToken: string;
		publicUrl: string;
		uploads: Array<{ backend: string; chunkSize: number; partCount: number }>;
	};
	managementToken = creation.managementToken;
	expect(creation.share.storageBackend).toBe("s3");
	expect(creation.uploads).toHaveLength(1);
	const expectedUploadBackend = process.env.SHAREPIPE_E2E_UPLOAD_BACKEND ?? "s3";
	expect(creation.uploads[0]).toMatchObject({
		backend: expectedUploadBackend,
		chunkSize: 8 * 1024 * 1024,
		partCount: Math.ceil(fixture.length / (8 * 1024 * 1024)),
	});

	await expect(page.getByRole("heading", { name: "分享已创建", exact: true })).toBeVisible({
		timeout: 120_000,
	});
	if (expectedTrustedDeviceName) {
		const pairCode = (await page.locator(".pair-code-card strong").textContent())?.trim() ?? "";
		expect(pairCode).toMatch(/^\d{6}$/u);
		await expect(page.getByLabel("公开分享链接二维码")).toBeVisible();

		const receiver = await browser.newContext({ viewport: { width: 390, height: 844 } });
		try {
			const receiverPage = await receiver.newPage();
			await receiverPage.goto(new URL("/pair", page.url()).toString());
			await receiverPage.getByLabel("六位配对码").fill(pairCode);
			await receiverPage.getByRole("button", { name: "接收文件", exact: true }).click();
			await expect(receiverPage.getByRole("heading", { name: shareName, exact: true })).toBeVisible();
			expect(new URL(receiverPage.url()).pathname).not.toBe(new URL(creation.publicUrl).pathname);
		} finally {
			await receiver.close();
		}

		await page.goto("/");
		await expect(page.getByText("可信设备", { exact: true })).toBeVisible();
		await expect(page.getByText(expectedTrustedDeviceName, { exact: true })).toBeVisible();
		await expect(page.getByRole("textbox", { name: "邀请码", exact: true })).toHaveCount(0);
		if (storageStatePath) await page.context().storageState({ path: storageStatePath });
	}
	await page.screenshot({ path: testInfo.outputPath("upload-success.png"), fullPage: false });
	expect(browserErrors).toEqual([]);

	await page.goto(creation.publicUrl);
	await expect(page.getByRole("heading", { name: shareName, exact: true })).toBeVisible();
	await expect(page.getByRole("button").filter({ hasText: fileName }).first()).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath("public-share.png"), fullPage: false });

	const grantResponsePromise = page.waitForResponse(
		(response) => response.url().endsWith("/downloads") && response.request().method() === "POST",
	);
	const downloadPromise = page.waitForEvent("download");
	await page.locator(".preview-pane").getByRole("button", { name: "下载", exact: true }).click();
	const grantResponse = await grantResponsePromise;
	expect(grantResponse.status()).toBe(201);
	const grant = (await grantResponse.json()) as { url: string; status: string };
	expect(grant.status).toBe("ready");
	const download = await downloadPromise;
	const downloadStream = await download.createReadStream();
	const chunks: Buffer[] = [];
	for await (const chunk of downloadStream) chunks.push(Buffer.from(chunk));
	expect(Buffer.concat(chunks).equals(fixture)).toBeTruthy();

	const rangeStart = 8 * 1024 * 1024 - 16;
	const rangeEnd = rangeStart + 63;
	const rangeResponse = await request.get(grant.url, {
		headers: { Range: `bytes=${rangeStart}-${rangeEnd}` },
	});
	expect(rangeResponse.status()).toBe(206);
	expect(rangeResponse.headers()["content-range"]).toBe(
		`bytes ${rangeStart}-${rangeEnd}/${fixture.length}`,
	);
	expect(Buffer.from(await rangeResponse.body()).equals(fixture.subarray(rangeStart, rangeEnd + 1))).toBeTruthy();
	expect(browserErrors).toEqual([]);

	} finally {
		if (managementToken) {
			const revokeShare = await request.delete(
				`/api/v1/manage/${encodeURIComponent(managementToken)}`,
			);
			expect(revokeShare.ok()).toBeTruthy();
		}
	}
});
