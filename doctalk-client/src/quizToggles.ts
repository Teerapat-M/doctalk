export type ShownMap = Record<number, boolean>;
export type OptionState = "default" | "selected" | "correct" | "incorrect" | "revealed";
export type PicksMap = Record<number, "A" | "B" | "C" | "D">;

export function clampN(n: number, min = 1, max = 20): number {
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, Math.trunc(n)));
}

export function optionState(
  letter: string,
  selected: string | undefined,
  correct: string,
  revealed: boolean,
): OptionState {
  if (!revealed) return selected === letter ? "selected" : "default";
  if (letter === correct) return selected && selected !== correct ? "revealed" : "correct";
  if (letter === selected) return "incorrect";
  return "default";
}

export function answerVisible(index: number, shown: ShownMap, showAll: boolean): boolean {
  if (Object.prototype.hasOwnProperty.call(shown, index)) {
    return shown[index];
  }
  return showAll;
}

export function toggleAllAnswers(next: boolean): { showAll: boolean; shown: ShownMap } {
  return { showAll: next, shown: {} };
}

export function toggleOneAnswer(shown: ShownMap, index: number, next: boolean): ShownMap {
  return { ...shown, [index]: next };
}
