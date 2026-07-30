import { describe, expect, it } from "vitest";
import type { PersistentFileHandle } from "../features/upload/file-system-access";
import type { ShareCreation } from "../types";
import { materializeHandles } from "./UploadPage";

function creation(): ShareCreation {
  return {
    share: {
      id: "share-one",
      name: "Handle restore",
      description: "",
      storageBackend: "s3",
      status: "uploading",
      expiresAt: "2026-08-01T00:00:00.000Z",
      downloadLimit: 10,
      downloadCount: 0,
      fileCount: 1,
      totalBytes: 5,
    passwordProtected: false,
    pairingEnabled: true,
    qrEnabled: true,
      createdAt: "2026-07-28T00:00:00.000Z",
      updatedAt: "2026-07-28T00:00:00.000Z",
    },
    publicToken: "public",
    managementToken: "manage",
    publicUrl: "https://share.example/s/public",
    managementUrl: "https://share.example/m/manage",
    recoveryUrl: "https://share.example/resume/manage",
    uploadTaskStatus: "uploading",
    uploads: [{
      sessionId: "session-one",
      entryId: "entry-one",
      path: "folder/test.txt",
      token: "manage",
      backend: "s3",
      chunkSize: 5,
      partCount: 1,
      expectedSize: 5,
      receivedBytes: 0,
      status: "open",
      parts: [],
    }],
  };
}

describe("stored browser file handles", () => {
  it("materializes a granted handle without asking the user to reselect the file", async () => {
    const file = new File(["hello"], "test.txt", { type: "text/plain" });
    const handle: PersistentFileHandle = {
      kind: "file",
      name: file.name,
      queryPermission: async () => "granted",
      getFile: async () => file,
    };
    const restored = await materializeHandles(creation(), new Map([["folder/test.txt", handle]]));
    expect(restored.pending.size).toBe(0);
    expect(restored.files).toHaveLength(1);
    expect(restored.files[0]).toMatchObject({ path: "folder/test.txt", file, status: "pending" });
  });

  it("keeps a prompt-state handle for the one-click authorization path", async () => {
    const handle: PersistentFileHandle = {
      kind: "file",
      name: "test.txt",
      queryPermission: async () => "prompt",
      requestPermission: async () => "granted",
      getFile: async () => new File(["hello"], "test.txt"),
    };
    const restored = await materializeHandles(creation(), new Map([["folder/test.txt", handle]]));
    expect(restored.files).toHaveLength(0);
    expect(restored.pending.get("folder/test.txt")).toBe(handle);
  });
});
