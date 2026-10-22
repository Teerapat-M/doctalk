import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchHealth, healthUrl } from "./health";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("healthUrl", () => {
  it("joins /health onto the API base, ignoring a trailing slash", () => {
    expect(healthUrl("http://localhost:8080")).toBe("http://localhost:8080/health");
    expect(healthUrl("http://localhost:8080/")).toBe("http://localhost:8080/health");
  });
});

describe("fetchHealth", () => {
  it("returns the payload on 200", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ status: "ok", checks: { database: "ok" } }),
      })),
    );

    await expect(fetchHealth("http://localhost:8080")).resolves.toEqual({
      status: "ok",
      checks: { database: "ok" },
    });
  });

  it("treats 503 as a valid unhealthy payload", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 503,
        json: async () => ({
          status: "unhealthy",
          checks: { database: "unreachable" },
        }),
      })),
    );

    await expect(fetchHealth("http://localhost:8080")).resolves.toEqual({
      status: "unhealthy",
      checks: { database: "unreachable" },
    });
  });

  it("throws on unexpected status codes", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 500,
        json: async () => ({}),
      })),
    );

    await expect(fetchHealth("http://localhost:8080")).rejects.toThrow(
      "health check failed: 500",
    );
  });
});
