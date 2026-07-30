import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Entry, PublicShare } from "../types";
import { SharePage } from "./SharePage";

const now = "2026-07-28T00:00:00Z";
const entry = (id: string, path: string, kind: "file" | "directory"): Entry => ({
  id,
  path,
  kind,
  size: kind === "file" ? 4 : 0,
  mimeType: kind === "file" ? "application/octet-stream" : "inode/directory",
  status: "ready",
  createdAt: now,
});

const nestedShare: PublicShare = {
  share: {
    id: "nested",
    name: "Nested folders",
    description: "",
    storageBackend: "s3",
    status: "active",
    expiresAt: "2026-08-28T00:00:00Z",
    downloadLimit: 10,
    downloadCount: 0,
    fileCount: 3,
    totalBytes: 12,
    passwordProtected: false,
    createdAt: now,
    updatedAt: now,
    pairingEnabled: false,
    qrEnabled: false,
  },
  entries: [
    entry("root", "root", "directory"),
    entry("level1", "root/level1", "directory"),
    entry("level2", "root/level1/level2", "directory"),
    entry("deep", "root/level1/level2/deep.txt", "file"),
    entry("middle", "root/level1/middle.txt", "file"),
    entry("top", "root/root.txt", "file"),
  ],
  locked: false,
};

describe("nested share tree", () => {
  beforeEach(() => {
    vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(nestedShare), { status: 200, headers: { "Content-Type": "application/json" } })));
  });
  afterEach(() => { cleanup(); vi.restoreAllMocks(); });

  it("renders every directory level and lets folders collapse and expand their descendants", async () => {
    const { container } = render(<SharePage token="public-token" />);
    const level1 = await waitFor(() => {
      const value = container.querySelector<HTMLButtonElement>('button[title="root/level1"]');
      expect(value).toBeInTheDocument();
      return value!;
    });
    expect(level1).toHaveAttribute("aria-expanded", "true");
    expect(container.querySelector('button[title="root/level1/level2/deep.txt"]')).toBeInTheDocument();

    fireEvent.click(level1);
    await waitFor(() => expect(container.querySelector('button[title="root/level1/level2"]')).not.toBeInTheDocument());
    expect(level1).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(level1);
    await waitFor(() => expect(container.querySelector('button[title="root/level1/level2/deep.txt"]')).toBeInTheDocument());
    expect(level1).toHaveAttribute("aria-expanded", "true");
  });
});
