import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FilePicker } from "./FilePicker";
import type { PickedFile } from "./types";

afterEach(cleanup);

function picked(path: string): PickedFile {
  return {
    id: path,
    file: new File([path], path.split("/").at(-1) || path),
    path,
    progress: 0,
    status: "pending",
  };
}

describe("FilePicker pending upload hierarchy", () => {
  const files = [
    picked("data/session.key"),
    picked("data/backups/sharepipe.db"),
    picked("data/storage/objects/nested.bin"),
  ];

  it("renders nested folder rows and can collapse or expand a subtree", () => {
    render(<FilePicker files={files} disabled={false} onAdd={vi.fn()} onRemove={vi.fn()} />);

    expect(screen.getByRole("treeitem", { name: "收起目录 data" })).toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: "收起目录 backups" })).toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: "收起目录 storage" })).toBeInTheDocument();
    expect(screen.getByRole("treeitem", { name: "收起目录 objects" })).toBeInTheDocument();
    expect(screen.getByText("nested.bin")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("treeitem", { name: "收起目录 storage" }));
    expect(screen.getByRole("treeitem", { name: "展开目录 storage" })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("treeitem", { name: "收起目录 objects" })).not.toBeInTheDocument();
    expect(screen.queryByText("nested.bin")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("treeitem", { name: "展开目录 storage" }));
    expect(screen.getByRole("treeitem", { name: "收起目录 objects" })).toBeInTheDocument();
    expect(screen.getByText("nested.bin")).toBeInTheDocument();
  });

  it("keeps the original per-file remove action", () => {
    const onRemove = vi.fn();
    render(<FilePicker files={files} disabled={false} onAdd={vi.fn()} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole("button", { name: "移除 nested.bin" }));
    expect(onRemove).toHaveBeenCalledWith("data/storage/objects/nested.bin");
  });
});
