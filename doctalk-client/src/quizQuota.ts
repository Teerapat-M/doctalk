export const QUIZ_DAILY_LIMIT = 5;
const storageKey = "dt-quiz-quota";

export type QuotaStore = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
};

export type QuotaSnapshot = { used: number; remaining: number; day: string };

// ponytail: guest daily cap is localStorage until Phase 5 auth; swap to GET /quota.
export function quotaDay(now = new Date(), timeZone = "Asia/Bangkok"): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    hourCycle: "h23",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
  }).formatToParts(now);
  const get = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((p) => p.type === type)?.value || "0");
  let year = get("year");
  let month = get("month");
  let day = get("day");
  if (get("hour") < 7) {
    const prev = new Date(Date.UTC(year, month - 1, day) - 86400000);
    year = prev.getUTCFullYear();
    month = prev.getUTCMonth() + 1;
    day = prev.getUTCDate();
  }
  return `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

export function readQuota(store: QuotaStore, now = new Date()): QuotaSnapshot {
  const day = quotaDay(now);
  let used = 0;
  try {
    const raw = JSON.parse(store.getItem(storageKey) || "null") as { day?: string; used?: number } | null;
    if (raw?.day === day) used = Math.max(0, Number(raw.used) || 0);
  } catch {
    used = 0;
  }
  return { used, remaining: Math.max(0, QUIZ_DAILY_LIMIT - used), day };
}

export function consumeQuota(store: QuotaStore, now = new Date()): QuotaSnapshot {
  const cur = readQuota(store, now);
  if (cur.remaining <= 0) return cur;
  const next = { day: cur.day, used: cur.used + 1 };
  store.setItem(storageKey, JSON.stringify(next));
  return { used: next.used, remaining: QUIZ_DAILY_LIMIT - next.used, day: cur.day };
}
