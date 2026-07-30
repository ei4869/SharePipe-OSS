import type { Entry } from "../../types";

export type PreviewKind = "image" | "pdf" | "video" | "audio" | "text" | "unsupported";

export function previewKind(entry: Pick<Entry, "mimeType" | "path">): PreviewKind {
  const mime = entry.mimeType.toLowerCase().split(";", 1)[0] ?? "";
  if (["text/html", "application/xhtml+xml", "image/svg+xml"].includes(mime)) return "unsupported";
  if (["image/jpeg", "image/png", "image/gif", "image/webp"].includes(mime)) return "image";
  if (mime === "application/pdf") return "pdf";
  if (mime.startsWith("video/")) return "video";
  if (mime.startsWith("audio/")) return "audio";
  if (mime.startsWith("text/") || mime === "application/json" || mime === "application/xml") return "text";
  if (/\.(txt|md|go|rs|py|js|jsx|ts|tsx|css|json|xml|yaml|yml|toml|sql|sh|ps1)$/iu.test(entry.path)) return "text";
  return "unsupported";
}
