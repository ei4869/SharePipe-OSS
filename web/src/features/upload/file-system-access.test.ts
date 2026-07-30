import { describe, expect, it } from "vitest";
import { readDirectoryHandle, type PersistentDirectoryHandle, type PersistentFileHandle } from "./file-system-access";

function fileHandle(name: string, contents: string): PersistentFileHandle {
  return { kind: "file", name, getFile: async () => new File([contents], name, { type: "text/plain" }) };
}

function directoryHandle(name: string, children: Array<PersistentFileHandle | PersistentDirectoryHandle>): PersistentDirectoryHandle {
  return {
    kind: "directory",
    name,
    async *values() {
      for (const child of children) yield child;
    },
  };
}

describe("directory selection", () => {
  it("preserves all levels of a nested directory path", async () => {
    const root = directoryHandle("root", [
      fileHandle("root.txt", "root"),
      directoryHandle("level1", [
        fileHandle("middle.txt", "middle"),
        directoryHandle("level2", [fileHandle("deep.txt", "deep")]),
      ]),
    ]);

    const selected = await readDirectoryHandle(root);
    expect(selected.map((item) => item.path)).toEqual([
      "root/root.txt",
      "root/level1/middle.txt",
      "root/level1/level2/deep.txt",
    ]);
  });
});
