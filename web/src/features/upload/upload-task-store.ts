import type { PickedFile } from "./types";
import type { PersistentFileHandle } from "./file-system-access";

const metadataKey = "sharepipe.pending-uploads.v1";
const databaseName = "sharepipe-upload-recovery";
const handleStore = "file-handles";

export interface PendingUploadTask {
  version: 1;
  shareId: string;
  recoveryToken: string;
  recoveryUrl: string;
  name: string;
  createdAt: string;
}

interface StoredHandle {
  id: string;
  shareId: string;
  path: string;
  handle: PersistentFileHandle;
}

function readTasks(): PendingUploadTask[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(metadataKey) ?? "[]") as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item): item is PendingUploadTask => {
      if (!item || typeof item !== "object") return false;
      const task = item as Partial<PendingUploadTask>;
      return task.version === 1 && typeof task.shareId === "string" && typeof task.recoveryToken === "string";
    });
  } catch {
    return [];
  }
}

function writeTasks(tasks: PendingUploadTask[]): void {
  try {
    localStorage.setItem(metadataKey, JSON.stringify(tasks.slice(0, 20)));
  } catch {
    // Private browsing or a locked-down browser may reject persistent storage.
  }
}

export function savePendingUpload(task: PendingUploadTask): void {
  writeTasks([task, ...readTasks().filter((item) => item.shareId !== task.shareId)]);
}

export function latestPendingUpload(): PendingUploadTask | null {
  return readTasks()[0] ?? null;
}

export function removePendingUpload(shareId: string): void {
  writeTasks(readTasks().filter((item) => item.shareId !== shareId));
  void clearFileHandles(shareId);
}

function openDatabase(): Promise<IDBDatabase | null> {
  if (!("indexedDB" in window)) return Promise.resolve(null);
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName, 1);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(handleStore)) {
        request.result.createObjectStore(handleStore, { keyPath: "id" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function complete(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error);
    transaction.onerror = () => reject(transaction.error);
  });
}

export async function saveFileHandles(shareId: string, files: PickedFile[]): Promise<void> {
  const database = await openDatabase();
  if (!database) return;
  const transaction = database.transaction(handleStore, "readwrite");
  const store = transaction.objectStore(handleStore);
  for (const item of files) {
    if (!item.handle) continue;
    const record: StoredHandle = {
      id: `${shareId}\u0000${item.path}`,
      shareId,
      path: item.path,
      handle: item.handle,
    };
    store.put(record);
  }
  await complete(transaction);
  database.close();
}

export async function loadFileHandles(shareId: string): Promise<Map<string, PersistentFileHandle>> {
  const database = await openDatabase();
  if (!database) return new Map();
  const transaction = database.transaction(handleStore, "readonly");
  const request = transaction.objectStore(handleStore).getAll();
  const records = await new Promise<StoredHandle[]>((resolve, reject) => {
    request.onsuccess = () => resolve(request.result as StoredHandle[]);
    request.onerror = () => reject(request.error);
  });
  await complete(transaction);
  database.close();
  return new Map(records.filter((item) => item.shareId === shareId).map((item) => [item.path, item.handle]));
}

async function clearFileHandles(shareId: string): Promise<void> {
  try {
    const database = await openDatabase();
    if (!database) return;
    const transaction = database.transaction(handleStore, "readwrite");
    const store = transaction.objectStore(handleStore);
    const request = store.getAllKeys();
    const keys = await new Promise<IDBValidKey[]>((resolve, reject) => {
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    for (const key of keys) {
      if (typeof key === "string" && key.startsWith(`${shareId}\u0000`)) store.delete(key);
    }
    await complete(transaction);
    database.close();
  } catch {
    // Cleanup failure must not turn a completed upload into an error.
  }
}
