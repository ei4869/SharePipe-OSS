import type { PickedFile } from "./types";

export interface UploadTreeDirectoryNode {
  kind: "directory";
  name: string;
  path: string;
  children: UploadTreeNode[];
  fileCount: number;
  totalBytes: number;
}

export interface UploadTreeFileNode {
  kind: "file";
  name: string;
  path: string;
  item: PickedFile;
}

export type UploadTreeNode = UploadTreeDirectoryNode | UploadTreeFileNode;

export interface VisibleUploadTreeNode {
  node: UploadTreeNode;
  depth: number;
}

interface MutableDirectoryNode {
  kind: "directory";
  name: string;
  path: string;
  directories: Map<string, MutableDirectoryNode>;
  files: UploadTreeFileNode[];
}

export function buildUploadTree(files: PickedFile[]): UploadTreeNode[] {
  const root: MutableDirectoryNode = {
    kind: "directory",
    name: "",
    path: "",
    directories: new Map(),
    files: [],
  };

  for (const item of files) {
    const normalizedPath = normalizePath(item.path || item.file.name);
    const parts = normalizedPath.split("/").filter(Boolean);
    const fileName = parts.pop() || item.file.name;
    let directory = root;

    for (const part of parts) {
      const path = directory.path ? `${directory.path}/${part}` : part;
      let child = directory.directories.get(part);
      if (!child) {
        child = {
          kind: "directory",
          name: part,
          path,
          directories: new Map(),
          files: [],
        };
        directory.directories.set(part, child);
      }
      directory = child;
    }

    directory.files.push({ kind: "file", name: fileName, path: normalizedPath, item });
  }

  return finalizeChildren(root);
}

export function flattenUploadTree(
  nodes: UploadTreeNode[],
  collapsedPaths: ReadonlySet<string>,
): VisibleUploadTreeNode[] {
  const visible: VisibleUploadTreeNode[] = [];

  const visit = (children: UploadTreeNode[], depth: number) => {
    for (const node of children) {
      visible.push({ node, depth });
      if (node.kind === "directory" && !collapsedPaths.has(node.path)) {
        visit(node.children, depth + 1);
      }
    }
  };

  visit(nodes, 0);
  return visible;
}

function finalizeChildren(directory: MutableDirectoryNode): UploadTreeNode[] {
  const directories: UploadTreeDirectoryNode[] = Array.from(directory.directories.values())
    .map((child) => {
      const children = finalizeChildren(child);
      return {
        kind: "directory" as const,
        name: child.name,
        path: child.path,
        children,
        fileCount: children.reduce(
          (count, node) => count + (node.kind === "directory" ? node.fileCount : 1),
          0,
        ),
        totalBytes: children.reduce(
          (bytes, node) => bytes + (node.kind === "directory" ? node.totalBytes : node.item.file.size),
          0,
        ),
      };
    })
    .sort((left, right) => compareNames(left.name, right.name));

  const files = [...directory.files].sort((left, right) => compareNames(left.name, right.name));
  return [...directories, ...files];
}

function normalizePath(path: string): string {
  return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
}

function compareNames(left: string, right: string): number {
  return left.localeCompare(right, undefined, { numeric: true, sensitivity: "base" });
}
