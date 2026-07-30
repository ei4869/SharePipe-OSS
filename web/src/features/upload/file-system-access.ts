export type HandlePermissionState = "granted" | "denied" | "prompt";

export interface PersistentFileHandle {
  readonly kind: "file";
  readonly name: string;
  getFile(): Promise<File>;
  queryPermission?(options?: { mode: "read" }): Promise<HandlePermissionState>;
  requestPermission?(options?: { mode: "read" }): Promise<HandlePermissionState>;
}

export interface PersistentDirectoryHandle {
  readonly kind: "directory";
  readonly name: string;
  values(): AsyncIterableIterator<PersistentFileHandle | PersistentDirectoryHandle>;
}

interface PickerWindow extends Window {
  showOpenFilePicker?: (options?: { multiple?: boolean }) => Promise<PersistentFileHandle[]>;
  showDirectoryPicker?: () => Promise<PersistentDirectoryHandle>;
}

export function fileSystemPicker(): PickerWindow {
  return window as PickerWindow;
}

export async function readDirectoryHandle(
  directory: PersistentDirectoryHandle,
  prefix = "",
): Promise<Array<{ file: File; path: string; handle: PersistentFileHandle }>> {
  const base = prefix ? `${prefix}/${directory.name}` : directory.name;
  const output: Array<{ file: File; path: string; handle: PersistentFileHandle }> = [];
  for await (const handle of directory.values()) {
    if (handle.kind === "file") {
      output.push({ file: await handle.getFile(), path: `${base}/${handle.name}`, handle });
    } else {
      output.push(...(await readDirectoryHandle(handle, base)));
    }
  }
  return output;
}
