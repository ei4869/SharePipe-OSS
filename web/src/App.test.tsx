import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

describe("App routing", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            expiryPresets: ["1h", "24h", "168h", "720h"],
            defaultExpiry: "168h",
            defaultDownloadLimit: 10,
            chunkSize: 8388608,
            maxFileBytes: 10737418240,
            maxShareBytes: 21474836480,
            maxFilesPerShare: 1000,
            storageBackend: "local",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
  });

  it("renders the uploader at the root path", async () => {
    render(<App />);
    expect(await screen.findByRole("heading", { name: "创建临时分享" })).toBeInTheDocument();
    expect(screen.getByText("SharePipe")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "管理后台" })).not.toBeInTheDocument();
  });

  it("keeps an unfinished upload visible after refreshing its recovery URL", async () => {
    window.history.replaceState({}, "", "/resume/management-token");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/public/config")) {
          return new Response(JSON.stringify({
            expiryPresets: ["168h"],
            defaultExpiry: "168h",
            defaultDownloadLimit: 10,
            chunkSize: 8388608,
            maxFileBytes: 10737418240,
            maxShareBytes: 21474836480,
            maxFilesPerShare: 1000,
            storageBackend: "s3",
          }), { status: 200, headers: { "Content-Type": "application/json" } });
        }
        return new Response(JSON.stringify({
          share: {
            id: "share-one",
            name: "刷新恢复测试",
            description: "",
            storageBackend: "s3",
            status: "uploading",
            expiresAt: "2026-08-01T00:00:00.000Z",
            downloadLimit: 10,
            downloadCount: 0,
            fileCount: 1,
            totalBytes: 10,
            passwordProtected: false,
            createdAt: "2026-07-28T00:00:00.000Z",
            updatedAt: "2026-07-28T00:00:00.000Z",
          },
          publicToken: "public-token",
          managementToken: "management-token",
          publicUrl: "https://share.example/s/public-token",
          managementUrl: "https://share.example/m/management-token",
          recoveryUrl: "https://share.example/resume/management-token",
          uploads: [{
            sessionId: "session-one",
            entryId: "entry-one",
            path: "large.bin",
            token: "management-token",
            backend: "s3",
            chunkSize: 5,
            partCount: 2,
            expectedSize: 10,
            receivedBytes: 5,
            status: "open",
            parts: [{ partNumber: 1, size: 5, etag: "etag-one", sha256: "a".repeat(64) }],
          }],
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }),
    );

    render(<App />);
    expect(await screen.findByRole("heading", { name: "继续未完成上传" })).toBeInTheDocument();
    expect(screen.getByText("刷新恢复测试")).toBeInTheDocument();
    expect(screen.getByDisplayValue("https://share.example/resume/management-token")).toBeInTheDocument();
    expect(screen.getByText(/请重新选择同一原文件或原文件夹/)).toBeInTheDocument();
  });
});
