import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../../api";
import type { StorageProfile, StorageUsage } from "../../types";
import { StorageUsagePanel } from "./StorageUsagePanel";

const r2Profile: StorageProfile = {
  id: "cloudflare-r2-binding", name: "Cloudflare R2", provider: "cloudflare-r2-binding",
  config: { bucket: "sharepipe", prefix: "app", region: "auto" }, kind: "s3", enabled: true,
  managed: true, hasCredentials: false, usesDefaultCredentials: true, hasSessionToken: false,
  default: true, inUse: 1, createdAt: "2026-07-01T00:00:00Z", updatedAt: "2026-07-01T00:00:00Z",
};

const usage: StorageUsage = {
  supported: true, available: true, provider: "cloudflare-r2-binding",
  profile: { id: r2Profile.id, name: r2Profile.name, bucket: "sharepipe", metricBucket: "sharepipe", prefix: "app" },
  range: "31d", current: { payloadBytes: 8_000_000_000, metadataBytes: 1000, objectCount: 42, uploadCount: 1 },
  storageSeries: [{ date: "2026-07-28", payloadBytes: 7_000_000_000, metadataBytes: 900, objectCount: 40, uploadCount: 1 }, { date: "2026-07-29", payloadBytes: 8_000_000_000, metadataBytes: 1000, objectCount: 42, uploadCount: 1 }],
  operationSeries: [{ date: "2026-07-29", classA: 960_000, classB: 20, free: 4, unclassified: 1 }],
  monthToDate: { classA: 960_000, classB: 20, free: 4, unclassified: 1, estimatedStorageGBMonth: 8 },
  budget: { storageGBMonth: 10, classA: 1_000_000, classB: 10_000_000, referenceDate: "2026-07-29", referenceKind: "cloudflare-standard-free-tier-or-custom" },
  estimated: true, observedAt: "2026-07-29T08:00:00Z", stale: false, cached: true, refreshLimited: false,
};

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("StorageUsagePanel", () => {
  it("renders summaries, semantic budget warnings, charts, and the equivalent table", async () => {
    vi.spyOn(api, "storageUsage").mockResolvedValue(usage);
    const { container } = render(<StorageUsagePanel profile={r2Profile} cloud />);
    await waitFor(() => expect(screen.getAllByText("7.5 GB").length).toBeGreaterThan(0));
    expect(screen.getByText(/缓存数据/u)).toBeInTheDocument();
    expect(screen.getByText("已使用预算的 70% 以上")).toBeInTheDocument();
    expect(screen.getByText("已使用预算的 95% 以上")).toBeInTheDocument();
    expect(screen.getByRole("progressbar", { name: "Class A / 月预算" })).toHaveAttribute("aria-valuenow", "96");
    expect(screen.getByRole("img", { name: "存储量趋势" })).toBeInTheDocument();
    expect(screen.getByText("查看等价数据表")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "未分类" })).toBeInTheDocument();
    expect(screen.getByLabelText("R2 操作类型图例")).toHaveTextContent("未分类");
    const freeBar = container.querySelector<HTMLElement>(".usage-operation-day .is-free");
    const unclassifiedBar = container.querySelector<HTMLElement>(".usage-operation-day .is-unclassified");
    expect(freeBar?.style.height).toMatch(/%$/u);
    expect(unclassifiedBar?.style.height).toMatch(/%$/u);
    expect(freeBar?.style.height).not.toBe(unclassifiedBar?.style.height);
    expect(screen.getAllByText(/未分类 1/u).length).toBeGreaterThan(0);
  });

  it("explains a missing analytics token without inventing usage", async () => {
    vi.spyOn(api, "storageUsage").mockResolvedValue({ ...usage, available: false, current: null, storageSeries: [], operationSeries: [], reason: "missing_analytics_token", message: "请配置只读 Token" });
    render(<StorageUsagePanel profile={r2Profile} cloud />);
    await waitFor(() => expect(screen.getByText("请配置只读 Token")).toBeInTheDocument());
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("does not call the Worker metrics endpoint for a non-R2 profile", () => {
    const request = vi.spyOn(api, "storageUsage");
    render(<StorageUsagePanel profile={{ ...r2Profile, id: "local", provider: "local", kind: "local" }} cloud={false} />);
    expect(screen.getByText("该提供商暂未接入用量监控。")).toBeInTheDocument();
    expect(request).not.toHaveBeenCalled();
  });
});
