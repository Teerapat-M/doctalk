import { afterEach, describe, expect, it, vi } from "vitest";
import { formatCitation, generateQuiz, quizUrl, type Citation } from "./quiz";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("quizUrl", () => {
  it("joins /quiz onto the API base", () => {
    expect(quizUrl("http://localhost:8081")).toBe("http://localhost:8081/quiz");
    expect(quizUrl("http://localhost:8081/")).toBe("http://localhost:8081/quiz");
  });
});

describe("formatCitation", () => {
  it("prefers sheet and row for spreadsheet sources", () => {
    const citation: Citation = {
      document_id: "d",
      chunk_id: "c",
      snippet: "x",
      sheet_name: "คำถาม",
      row_number: 2,
    };
    expect(formatCitation(citation)).toBe("คำถาม, row 2");
  });

  it("uses page for PDFs", () => {
    expect(
      formatCitation({
        document_id: "d",
        chunk_id: "c",
        snippet: "x",
        page_number: 3,
      }),
    ).toBe("page 3");
  });
});

describe("generateQuiz", () => {
  it("posts document_ids, n, and mode", async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body)) as { n: number; mode: string };
      expect(body.n).toBe(4);
      expect(body.mode).toBe("from_columns");
      return {
        ok: true,
        status: 200,
        json: async () => ({
          questions: [
            {
              prompt: "เมืองหลวง?",
              options: { A: "ก", B: "ข", C: "ค", D: "ง" },
              correct: "A",
              explanation: "กรุงเทพมหานคร",
              citation: { document_id: "d", chunk_id: "c", snippet: "…", sheet_name: "คำถาม", row_number: 2 },
            },
          ],
        }),
      };
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await generateQuiz("http://localhost:8081", {
      document_ids: ["d"],
      n: 4,
      mode: "from_columns",
    });
    expect(result.questions).toHaveLength(1);
    expect(result.questions[0].correct).toBe("A");
    expect(fetchMock).toHaveBeenCalledWith("http://localhost:8081/quiz", expect.objectContaining({ method: "POST" }));
  });

  it("surfaces API errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 400,
        json: async () => ({ error: "n is capped at 20" }),
      })),
    );
    await expect(
      generateQuiz("http://localhost:8081", { document_ids: ["d"], n: 99 }),
    ).rejects.toThrow("n is capped at 20");
  });
});
