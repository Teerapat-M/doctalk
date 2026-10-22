import { describe, expect, it } from "vitest";
import { canRetryAssistant, resetAssistantForRetry, retryQuestionOf, type ChatRetryItem } from "./chatRetry";

const thread: ChatRetryItem[] = [
  { id: "u1", role: "user", text: "What is pgvector?" },
  { id: "a1", role: "assistant", text: "…", error: "ask timed out" },
];

describe("chat retry state", () => {
  it("finds the user question for a failed assistant turn", () => {
    expect(retryQuestionOf(thread, "a1")).toBe("What is pgvector?");
    expect(retryQuestionOf(thread, "u1")).toBeUndefined();
    expect(canRetryAssistant(thread[1])).toBe(true);
    expect(canRetryAssistant(thread[0])).toBe(false);
  });

  it("clears the assistant bubble so Retry can stream again", () => {
    const next = resetAssistantForRetry(thread, "a1");
    expect(next[1]).toMatchObject({ id: "a1", text: "", error: undefined });
    expect(next[0].text).toBe("What is pgvector?");
  });
});
