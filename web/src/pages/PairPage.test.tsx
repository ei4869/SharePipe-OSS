import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PairPage } from "./PairPage";

afterEach(() => { cleanup(); vi.restoreAllMocks() });

describe("pair code entry", () => {
  it("normalizes pasted digits and redirects using the high-entropy access URL", async () => {
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/public/config")) return new Response(JSON.stringify({ turnstileSiteKey: "" }), { status: 200, headers: { "Content-Type": "application/json" } });
      return new Response(JSON.stringify({ url: "https://share.example/s/access-token", expiresAt: "2026-08-01T00:00:00Z" }), { status: 201, headers: { "Content-Type": "application/json" } });
    }));
    render(<PairPage />);
    const input = screen.getByLabelText("六位配对码");
    fireEvent.paste(input, { clipboardData: { getData: () => "12 34-56" } });
    expect(input).toHaveValue("123456");
    expect(screen.getByRole("button", { name: /接收文件/u })).toBeEnabled();
  });
});
