import { apiFetch } from "./api";
import type { Citation } from "./quiz";

export type AskRequest = {
  question: string;
  document_ids: string[];
  k?: number;
};

export type AskDelta = { event: "delta"; text: string };
export type AskFinal = { event: "final"; citations: Citation[] };
export type AskError = { event: "error"; error: string };
export type AskEvent = AskDelta | AskFinal | AskError;

export type AskHandlers = {
  onDelta: (text: string) => void;
  onFinal: (citations: Citation[]) => void;
  onError: (error: string) => void;
};

export function askUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/$/, "")}/ask`;
}

export function createSSEParser(): { push: (chunk: string) => AskEvent[] } {
  let buf = "";
  return {
    push(chunk: string): AskEvent[] {
      buf += chunk.replace(/\r\n/g, "\n");
      const events: AskEvent[] = [];
      for (;;) {
        const sep = buf.indexOf("\n\n");
        if (sep < 0) break;
        const block = buf.slice(0, sep);
        buf = buf.slice(sep + 2);
        const parsed = parseSSEBlock(block);
        if (parsed) events.push(parsed);
      }
      return events;
    },
  };
}

export function parseSSEBlock(block: string): AskEvent | null {
  let event = "";
  const dataLines: string[] = [];
  for (const raw of block.split("\n")) {
    if (raw.startsWith("event:")) {
      event = raw.slice(6).trim();
    } else if (raw.startsWith("data:")) {
      dataLines.push(raw.slice(5).trim());
    }
  }
  if (!event || dataLines.length === 0) return null;
  const raw = dataLines.join("\n");
  try {
    const data = JSON.parse(raw) as { text?: string; citations?: Citation[]; error?: string };
    if (event === "delta") return { event, text: data.text ?? "" };
    if (event === "final") return { event, citations: data.citations ?? [] };
    if (event === "error") return { event, error: data.error ?? raw };
  } catch {
    if (event === "error") return { event, error: raw };
  }
  return null;
}

function dispatch(ev: AskEvent, handlers: AskHandlers): void {
  if (ev.event === "delta") handlers.onDelta(ev.text);
  else if (ev.event === "final") handlers.onFinal(ev.citations);
  else handlers.onError(ev.error);
}

export const ASK_IDLE_TIMEOUT_MS = 35_000;
export const ASK_IDLE_ERROR = "ask timed out";

export async function streamAsk(
  baseUrl: string,
  req: AskRequest,
  handlers: AskHandlers,
  signal?: AbortSignal,
  idleMs: number = ASK_IDLE_TIMEOUT_MS,
): Promise<void> {
  const res = await apiFetch(askUrl(baseUrl), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
    signal,
  });
  const ctype = res.headers.get("content-type") || "";
  if (!ctype.includes("text/event-stream")) {
    let message = `ask failed: ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      /* keep fallback */
    }
    throw new Error(message);
  }
  if (!res.body) {
    throw new Error("ask failed: empty stream");
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  const parser = createSSEParser();
  try {
    for (;;) {
      const chunk = await readWithIdle(reader, idleMs, signal);
      if (chunk === null) break;
      for (const ev of parser.push(decoder.decode(chunk, { stream: true }))) {
        dispatch(ev, handlers);
      }
    }
    for (const ev of parser.push(decoder.decode())) {
      dispatch(ev, handlers);
    }
  } catch (err: unknown) {
    const name = (err as { name?: string }).name;
    if (name === "AbortError") throw err;
    const message = err instanceof Error ? err.message : ASK_IDLE_ERROR;
    handlers.onError(message);
  } finally {
    try {
      await reader.cancel();
    } catch {
      /* already closed */
    }
  }
}

function readWithIdle(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  idleMs: number,
  signal?: AbortSignal,
): Promise<Uint8Array | null> {
  if (idleMs <= 0) {
    return reader.read().then(({ done, value }) => (done ? null : (value ?? new Uint8Array())));
  }
  return new Promise((resolve, reject) => {
    let settled = false;
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      reject(new Error(ASK_IDLE_ERROR));
    }, idleMs);
    const onAbort = () => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      reject(signal?.reason instanceof Error ? signal.reason : new DOMException("Aborted", "AbortError"));
    };
    if (signal) {
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.addEventListener("abort", onAbort, { once: true });
    }
    reader
      .read()
      .then(({ done, value }) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        signal?.removeEventListener("abort", onAbort);
        resolve(done ? null : (value ?? new Uint8Array()));
      })
      .catch((err: unknown) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        signal?.removeEventListener("abort", onAbort);
        reject(err);
      });
  });
}
