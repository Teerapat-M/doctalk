import { apiFetch } from "./api";

export type SheetInfo = {
  name: string;
  headers: string[];
  row_count: number;
};

export type DocumentRecord = {
  id: string;
  filename: string;
  status: string;
  source_kind: string;
  sheet_name?: string;
  sheets?: SheetInfo[];
  chunk_count: number;
  page_count?: number | null;
  row_count?: number | null;
  error_message?: string | null;
};

export type ColumnMap = {
  content?: string;
  question?: string;
  answer?: string;
  explanation?: string;
  choice_a?: string;
  choice_b?: string;
  choice_c?: string;
  choice_d?: string;
};

export function documentsUrl(baseUrl: string, id?: string): string {
  const trimmed = baseUrl.replace(/\/$/, "");
  return id ? `${trimmed}/documents/${id}` : `${trimmed}/documents`;
}

export async function fetchDocuments(baseUrl: string): Promise<DocumentRecord[]> {
  const res = await apiFetch(documentsUrl(baseUrl));
  if (!res.ok) {
    throw new Error(`documents failed: ${res.status}`);
  }
  return (await res.json()) as DocumentRecord[];
}

export async function uploadDocument(baseUrl: string, file: File): Promise<DocumentRecord> {
  const body = new FormData();
  body.append("file", file);
  const res = await apiFetch(documentsUrl(baseUrl), { method: "POST", body });
  if (!res.ok) {
    throw new Error(await readError(res, `upload failed: ${res.status}`));
  }
  return (await res.json()) as DocumentRecord;
}

export async function ingestDocument(
  baseUrl: string,
  id: string,
  sheet: string,
  columns: ColumnMap,
): Promise<DocumentRecord> {
  const res = await apiFetch(`${documentsUrl(baseUrl, id)}/ingest`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sheet, columns }),
  });
  if (!res.ok) {
    throw new Error(await readError(res, `ingest failed: ${res.status}`));
  }
  return (await res.json()) as DocumentRecord;
}

export async function fetchDocument(baseUrl: string, id: string): Promise<DocumentRecord> {
  const res = await apiFetch(documentsUrl(baseUrl, id));
  if (res.status === 404) {
    throw new Error("not found");
  }
  if (!res.ok) {
    throw new Error(await readError(res, `document failed: ${res.status}`));
  }
  return (await res.json()) as DocumentRecord;
}

export function documentCountLabel(d: DocumentRecord): string {
  if (d.row_count != null) {
    return `${d.status}, ${d.row_count} rows`;
  }
  if (d.page_count != null) {
    return `${d.status}, ${d.page_count} pages`;
  }
  return d.status;
}

async function readError(res: Response, fallback: string): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error || fallback;
  } catch {
    return fallback;
  }
}
