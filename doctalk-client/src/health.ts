import { apiFetch } from "./api";

export type HealthResponse = {
  status: string;
  checks: { database: string };
};

export function healthUrl(baseUrl: string): string {
  const trimmed = baseUrl.replace(/\/$/, "");
  return `${trimmed}/health`;
}

export async function fetchHealth(baseUrl: string): Promise<HealthResponse> {
  const res = await apiFetch(healthUrl(baseUrl));
  if (!res.ok && res.status !== 503) {
    throw new Error(`health check failed: ${res.status}`);
  }
  return (await res.json()) as HealthResponse;
}
