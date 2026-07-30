import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AdminPage, InvitesPanel } from "./AdminPage";

const limits = {
  maxFileBytes: 10 * 1024 ** 3,
  maxShareBytes: 20 * 1024 ** 3,
  maxFilesPerShare: 1000,
  defaultExpiry: "168h",
  defaultDownloadLimit: 10,
  zipAsyncThresholdBytes: 2 * 1024 ** 3,
};

function administratorFetch(local: boolean, authenticated = true, passwordConfigured = true) {
  const paths: string[] = [];
  const handler = vi.fn(async (input: RequestInfo | URL) => {
    const path = typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url;
    paths.push(path);
    let value: unknown;
    if (path === "/api/v1/admin/session") {
      value = { local, authenticated, email: local ? "local@sharepipe" : "", passwordConfigured };
    } else if (path === "/api/v1/admin/overview") {
      value = { activeShares: 0, storageBytes: 0, downloadsToday: 0, pendingCleanup: 0, storage: [] };
    } else if (path.startsWith("/api/v1/admin/shares")) {
      value = { items: [] };
    } else if (path === "/api/v1/admin/invites") {
      value = { items: [] };
    } else if (path === "/api/v1/admin/settings") {
      value = { limits, defaultStorage: "cloudflare-r2" };
    } else if (path === "/api/v1/admin/security") {
      value = { local: true, currentEmail: "local@sharepipe", emails: ["ai4869ei@gmail.com"], passwordConfigured: true };
    } else if (path === "/api/v1/admin/storage/profiles" || path === "/api/v1/admin/storage/providers") {
      value = { items: [] };
    } else {
      return new Response(JSON.stringify({ error: { message: `unexpected request: ${path}` } }), { status: 404 });
    }
    return new Response(JSON.stringify(value), { status: 200, headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", handler);
  return paths;
}

describe("administrator security visibility", () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(cleanup);

  it("does not render or request advanced security for a public administrator", async () => {
    const paths = administratorFetch(false);
    render(<AdminPage />);
    expect(await screen.findByRole("button", { name: "概览" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "高级安全" })).not.toBeInTheDocument();
    expect(paths).not.toContain("/api/v1/admin/security");
  });

  it("renders advanced security and loads it for a trusted loopback administrator", async () => {
    const paths = administratorFetch(true);
    render(<AdminPage />);
    expect(await screen.findByRole("button", { name: "高级安全" })).toBeInTheDocument();
    expect(paths).toContain("/api/v1/admin/security");
  });

  it("offers password and email as independent public login methods", async () => {
    const paths = administratorFetch(false, false, true);
    render(<AdminPage />);
    expect(await screen.findByPlaceholderText("管理员密码")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "使用管理员邮箱认证" })).toHaveAttribute("href", "/admin/email-login");
    expect(screen.queryByRole("button", { name: "高级安全" })).not.toBeInTheDocument();
    expect(paths).toEqual(["/api/v1/admin/session"]);
  });

  it("does not offer password login until a local administrator configures it", async () => {
    administratorFetch(false, false, false);
    render(<AdminPage />);
    expect(await screen.findByText(/管理员密码尚未设置/u)).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("管理员密码")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "使用管理员邮箱认证" })).toBeInTheDocument();
  });
});

describe("trusted invitation creation", () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(cleanup);

  it("uses the device name as the invitation title and makes the code single-use", async () => {
    let payload: Record<string, unknown> | null = null;
    vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      payload = JSON.parse(String(init?.body)) as Record<string, unknown>;
      return new Response(JSON.stringify({ id: "invite", code: "SP-test", label: payload.label, kind: payload.kind, deviceName: payload.deviceName, maxUses: 1, uses: 0, createdAt: new Date().toISOString() }), { status: 201, headers: { "Content-Type": "application/json" } });
    }));
    render(<InvitesPanel invites={[]} onChanged={vi.fn(async () => undefined)} onError={vi.fn()} />);

    fireEvent.change(screen.getByLabelText("邀请码类型"), { target: { value: "trusted_device" } });
    fireEvent.change(screen.getByLabelText("设备或人员名称"), { target: { value: "小明的电脑" } });
    expect(screen.getByLabelText("邀请码最大使用次数")).toBeDisabled();
    expect(screen.getByLabelText("邀请码最大使用次数")).toHaveValue(1);
    fireEvent.click(screen.getByRole("button", { name: "创建" }));

    await waitFor(() => expect(payload).not.toBeNull());
    expect(payload).toMatchObject({ label: "小明的电脑", deviceName: "小明的电脑", kind: "trusted_device", maxUses: 1 });
  });
});
