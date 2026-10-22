import { useEffect, useMemo, useState } from "react";
import {
  AlertBanner,
  Button,
  CitationChip,
  EmptyState,
  Field,
  OptionRow,
  Select,
  Skeleton,
  Stepper,
  Toggle,
} from "./components";
import { documentCountLabel, type DocumentRecord } from "./documents";
import { generateQuiz, type QuizMode, type QuizQuestion } from "./quiz";
import { fetchQuota, quizQuotaReached, type QuotaResponse } from "./quizQuota";
import {
  answerVisible,
  clampN,
  optionState,
  toggleAllAnswers,
  toggleOneAnswer,
  type PicksMap,
  type ShownMap,
} from "./quizToggles";

const optionLetters = ["A", "B", "C", "D"] as const;

type Props = {
  baseUrl?: string;
  docs?: DocumentRecord[];
  sourceId?: string;
};

export default function QuizPanel({
  baseUrl = import.meta.env.VITE_API_URL ?? "",
  docs = [],
  sourceId: sourceIdProp,
}: Props) {
  const [sourceId, setSourceId] = useState(sourceIdProp ?? "");
  useEffect(() => {
    if (sourceIdProp) setSourceId(sourceIdProp);
  }, [sourceIdProp]);
  const [n, setN] = useState(5);
  const [mode, setMode] = useState<QuizMode>("generate");
  const [questions, setQuestions] = useState<QuizQuestion[]>([]);
  const [showAllAnswers, setShowAllAnswers] = useState(false);
  const [shown, setShown] = useState<ShownMap>({});
  const [picks, setPicks] = useState<PicksMap>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [quota, setQuota] = useState<QuotaResponse | null>(null);

  const selected = useMemo(() => docs.find((d) => d.id === sourceId), [docs, sourceId]);
  const readyDocs = docs.filter((d) => d.status === "done");
  const quizQuota = quota?.quiz;
  const quotaReached = quizQuotaReached(quota);
  const quotaLimit = quizQuota?.limit || 5;
  const quotaRemaining = quizQuota?.remaining ?? quotaLimit;

  useEffect(() => {
    let cancelled = false;
    fetchQuota(baseUrl)
      .then((q) => {
        if (!cancelled) setQuota(q);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [baseUrl]);

  async function refreshQuota() {
    try {
      setQuota(await fetchQuota(baseUrl));
    } catch {
      /* keep last snapshot */
    }
  }

  async function onGenerate() {
    if (!sourceId || quotaReached) return;
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
      setPicks({});
      await refreshQuota();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "quiz failed");
      await refreshQuota();
    } finally {
      setBusy(false);
    }
  }

  const answered = Object.keys(picks).length;
  const scored = questions.filter((q, i) => answerVisible(i, shown, showAllAnswers) && picks[i] === q.correct).length;
  const revealedCount = questions.filter((_, i) => answerVisible(i, shown, showAllAnswers)).length;

  return (
    <section className="quiz-panel">
      <h2>สร้างแบบทดสอบ</h2>
      <p className="lede">Generate a multiple-choice quiz from your documents.</p>

      <div className="source-grid">
        {readyDocs.map((d) => (
          <button
            key={d.id}
            type="button"
            className="source-card"
            data-active={d.id === sourceId ? "true" : "false"}
            onClick={() => setSourceId(d.id)}
          >
            <span className="source-kind">{d.source_kind === "pdf" ? "PDF" : "XLS"}</span>
            <strong>{d.filename}</strong>
            <span className="doc-item-meta">{documentCountLabel(d)}</span>
          </button>
        ))}
      </div>

      <div className="row" style={{ marginTop: "var(--dt-space-4)" }}>
        {readyDocs.length === 0 ? (
          <Field label="แหล่งที่มา · Source">
            <Select value={sourceId} onChange={(e) => setSourceId(e.target.value)}>
              <option value="">Select a document</option>
              {docs.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.filename} ({documentCountLabel(d)})
                </option>
              ))}
            </Select>
          </Field>
        ) : null}
        <div className="dt-field">
          จำนวนคำถาม · Questions
          <Stepper value={n} onChange={(v) => setN(clampN(v))} />
        </div>
        {selected && selected.source_kind !== "pdf" ? (
          <Field label="Mode">
            <Select value={mode} onChange={(e) => setMode(e.target.value as QuizMode)}>
              <option value="generate">Generate with Gemini</option>
              <option value="from_columns">From question/answer columns</option>
            </Select>
          </Field>
        ) : null}
        <Button onClick={() => void onGenerate()} disabled={busy || !sourceId || selected?.status !== "done" || quotaReached}>
          {questions.length ? "↻ สร้างใหม่" : "สร้างแบบทดสอบ · Generate"}
        </Button>
        <Toggle
          label="แสดงเฉลย · Show answers"
          on={showAllAnswers}
          onChange={() => {
            const next = toggleAllAnswers(!showAllAnswers);
            setShowAllAnswers(next.showAll);
            setShown(next.shown);
          }}
        />
      </div>
        <p className="lede">ใช้โควตา 1 ครั้ง · เหลือ {quotaRemaining}/{quotaLimit} ครั้งวันนี้</p>

      {quotaReached ? (
        <EmptyState title="ใช้โควตาวันนี้ครบแล้ว">
          <p>Daily quiz limit reached ({quotaLimit}/{quotaLimit}). โควตาจะรีเซ็ตตอน 07:00 (เวลาไทย)</p>
          {questions.length ? <p>แบบทดสอบเดิมยังเปิดดูได้</p> : null}
        </EmptyState>
      ) : null}

      {busy ? (
        <div className="quiz-generating">
          <h3>กำลังสร้างแบบทดสอบ…</h3>
          <p className="lede">Generating questions from your documents</p>
          <Skeleton type="question" />
          <Skeleton type="question" />
        </div>
      ) : null}

      {error && !busy ? (
        <AlertBanner tone="danger">
          <strong>สร้างแบบทดสอบไม่สำเร็จ</strong>
          <p>{error}</p>
          <Button variant="secondary" onClick={() => void onGenerate()}>
            ↻ ลองใหม่ · Retry
          </Button>
        </AlertBanner>
      ) : null}

      {!busy && !error && !questions.length && !quotaReached ? (
        readyDocs.length === 0 ? (
          <EmptyState title="ยังไม่มีแบบทดสอบ">
            No quizzes yet. อัปโหลด PDF หรือนำเข้า Excel ก่อน แล้วสร้างแบบทดสอบได้ในคลิกเดียว
          </EmptyState>
        ) : (
          <p className="lede">ยังไม่มีแบบทดสอบ · No quizzes yet.</p>
        )
      ) : null}

      {questions.length > 0 && !busy ? (
        <p className="lede">
          ตอบแล้ว {answered}/{questions.length}
          {revealedCount ? ` · คะแนน ${scored}/${revealedCount} · Score` : ""}
        </p>
      ) : null}

      {!busy &&
        questions.map((q, index) => {
          const visible = answerVisible(index, shown, showAllAnswers);
          return (
            <article className="question" key={`${q.prompt}-${index}`}>
              <h3>
                ข้อ {index + 1} / {questions.length}. {q.prompt}
              </h3>
              <div className="options">
                {optionLetters.map((letter) => (
                  <OptionRow
                    key={letter}
                    letter={letter}
                    text={q.options[letter]}
                    state={optionState(letter, picks[index], q.correct, visible)}
                    onClick={visible ? undefined : () => setPicks((prev) => ({ ...prev, [index]: letter }))}
                  />
                ))}
              </div>
              <Toggle
                label="Show answer"
                on={visible}
                onChange={() => setShown(toggleOneAnswer(shown, index, !visible))}
              />
              {visible ? (
                <div className="answer-key">
                  <p>
                    <strong>คำอธิบาย · Explanation.</strong> {q.explanation}
                  </p>
                  <p className="citation">
                    <CitationChip citation={q.citation} />
                  </p>
                </div>
              ) : null}
            </article>
          );
        })}
    </section>
  );
}
