import { apiFetch } from "./api";

export type QuotaBucket = {
  used: number;
  remaining: number;
  limit: number;
  reset_at?: string;
  retry_after?: number;
};

export type QuotaResponse = {
  quiz: QuotaBucket;
  ask: QuotaBucket;
  ingest: QuotaBucket;
};

export function quotaUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/$/, "")}/quota`;
}

export async function fetchQuota(baseUrl: string): Promise<QuotaResponse> {
  const res = await apiFetch(quotaUrl(baseUrl));
  if (!res.ok) {
    throw new Error(`quota failed: ${res.status}`);
  }
  return (await res.json()) as QuotaResponse;
}

export function quizQuotaReached(quota: QuotaResponse | null): boolean {
  const quiz = quota?.quiz;
  return !!quiz && quiz.limit > 0 && quiz.remaining <= 0;
}
