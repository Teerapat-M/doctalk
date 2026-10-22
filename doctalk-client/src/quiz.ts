import { apiFetch } from "./api";

export type Citation = {
  document_id: string;
  page_number?: number;
  sheet_name?: string;
  row_number?: number;
  chunk_id: string;
  snippet: string;
};

export type QuizOptions = {
  A: string;
  B: string;
  C: string;
  D: string;
};

export type QuizQuestion = {
  prompt: string;
  options: QuizOptions;
  correct: "A" | "B" | "C" | "D";
  explanation: string;
  citation: Citation;
};

export type QuizResponse = {
  questions: QuizQuestion[];
};

export type QuizMode = "generate" | "from_columns";

export type QuizRequest = {
  document_ids: string[];
  n: number;
  mode?: QuizMode;
  chunk_ids?: string[];
};

export function quizUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/$/, "")}/quiz`;
}

export function formatCitation(citation: Citation): string {
  if (citation.sheet_name && citation.row_number) {
    return `${citation.sheet_name}, row ${citation.row_number}`;
  }
  if (citation.page_number) {
    return `page ${citation.page_number}`;
  }
  return "source";
}

export async function generateQuiz(baseUrl: string, req: QuizRequest): Promise<QuizResponse> {
  const res = await apiFetch(quizUrl(baseUrl), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    let message = `quiz failed: ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      /* keep fallback */
    }
    throw new Error(message);
  }
  return (await res.json()) as QuizResponse;
}
