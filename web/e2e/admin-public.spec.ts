import { expect, test } from "@playwright/test";

test("public admin supports app authentication without exposing advanced security", async ({ page, request }) => {
	const sessionResponse = await request.get("/api/v1/admin/session");
	expect(sessionResponse.status()).toBe(200);
	const session = (await sessionResponse.json()) as {
		authenticated: boolean;
		local: boolean;
		passwordConfigured: boolean;
	};
	expect(session).toMatchObject({ authenticated: false, local: false });

	await page.goto("/admin");
	await expect(page.getByRole("heading", { name: "登录管理后台", exact: true })).toBeVisible();
	await expect(page.getByRole("link", { name: "使用管理员邮箱认证", exact: true })).toHaveAttribute(
		"href",
		"/admin/email-login",
	);
	await expect(page.getByRole("button", { name: "高级安全", exact: true })).toHaveCount(0);

	if (session.passwordConfigured) {
		await expect(page.getByPlaceholder("管理员密码", { exact: true })).toBeVisible();
	} else {
		await expect(page.getByText(/管理员密码尚未设置/)).toBeVisible();
	}

	const overview = await request.get("/api/v1/admin/overview");
	expect(overview.status()).toBe(401);
	const security = await request.get("/api/v1/admin/security");
	expect(security.status()).toBe(404);
	const control = await request.get("/api/v1/control/security");
	expect(control.status()).toBe(404);
	const emailLogin = await request.get("/admin/email-login", { maxRedirects: 0 });
	expect(emailLogin.status()).toBe(302);
	expect(emailLogin.headers().location).toContain("cloudflareaccess.com");
});
