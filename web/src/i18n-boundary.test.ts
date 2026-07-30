import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

function productionTSX(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const path = join(root, entry.name);
    if (entry.isDirectory()) return productionTSX(path);
    return entry.name.endsWith(".tsx") && !entry.name.endsWith(".test.tsx") ? [path] : [];
  });
}

describe("simplified Chinese message boundary", () => {
  it("keeps visible Chinese copy out of production TSX components", () => {
    const sourceRoot = dirname(fileURLToPath(import.meta.url));
    const violations = productionTSX(sourceRoot).flatMap((path) => {
      const lines = readFileSync(path, "utf8").split(/\r?\n/u);
      return lines.flatMap((line, index) => /\p{Script=Han}/u.test(line) ? [`${path}:${index + 1}`] : []);
    });
    expect(violations, "Move visible Simplified Chinese copy into i18n/zh-CN.ts").toEqual([]);
  });
});
