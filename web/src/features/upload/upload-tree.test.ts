import { describe, expect, it } from "vitest";
import type { PickedFile } from "./types";
import { buildUploadTree, flattenUploadTree } from "./upload-tree";

function picked(path: string, size: number): PickedFile {
  return {
    id: path,
    file: new File([new Uint8Array(size)], path.split("/").at(-1) || path),
    path,
    progress: 0,
    status: "pending",
  };
}

describe("upload tree", () => {
  const files = [
    picked("data/session.key", 10),
    picked("data/backups/sharepipe.db", 20),
    picked("data/storage/objects/nested.bin", 30),
    picked("loose.txt", 40),
  ];

  it("builds nested directories and aggregates their files and sizes", () => {
    const tree = buildUploadTree(files);
    const data = tree.find((node) => node.kind === "directory" && node.path === "data");

    expect(data).toMatchObject({ fileCount: 3, totalBytes: 60 });
    expect(data?.kind === "directory" ? data.children.map((node) => node.name) : []).toEqual([
      "backups",
      "storage",
      "session.key",
    ]);
    expect(flattenUploadTree(tree, new Set()).map(({ node, depth }) => `${depth}:${node.path}`)).toEqual([
      "0:data",
      "1:data/backups",
      "2:data/backups/sharepipe.db",
      "1:data/storage",
      "2:data/storage/objects",
      "3:data/storage/objects/nested.bin",
      "1:data/session.key",
      "0:loose.txt",
    ]);
  });

  it("hides every descendant of a collapsed directory without losing sibling files", () => {
    const visible = flattenUploadTree(buildUploadTree(files), new Set(["data/storage"]));
    expect(visible.map(({ node }) => node.path)).toEqual([
      "data",
      "data/backups",
      "data/backups/sharepipe.db",
      "data/storage",
      "data/session.key",
      "loose.txt",
    ]);
  });

  it("normalizes Windows separators into the same directory hierarchy", () => {
    const visible = flattenUploadTree(
      buildUploadTree([picked("data\\storage\\windows.bin", 5)]),
      new Set(),
    );
    expect(visible.map(({ node }) => node.path)).toEqual([
      "data",
      "data/storage",
      "data/storage/windows.bin",
    ]);
  });
});
