import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ShareCreation } from "../types";
import { UploadSuccess } from "./UploadPage";

vi.mock("qrcode", () => ({ default: { toCanvas: vi.fn(async () => undefined) } }));
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function creation(pairingEnabled: boolean, qrEnabled: boolean): ShareCreation {
  return { share: { id: "share", name: "test", description: "", storageBackend: "s3", status: "active", expiresAt: "2026-08-01T00:00:00Z", downloadLimit: 10, downloadCount: 0, fileCount: 1, totalBytes: 1, passwordProtected: false, createdAt: "2026-07-28T00:00:00Z", updatedAt: "2026-07-28T00:00:00Z", pairingEnabled, qrEnabled }, publicToken: "public", managementToken: "manage", publicUrl: "https://share.example/s/public", managementUrl: "https://share.example/m/manage", recoveryUrl: "https://share.example/resume/manage", uploadTaskStatus: "completed", uploads: [] };
}

describe("upload success optional sharing aids", () => {
  it.each([[true, true], [true, false], [false, true], [false, false]])("renders pairing=%s and QR=%s independently", async (pairingEnabled, qrEnabled) => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ share: creation(pairingEnabled, qrEnabled).share, entries: [], locked: false, pairing: pairingEnabled ? { enabled: true, code: "123456", expiresAt: "2026-07-28T00:10:00Z" } : { enabled: false } }), { status: 200, headers: { "Content-Type": "application/json" } })));
    const { container } = render(<UploadSuccess creation={creation(pairingEnabled, qrEnabled)} />);
    if (pairingEnabled) expect(await screen.findByText("123456")).toBeInTheDocument(); else await waitFor(() => expect(screen.queryByText("123456")).not.toBeInTheDocument());
    expect(Boolean(container.querySelector("canvas"))).toBe(qrEnabled);
    expect(container.querySelector(".success-share-grid")).toHaveClass(qrEnabled ? "has-qr" : "success-share-grid");
    if (!qrEnabled) expect(container.querySelector(".success-share-grid")).not.toHaveClass("has-qr");
  });
});
