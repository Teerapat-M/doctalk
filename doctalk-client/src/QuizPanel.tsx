import { useEffect, useMemo, useState } from "react";
import {
  fetchDocument,
  fetchDocuments,
  ingestDocument,
  uploadDocument,
  type ColumnMap,
  type DocumentRecord,
} from "./documents";
import { formatCitation, generateQuiz, type QuizMode, type QuizQuestion } from "./quiz";

const apiBase = import.meta.env.VITE_API_URL ?? "";
const optionLetters = ["A", "B", "C", "D"] as const;

type Props = {
  baseUrl?: string;
};

export default function QuizPanel({ baseUrl = apiBase }: Props) {
  const [docs, setDocs] = useState<DocumentRecord[]>([]);
  const [sourceId, setSourceId] = useState("");
  const [n, setN] = useState(5);
  const [mode, setMode] = useState<QuizMode>("generate");
  const [questions, setQuestions] = useState<QuizQuestion[]>([]);
  const [showAllAnswers, setShowAllAnswers] = useState(false);
  const [shown, setShown] = useState<Record<number, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sheet, setSheet] = useState("");
  const [questionCol, setQuestionCol] = useState("");
  const [answerCol, setAnswerCol] = useState("");
  const [explanationCol, setExplanationCol] = useState("");
  const [contentCol, setContentCol] = useState("");
  const [mapMode, setMapMode] = useState<"question" | "content">("question");

  const selected = useMemo(() => docs.find((d) => d.id === sourceId), [docs, sourceId]);
  const readyDocs = docs.filter((d) => d.status === "done");
  const mappingDocs = docs.filter((d) => d.status === "mapping");

  async function refresh() {
    const list = await fetchDocuments(baseUrl);
    setDocs(list);
    return list;
  }

  useEffect(() => {
    let cancelled = false;
    fetchDocuments(baseUrl)
      .then((list) => {
        if (!cancelled) setDocs(list);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "could not list documents");
      });
    return () => {
      cancelled = true;
    };
  }, [baseUrl]);

  useEffect(() => {
    if (!selected?.sheets?.length) return;
    const first = selected.sheets[0];
    setSheet(first.name);
    const headers = first.headers;
    setQuestionCol(pickHeader(headers, ["question", "คำถาม"]));
    setAnswerCol(pickHeader(headers, ["answer", "คำตอบ"]));
    setExplanationCol(pickHeader(headers, ["explanation", "คำอธิบาย"]));
    setContentCol(pickHeader(headers, ["content", "เนื้อหา"]));
  }, [selected?.id, selected?.sheets]);

  const sheetHeaders = selected?.sheets?.find((s) => s.name === sheet)?.headers ?? [];

  async function onUpload(file: File) {
    setError(null);
    setBusy(true);
    try {
      const doc = await uploadDocument(baseUrl, file);
      const list = await refresh();
      setSourceId(doc.id);
      if (doc.status !== "mapping") {
        await pollUntilSettled(doc.id, list);
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "upload failed");
    } finally {
      setBusy(false);
    }
  }

  async function pollUntilSettled(id: string, list?: DocumentRecord[]) {
    let current = list ?? docs;
    for (let i = 0; i < 30; i++) {
      const doc = current.find((d) => d.id === id) ?? (await fetchDocument(baseUrl, id));
      if (doc.status === "done" || doc.status === "failed" || doc.status === "mapping") {
        await refresh();
        return;
      }
      await new Promise((r) => setTimeout(r, 1000));
      current = await refresh();
    }
  }

  async function onIngest() {
    if (!sourceId || !sheet) return;
    const columns: ColumnMap = {};
    if (mapMode === "question") {
      columns.question = questionCol;
      if (answerCol) columns.answer = answerCol;
      if (explanationCol) columns.explanation = explanationCol;
    } else {
      columns.content = contentCol;
    }
    setBusy(true);
    setError(null);
    try {
      await ingestDocument(baseUrl, sourceId, sheet, columns);
      await pollUntilSettled(sourceId);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "ingest failed");
    } finally {
      setBusy(false);
    }
  }

  async function onGenerate() {
    if (!sourceId) return;
    setBusy(true);
    setError(null);
    try {
      const result = await generateQuiz(baseUrl, {
        document_ids: [sourceId],
        n,
        mode: selected?.source_kind === "pdf" ? "generate" : mode,
      });
      setQuestions(result.questions);
      setShown({});
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "quiz failed");
    } finally {
      setBusy(false);
    }
  }

  function answerVisible(index: number): boolean {
    if (Object.prototype.hasOwnProperty.call(shown, index)) {
      return shown[index];
    }
    return showAllAnswers;
  }

  return (
    <section>
      <h2>Quiz</h2>
      <p className="lede">Pick a source and generate A–D questions. Answer keys stay hidden until you toggle them.</p>

      <div className="row" style={{ marginTop: "var(--dt-space-4)" }}>
        <label>
          Source
          <select value={sourceId} onChange={(e) => setSourceId(e.target.value)}>
            <option value="">Select a document</option>
            {docs.map((d) => (
              <option key={d.id} value={d.id}>
                {d.filename} ({documentCountLabel(d)})
              </option>
            ))}
          </select>
        </label>
        <label>
          Questions
          <input
            type="number"
            min={1}
            max={20}
            value={n}
            onChange={(e) => setN(Math.max(1, Math.min(20, Number(e.target.value) || 1)))}
          />
        </label>
        {selected && selected.source_kind !== "pdf" ? (
          <label>
            Mode
            <select value={mode} onChange={(e) => setMode(e.target.value as QuizMode)}>
              <option value="generate">Generate with Gemini</option>
              <option value="from_columns">From question/answer columns</option>
            </select>
          </label>
        ) : null}
        <label>
          Upload PDF, XLSX, or CSV
          <input
            type="file"
            accept=".pdf,.xlsx,.csv,application/pdf,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) void onUpload(file);
              e.target.value = "";
            }}
          />
        </label>
      </div>

      {selected?.status === "mapping" ? (
        <div className="question">
          <p>Map columns for {selected.filename}.</p>
          <div className="row">
            <label>
              Sheet
              <select value={sheet} onChange={(e) => setSheet(e.target.value)}>
                {(selected.sheets ?? []).map((s) => (
                  <option key={s.name} value={s.name}>
                    {s.name} ({s.row_count} rows)
                  </option>
                ))}
              </select>
            </label>
            <label>
              Map
              <select value={mapMode} onChange={(e) => setMapMode(e.target.value as "question" | "content")}>
                <option value="question">Question column</option>
                <option value="content">Content column</option>
              </select>
            </label>
            {mapMode === "question" ? (
              <>
                <label>
                  Question
                  <select value={questionCol} onChange={(e) => setQuestionCol(e.target.value)}>
                    {sheetHeaders.map((h) => (
                      <option key={h} value={h}>
                        {h}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Answer (optional)
                  <select value={answerCol} onChange={(e) => setAnswerCol(e.target.value)}>
                    <option value="">None</option>
                    {sheetHeaders.map((h) => (
                      <option key={h} value={h}>
                        {h}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Explanation (optional)
                  <select value={explanationCol} onChange={(e) => setExplanationCol(e.target.value)}>
                    <option value="">None</option>
                    {sheetHeaders.map((h) => (
                      <option key={h} value={h}>
                        {h}
                      </option>
                    ))}
                  </select>
                </label>
              </>
            ) : (
              <label>
                Content
                <select value={contentCol} onChange={(e) => setContentCol(e.target.value)}>
                  {sheetHeaders.map((h) => (
                    <option key={h} value={h}>
                      {h}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <button type="button" onClick={() => void onIngest()} disabled={busy}>
              Ingest sheet
            </button>
          </div>
        </div>
      ) : null}

      <div className="row" style={{ marginTop: "var(--dt-space-4)" }}>
        <button type="button" onClick={() => void onGenerate()} disabled={busy || !sourceId || selected?.status !== "done"}>
          {questions.length ? "Regenerate" : "Generate quiz"}
        </button>
        <label className="toggle">
          <input
            type="checkbox"
            checked={showAllAnswers}
            onChange={(e) => {
              setShowAllAnswers(e.target.checked);
              setShown({});
            }}
          />
          Show answer key
        </label>
      </div>

      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}
      {!docs.length ? <p>Upload a document to quiz it.</p> : null}
      {readyDocs.length + mappingDocs.length === 0 && docs.length ? <p>Waiting for a ready source…</p> : null}

      {questions.map((q, index) => {
        const visible = answerVisible(index);
        return (
          <article className="question" key={`${q.prompt}-${index}`}>
            <h3>
              {index + 1}. {q.prompt}
            </h3>
            <ul className="options">
              {optionLetters.map((letter) => (
                <li key={letter} className={visible && q.correct === letter ? "correct" : undefined}>
                  <strong>{letter}.</strong> {q.options[letter]}
                </li>
              ))}
            </ul>
            <label className="toggle">
              <input
                type="checkbox"
                checked={visible}
                onChange={(e) => setShown((prev) => ({ ...prev, [index]: e.target.checked }))}
              />
              Show answer
            </label>
            {visible ? (
              <div className="answer-key">
                <p>
                  Correct: {q.correct}. {q.explanation}
                </p>
                <p className="citation">Citation: {formatCitation(q.citation)}</p>
              </div>
            ) : null}
          </article>
        );
      })}
    </section>
  );
}

function pickHeader(headers: string[], names: string[]): string {
  const lower = names.map((n) => n.toLowerCase());
  return headers.find((h) => lower.includes(h.toLowerCase())) ?? headers[0] ?? "";
}

function documentCountLabel(d: DocumentRecord): string {
  if (d.row_count != null) {
    return `${d.status}, ${d.row_count} rows`;
  }
  if (d.page_count != null) {
    return `${d.status}, ${d.page_count} pages`;
  }
  return d.status;
}
