import { defineConfig } from "@playwright/test";

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: false,
	retries: 0,
	workers: 1,
	outputDir: "../outputs/playwright",
	reporter: "list",
	use: {
		baseURL: process.env.SHAREPIPE_E2E_BASE_URL ?? "http://127.0.0.1:8080",
		browserName: "chromium",
		channel: "chrome",
		headless: true,
		trace: "retain-on-failure",
	},
});
