import { describe, expect, it } from "vitest";
import { previewKind } from "./preview-policy";

describe("preview policy", () => {
  it.each([
    ["image/png", "image.png", "image"],
    ["application/pdf", "document.pdf", "pdf"],
    ["text/plain", "notes.txt", "text"],
    ["application/json", "data.json", "text"],
    ["audio/mpeg", "sound.mp3", "audio"],
    ["video/mp4", "movie.mp4", "video"],
    ["text/html", "page.html", "unsupported"],
    ["image/svg+xml", "vector.svg", "unsupported"],
    ["application/octet-stream", "random.bin", "unsupported"],
    ["application/octet-stream", "source.ts", "text"],
  ] as const)("maps %s to %s", (mimeType, path, expected) => {
    expect(previewKind({ mimeType, path })).toBe(expected);
  });
});
