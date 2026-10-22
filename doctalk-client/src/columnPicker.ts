import type { ColumnMap, SheetInfo } from "./documents";

export type ColumnRole = "question" | "answer" | "explanation" | "ignore";

export type ColumnPickerState = {
  sheet: string;
  roles: Record<string, ColumnRole>;
};

const questionNames = ["question", "คำถาม", "content", "เนื้อหา"];
const answerNames = ["answer", "คำตอบ"];
const explanationNames = ["explanation", "คำอธิบาย"];
const contentNames = ["content", "เนื้อหา"];

export function pickHeader(headers: string[], names: string[]): string {
  const lower = names.map((n) => n.toLowerCase());
  return headers.find((h) => lower.includes(h.toLowerCase())) ?? headers[0] ?? "";
}

export function sheetHeaders(sheets: SheetInfo[] | undefined, sheet: string): string[] {
  return sheets?.find((s) => s.name === sheet)?.headers ?? [];
}

export function defaultRole(header: string): ColumnRole {
  const h = header.toLowerCase();
  if (questionNames.includes(h)) return "question";
  if (answerNames.includes(h)) return "answer";
  if (explanationNames.includes(h)) return "explanation";
  return "ignore";
}

export function excelColLetter(index: number): string {
  let n = index;
  let out = "";
  do {
    out = String.fromCharCode(65 + (n % 26)) + out;
    n = Math.floor(n / 26) - 1;
  } while (n >= 0);
  return out;
}

export function defaultColumnPicker(sheets: SheetInfo[] | undefined): ColumnPickerState {
  const first = sheets?.[0];
  const roles: Record<string, ColumnRole> = {};
  for (const h of first?.headers ?? []) roles[h] = defaultRole(h);
  return { sheet: first?.name ?? "", roles };
}

export function selectSheet(sheets: SheetInfo[] | undefined, sheet: string): ColumnPickerState {
  const headers = sheetHeaders(sheets, sheet);
  const roles: Record<string, ColumnRole> = {};
  for (const h of headers) roles[h] = defaultRole(h);
  return { sheet, roles };
}

export function setRole(state: ColumnPickerState, header: string, role: ColumnRole): ColumnPickerState {
  const roles = { ...state.roles, [header]: role };
  if (role !== "ignore") {
    for (const [h, r] of Object.entries(roles)) {
      if (h !== header && r === role) roles[h] = "ignore";
    }
  }
  return { ...state, roles };
}

export function buildColumnMap(state: ColumnPickerState, headers: string[]): ColumnMap {
  const columns: ColumnMap = {};
  for (const h of headers) {
    const role = state.roles[h] ?? "ignore";
    if (role === "question") columns.question = h;
    if (role === "answer") columns.answer = h;
    if (role === "explanation") columns.explanation = h;
  }
  if (columns.question && !columns.answer && contentNames.includes(columns.question.toLowerCase())) {
    return { content: columns.question };
  }
  return columns;
}
