import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchQuota, formatQuotaLabel, quizQuotaReached, quotaUrl } from "./quizQuota";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("quotaUrl", () => {
  it("joins /quota onto the API base", () => {
    expect(quotaUrl("http://localhost:8181")).toBe("http://localhost:8181/quota");
    expect(quotaUrl("http://localhost:8181/")).toBe("http://localhost:8181/quota");
  });
});

describe("fetchQuota", () => {
  it("reads server quota with credentials", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        quiz: { used: 5, remaining: 0, limit: 5, reset_at: "2026-10-23T00:00:00Z" },
        ask: { used: 0, remaining: 20, limit: 20 },
        ingest: { used: 1, remaining: 9, limit: 10 },
      }),
    }));
    vi.stubGlobal("fetch", fetchMock);
    const quota = await fetchQuota("http://localhost:8181");
    expect(quota.quiz.remaining).toBe(0);
    expect(quizQuotaReached(quota)).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost:8181/quota",
      expect.objectContaining({ credentials: "include" }),
    );
  });

  it("does not treat an unloaded quota as reached", () => {
    expect(quizQuotaReached(null)).toBe(false);
    expect(
      quizQuotaReached({
        quiz: { used: 0, remaining: 5, limit: 5 },
        ask: { used: 0, remaining: 20, limit: 20 },
        ingest: { used: 0, remaining: 10, limit: 10 },
      }),
    ).toBe(false);
  });
});

describe("formatQuotaLabel", () => {
  it("shows remaining ask and quiz quota", () => {
    expect(formatQuotaLabel(null)).toBe("");
    expect(
      formatQuotaLabel({
        quiz: { used: 1, remaining: 4, limit: 5 },
        ask: { used: 2, remaining: 18, limit: 20 },
        ingest: { used: 0, remaining: 10, limit: 10 },
      }),
    ).toBe("Ask 18/20 · Quiz 4/5");
  });
});
