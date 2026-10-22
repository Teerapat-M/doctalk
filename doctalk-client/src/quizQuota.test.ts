import { describe, expect, it } from "vitest";
import { QUIZ_DAILY_LIMIT, consumeQuota, quotaDay, readQuota } from "./quizQuota";

function mem(initial?: string) {
  const data: Record<string, string> = initial ? { "dt-quiz-quota": initial } : {};
  return {
    getItem: (k: string) => data[k] ?? null,
    setItem: (k: string, v: string) => {
      data[k] = v;
    },
    data,
  };
}

describe("quotaDay", () => {
  it("rolls back before 07:00 Asia/Bangkok", () => {
    expect(quotaDay(new Date("2026-10-22T00:30:00+07:00"))).toBe("2026-10-21");
    expect(quotaDay(new Date("2026-10-22T07:00:00+07:00"))).toBe("2026-10-22");
  });
});

describe("readQuota / consumeQuota", () => {
  it("starts empty and stops at the daily cap", () => {
    const store = mem();
    const now = new Date("2026-10-22T10:00:00+07:00");
    expect(readQuota(store, now)).toEqual({ used: 0, remaining: QUIZ_DAILY_LIMIT, day: "2026-10-22" });
    let snap = readQuota(store, now);
    for (let i = 0; i < QUIZ_DAILY_LIMIT; i++) snap = consumeQuota(store, now);
    expect(snap.used).toBe(5);
    expect(snap.remaining).toBe(0);
    expect(consumeQuota(store, now).used).toBe(5);
  });

  it("does not count a previous day's usage", () => {
    const store = mem(JSON.stringify({ day: "2026-10-21", used: 5 }));
    expect(readQuota(store, new Date("2026-10-22T10:00:00+07:00")).remaining).toBe(QUIZ_DAILY_LIMIT);
  });
});
