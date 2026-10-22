import { describe, expect, it } from "vitest";
import { answerVisible, clampN, optionState, toggleAllAnswers, toggleOneAnswer } from "./quizToggles";

describe("quiz toggles", () => {
  it("uses the global key until a per-question override is set", () => {
    expect(answerVisible(0, {}, false)).toBe(false);
    expect(answerVisible(0, {}, true)).toBe(true);
    expect(answerVisible(1, { 1: false }, true)).toBe(false);
    expect(answerVisible(1, { 1: true }, false)).toBe(true);
  });

  it("clears per-question overrides when toggling all at once", () => {
    const next = toggleAllAnswers(true);
    expect(next).toEqual({ showAll: true, shown: {} });
    expect(answerVisible(2, next.shown, next.showAll)).toBe(true);
  });

  it("records a per-question toggle without changing others", () => {
    const shown = toggleOneAnswer({ 0: true }, 1, false);
    expect(shown[0]).toBe(true);
    expect(shown[1]).toBe(false);
    expect(answerVisible(1, shown, true)).toBe(false);
  });
});

describe("optionState", () => {
  it("uses Default/Selected until answers are revealed", () => {
    expect(optionState("A", undefined, "B", false)).toBe("default");
    expect(optionState("A", "A", "B", false)).toBe("selected");
  });

  it("marks Correct, Incorrect, and Revealed after the key is shown", () => {
    expect(optionState("B", "B", "B", true)).toBe("correct");
    expect(optionState("B", undefined, "B", true)).toBe("correct");
    expect(optionState("B", "A", "B", true)).toBe("revealed");
    expect(optionState("A", "A", "B", true)).toBe("incorrect");
    expect(optionState("C", "A", "B", true)).toBe("default");
  });
});

describe("clampN", () => {
  it("stays inside 1–20", () => {
    expect(clampN(0)).toBe(1);
    expect(clampN(50)).toBe(20);
    expect(clampN(7.9)).toBe(7);
  });
});
