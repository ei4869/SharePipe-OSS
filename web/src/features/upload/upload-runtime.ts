import type { ShareCreation, UploadDescriptor } from "../../types";
import { basename } from "../../utils";
import type { PersistentFileHandle } from "./file-system-access";
import type { PickedFile } from "./types";

export async function materializeHandles(
  creation: ShareCreation,
  handles: Map<string, PersistentFileHandle>,
): Promise<{ files: PickedFile[]; pending: Map<string, PersistentFileHandle> }> {
  const files: PickedFile[] = [];
  const pending = new Map<string, PersistentFileHandle>();
  for (const descriptor of creation.uploads) {
    if (descriptor.status === "completed") continue;
    const handle = handles.get(descriptor.path);
    if (!handle) continue;
    const permission = handle.queryPermission ? await handle.queryPermission({ mode: "read" }) : "granted";
    if (permission !== "granted") {
      if (permission === "prompt") pending.set(descriptor.path, handle);
      continue;
    }
    try {
      const file = await handle.getFile();
      if (file.size !== descriptor.expectedSize) continue;
      files.push({
        id: crypto.randomUUID(),
        file,
        path: descriptor.path,
        progress: restoredProgress(descriptor),
        status: "pending",
        handle,
      });
    } catch {
      // The file moved or the browser revoked access. The re-selection fallback remains available.
    }
  }
  return { files, pending };
}

export function mergePickedFiles(current: PickedFile[], additions: PickedFile[]): PickedFile[] {
  const byPath = new Map(current.map((item) => [item.path, item]));
  for (const item of additions) byPath.set(item.path, item);
  return [...byPath.values()];
}

export function hasEveryOpenFile(creation: ShareCreation, files: PickedFile[]): boolean {
  return creation.uploads.every(
    (descriptor) => descriptor.status === "completed" || files.some((file) => file.path === descriptor.path && file.file.size === descriptor.expectedSize),
  );
}

export function matchRecoveredPath(creation: ShareCreation, file: File, path: string, occupied: Set<string>): string {
  const exact = creation.uploads.find((item) => item.path === path && item.expectedSize === file.size);
  if (exact) return exact.path;
  const candidates = creation.uploads.filter(
    (item) => !occupied.has(item.path) && item.expectedSize === file.size && basename(item.path) === file.name,
  );
  return candidates.length === 1 ? candidates[0].path : path;
}

export function restoredProgress(descriptor: UploadDescriptor): number {
  return descriptor.expectedSize === 0 ? 100 : Math.min(100, (descriptor.receivedBytes / descriptor.expectedSize) * 100);
}

export async function sha256Blob(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await crypto.subtle.digest("SHA-256", await blob.arrayBuffer()));
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export function fileKey(file: File, path: string) {
  return [path, file.size, file.lastModified].join(":");
}

export function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
