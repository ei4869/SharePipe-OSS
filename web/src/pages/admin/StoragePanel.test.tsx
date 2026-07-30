import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { StoragePanel } from "./StoragePanel";
import type { StorageProfile, StorageProviderDefinition } from "../../types";

const providers: StorageProviderDefinition[] = [
  { id: "local", label: "本地磁盘（仅本机运行时）", kind: "local", documentationUrl: "https://example.com/local", validation: "built-in", available: false, creatable: false, unavailableReason: "Worker 无法挂载本机文件系统" },
  { id: "nas", label: "NAS（仅本机运行时）", kind: "local", documentationUrl: "https://example.com/nas", validation: "mounted-filesystem", available: false, creatable: false, unavailableReason: "Worker 无法挂载本机文件系统" },
  { id: "cloudflare-r2-binding", label: "Cloudflare R2（Worker Binding）", kind: "s3", documentationUrl: "https://example.com/binding", validation: "live-tested", available: true, creatable: false },
  { id: "cloudflare-r2", label: "Cloudflare R2（S3 直传）", kind: "s3", documentationUrl: "https://example.com/r2", validation: "live-tested", available: true, creatable: true },
  { id: "aws-s3", label: "Amazon S3", kind: "s3", documentationUrl: "https://example.com/s3", validation: "official-spec", available: true, creatable: true },
];

const profiles: StorageProfile[] = [{
  id: "cloudflare-r2-binding",
  name: "Cloudflare R2",
  provider: "cloudflare-r2-binding",
  config: { bucket: "sharepipe-example", prefix: "sharepipe-cloud", region: "auto" },
  kind: "s3",
  enabled: true,
  managed: true,
  hasCredentials: false,
  usesDefaultCredentials: true,
  hasSessionToken: false,
  default: true,
  inUse: 11,
  createdAt: new Date(0).toISOString(),
  updatedAt: new Date(0).toISOString(),
}];

describe("public Worker storage panel", () => {
  it("shows local capabilities as unavailable and offers every remote provider", () => {
    render(<StoragePanel profiles={profiles} providers={providers} onChanged={vi.fn()} onError={vi.fn()} />);
    expect(screen.getByText("本地磁盘（仅本机运行时）")).toBeInTheDocument();
    expect(screen.getByText("NAS（仅本机运行时）")).toBeInTheDocument();
    expect(screen.getByText("凭据：Worker Binding（无需网页保存 S3 密钥）")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /编辑/u })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /新增存储/u }));
    expect(screen.getByRole("option", { name: "本地磁盘（仅本机运行时）（当前运行时不可用）" })).toBeDisabled();
    expect(screen.getByRole("option", { name: "Cloudflare R2（Worker Binding）（部署托管）" })).toBeDisabled();
    expect(screen.getByRole("option", { name: "Cloudflare R2（S3 直传）" })).toBeEnabled();
    expect(screen.getByRole("option", { name: "Amazon S3" })).toBeEnabled();
  });
});
