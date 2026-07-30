import type { PersistentFileHandle } from "./file-system-access";

export interface PickedFile {
  id: string;
  file: File;
  path: string;
  progress: number;
  status: "pending" | "uploading" | "completed" | "error";
  error?: string;
  handle?: PersistentFileHandle;
}
