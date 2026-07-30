import { describe, expect, it } from "vitest";
import { basename, formatBytes, parentPath } from "./utils";

describe("file display helpers", () => {
  it("formats sizes and paths", () => {
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(basename("项目/设计稿.pdf")).toBe("设计稿.pdf");
    expect(parentPath("项目/设计稿.pdf")).toBe("项目");
  });
});
