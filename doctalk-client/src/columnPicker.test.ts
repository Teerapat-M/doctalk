import { describe, expect, it } from "vitest";
import {
  buildColumnMap,
  defaultColumnPicker,
  defaultRole,
  excelColLetter,
  pickHeader,
  selectSheet,
  setRole,
  sheetHeaders,
} from "./columnPicker";
import type { SheetInfo } from "./documents";

const sheets: SheetInfo[] = [
  {
    name: "คำถาม",
    headers: ["คำถาม", "คำตอบ", "คำอธิบาย"],
    row_count: 4,
  },
  {
    name: "Notes",
    headers: ["content", "extra"],
    row_count: 2,
  },
];

describe("pickHeader", () => {
  it("matches English or Thai header names, else the first header", () => {
    expect(pickHeader(["Question", "Answer"], ["question", "คำถาม"])).toBe("Question");
    expect(pickHeader(["คำถาม", "คำตอบ"], ["question", "คำถาม"])).toBe("คำถาม");
    expect(pickHeader(["foo", "bar"], ["question"])).toBe("foo");
    expect(pickHeader([], ["question"])).toBe("");
  });
});

describe("sheetHeaders", () => {
  it("returns headers for the named sheet", () => {
    expect(sheetHeaders(sheets, "Notes")).toEqual(["content", "extra"]);
    expect(sheetHeaders(sheets, "missing")).toEqual([]);
    expect(sheetHeaders(undefined, "Notes")).toEqual([]);
  });
});

describe("defaultRole", () => {
  it("classifies Question / Answer / Explanation / Ignore", () => {
    expect(defaultRole("คำถาม")).toBe("question");
    expect(defaultRole("Answer")).toBe("answer");
    expect(defaultRole("คำอธิบาย")).toBe("explanation");
    expect(defaultRole("#")).toBe("ignore");
  });
});

describe("excelColLetter", () => {
  it("uses Excel-style letters", () => {
    expect(excelColLetter(0)).toBe("A");
    expect(excelColLetter(1)).toBe("B");
    expect(excelColLetter(25)).toBe("Z");
    expect(excelColLetter(26)).toBe("AA");
  });
});

describe("defaultColumnPicker", () => {
  it("selects the first sheet and pre-fills known column roles", () => {
    expect(defaultColumnPicker(sheets)).toEqual({
      sheet: "คำถาม",
      roles: { คำถาม: "question", คำตอบ: "answer", คำอธิบาย: "explanation" },
    });
  });
});

describe("setRole", () => {
  it("keeps question/answer/explanation unique and allows Ignore", () => {
    const start = defaultColumnPicker(sheets);
    const ignored = setRole(start, "คำอธิบาย", "ignore");
    expect(ignored.roles["คำอธิบาย"]).toBe("ignore");
    const swapped = setRole(start, "คำตอบ", "question");
    expect(swapped.roles["คำตอบ"]).toBe("question");
    expect(swapped.roles["คำถาม"]).toBe("ignore");
  });
});

describe("selectSheet", () => {
  it("resets roles for the chosen sheet", () => {
    expect(selectSheet(sheets, "Notes")).toEqual({
      sheet: "Notes",
      roles: { content: "question", extra: "ignore" },
    });
  });
});

describe("buildColumnMap", () => {
  it("maps Question/Answer/Explanation roles onto the ingest API", () => {
    expect(buildColumnMap(defaultColumnPicker(sheets), ["คำถาม", "คำตอบ", "คำอธิบาย"])).toEqual({
      question: "คำถาม",
      answer: "คำตอบ",
      explanation: "คำอธิบาย",
    });
  });

  it("maps a content header without an answer column to columns.content", () => {
    const state = selectSheet(sheets, "Notes");
    expect(buildColumnMap(state, ["content", "extra"])).toEqual({ content: "content" });
  });
});
