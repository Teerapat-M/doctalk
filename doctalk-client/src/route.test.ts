import { describe, expect, it } from "vitest";
import { hashFor, parseHash } from "./route";

describe("parseHash", () => {
  it("maps landing, workspace, quiz, error, and unknown paths", () => {
    expect(parseHash("")).toEqual({ name: "landing" });
    expect(parseHash("#/")).toEqual({ name: "landing" });
    expect(parseHash("#/workspace/abc")).toEqual({ name: "workspace", id: "abc", tab: "chat" });
    expect(parseHash("#/workspace/abc/quiz")).toEqual({ name: "workspace", id: "abc", tab: "quiz" });
    expect(parseHash("#/quiz")).toEqual({ name: "workspace", tab: "quiz" });
    expect(parseHash("#/not-found")).toEqual({ name: "not-found" });
    expect(parseHash("#/error?message=gemini%20chat:%20HTTP%20503")).toEqual({
      name: "error",
      message: "gemini chat: HTTP 503",
    });
    expect(parseHash("#/nope")).toEqual({ name: "not-found" });
  });
});

describe("hashFor", () => {
  it("does not treat quiz as a document id", () => {
    expect(hashFor({ name: "workspace", tab: "quiz" })).toBe("#/quiz");
    expect(hashFor({ name: "workspace", id: "abc", tab: "quiz" })).toBe("#/workspace/abc/quiz");
    expect(parseHash(hashFor({ name: "workspace", tab: "quiz" }))).toEqual({
      name: "workspace",
      tab: "quiz",
    });
  });
});
