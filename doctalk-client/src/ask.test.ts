import { afterEach, describe, expect, it, vi } from "vitest";
import { askUrl, createSSEParser, parseSSEBlock, streamAsk } from "./ask";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("askUrl", () => {
  it("joins /ask onto the API base", () => {
    expect(askUrl("http://localhost:8181")).toBe("http://localhost:8181/ask");
    expect(askUrl("http://localhost:8181/")).toBe("http://localhost:8181/ask");
  });
});

describe("parseSSEBlock", () => {
  it("parses delta, final citations, and redacted error events", () => {
    expect(parseSSEBlock('event: delta\ndata: {"text":"Hello"}')).toEqual({
      event: "delta",
      text: "Hello",
    });
    expect(
      parseSSEBlock('event: final\ndata: {"citations":[{"document_id":"d","chunk_id":"c","snippet":"x","page_number":2}]}'),
    ).toEqual({
      event: "final",
      citations: [{ document_id: "d", chunk_id: "c", snippet: "x", page_number: 2 }],
    });
    expect(parseSSEBlock('event: error\ndata: {"error":"gemini chat: HTTP 503: UNAVAILABLE"}')).toEqual({
      event: "error",
      error: "gemini chat: HTTP 503: UNAVAILABLE",
    });
  });
});

describe("createSSEParser", () => {
  it("reassembles events split across chunks", () => {
    const parser = createSSEParser();
    expect(parser.push("event: delta\ndata: {\"text\":\"Hel")).toEqual([]);
    expect(parser.push("lo\"}\n\nevent: final\ndata: {\"citations\":[]}\n\n")).toEqual([
      { event: "delta", text: "Hello" },
      { event: "final", citations: [] },
    ]);
  });
});

describe("streamAsk", () => {
  it("reads an SSE body and surfaces JSON errors before the stream", async () => {
    const deltas: string[] = [];
    const finals: unknown[] = [];
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          new TextEncoder().encode('event: delta\ndata: {"text":"Hi"}\n\nevent: final\ndata: {"citations":[]}\n\n'),
        );
        controller.close();
      },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        headers: { get: () => "text/event-stream" },
        body: stream,
      })),
    );
    await streamAsk(
      "",
      { question: "q", document_ids: ["d"] },
      {
        onDelta: (t) => deltas.push(t),
        onFinal: (c) => finals.push(c),
        onError: () => {
          throw new Error("unexpected error event");
        },
      },
    );
    expect(deltas).toEqual(["Hi"]);
    expect(finals).toEqual([[]]);

    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 400,
        headers: { get: () => "application/json" },
        json: async () => ({ error: "k is out of range" }),
      })),
    );
    await expect(
      streamAsk("", { question: "q", document_ids: ["d"], k: 0 }, {
        onDelta: () => undefined,
        onFinal: () => undefined,
        onError: () => undefined,
      }),
    ).rejects.toThrow("k is out of range");
  });

  it("dispatches a redacted SSE error event", async () => {
    const errors: string[] = [];
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          new TextEncoder().encode(
            'event: error\ndata: {"error":"gemini chat: HTTP 503: UNAVAILABLE"}\n\n',
          ),
        );
        controller.close();
      },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        headers: { get: () => "text/event-stream" },
        body: stream,
      })),
    );
    await streamAsk(
      "",
      { question: "q", document_ids: ["d"] },
      {
        onDelta: () => undefined,
        onFinal: () => undefined,
        onError: (e) => errors.push(e),
      },
    );
    expect(errors).toEqual(["gemini chat: HTTP 503: UNAVAILABLE"]);
  });

  it("surfaces an idle timeout as an error event with Retry state", async () => {
    const errors: string[] = [];
    const stream = new ReadableStream<Uint8Array>({
      start() {
        /* hang without bytes so the client idle timer fires */
      },
      cancel() {
        /* streamAsk cancels the reader after idle */
      },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        headers: { get: () => "text/event-stream" },
        body: stream,
      })),
    );
    await streamAsk(
      "",
      { question: "q", document_ids: ["d"] },
      {
        onDelta: () => undefined,
        onFinal: () => undefined,
        onError: (e) => errors.push(e),
      },
      undefined,
      25,
    );
    expect(errors).toEqual(["ask timed out"]);
  });
});

