import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { streamAsk } from "./ask";
import { canRetryAssistant, resetAssistantForRetry, retryQuestionOf } from "./chatRetry";
import { buildColumnMap, defaultColumnPicker, sheetHeaders, type ColumnPickerState } from "./columnPicker";
import {
  Button,
  ColumnPicker,
  Dropzone,
  EmptyState,
  ErrorPanel,
  Input,
  MessageBubble,
  Progress,
} from "./components";
import { documentCountLabel, type ColumnMap, type DocumentRecord } from "./documents";
import QuizPanel from "./QuizPanel";
import type { Citation } from "./quiz";
import { hashFor } from "./route";

type ChatItem = {
  id: string;
  role: "user" | "assistant";
  text: string;
  citations?: Citation[];
  error?: string;
};

type Props = {
  baseUrl: string;
  docs: DocumentRecord[];
  docsReady?: boolean;
  selectedId?: string;
  tab: "chat" | "quiz";
  busy: boolean;
  error: string | null;
  menuOpen: boolean;
  onCloseMenu: () => void;
  onUpload: (file: File) => void;
  onIngest: (id: string, sheet: string, columns: ColumnMap) => void;
  onMissing: () => void;
};

export default function Workspace({
  baseUrl,
  docs,
  docsReady = false,
  selectedId,
  tab,
  busy,
  error,
  menuOpen,
  onCloseMenu,
  onUpload,
  onIngest,
  onMissing,
}: Props) {
  const selected = useMemo(() => docs.find((d) => d.id === selectedId), [docs, selectedId]);
  const [picker, setPicker] = useState<ColumnPickerState>(defaultColumnPicker(selected?.sheets));
  const [question, setQuestion] = useState("");
  const [messages, setMessages] = useState<ChatItem[]>([]);
  const [streaming, setStreaming] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const logRef = useRef<HTMLDivElement | null>(null);
  const messagesRef = useRef<ChatItem[]>([]);
  messagesRef.current = messages;

  useEffect(() => {
    setPicker(defaultColumnPicker(selected?.sheets));
    setMessages([]);
    abortRef.current?.abort();
    setStreaming(false);
  }, [selected?.id, selected?.sheets]);

  useEffect(() => {
    if (docsReady && selectedId && !selected) {
      onMissing();
    }
  }, [docsReady, selectedId, selected, onMissing]);

  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight);
  }, [messages, streaming]);

  async function runAsk(q: string, asstId: string) {
    if (!selected || selected.status !== "done") return;
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setStreaming(true);
    setMessages((prev) => resetAssistantForRetry(prev, asstId));
    try {
      await streamAsk(
        baseUrl,
        { question: q, document_ids: [selected.id] },
        {
          onDelta: (text) => {
            setMessages((prev) =>
              prev.map((m) => (m.id === asstId ? { ...m, text: m.text + text } : m)),
            );
          },
          onFinal: (citations) => {
            setMessages((prev) => prev.map((m) => (m.id === asstId ? { ...m, citations } : m)));
          },
          onError: (err) => {
            setMessages((prev) =>
              prev.map((m) => (m.id === asstId ? { ...m, error: err, text: m.text || err } : m)),
            );
          },
        },
        ac.signal,
      );
    } catch (err: unknown) {
      if ((err as { name?: string }).name === "AbortError") return;
      const message = err instanceof Error ? err.message : "ask failed";
      setMessages((prev) =>
        prev.map((m) => (m.id === asstId ? { ...m, error: message, text: m.text || message } : m)),
      );
    } finally {
      setStreaming(false);
    }
  }

  async function onAsk(e: FormEvent) {
    e.preventDefault();
    const q = question.trim();
    if (!q || !selected || selected.status !== "done" || streaming) return;
    setQuestion("");
    const userId = `u-${Date.now()}`;
    const asstId = `a-${Date.now()}`;
    setMessages((prev) => [
      ...prev,
      { id: userId, role: "user", text: q },
      { id: asstId, role: "assistant", text: "" },
    ]);
    await runAsk(q, asstId);
  }

  function onRetry(asstId: string) {
    if (streaming) return;
    const q = retryQuestionOf(messagesRef.current, asstId);
    if (!q) return;
    void runAsk(q, asstId);
  }

  return (
    <div className="workspace">
      <div
        className="sidebar-backdrop"
        data-open={menuOpen ? "true" : "false"}
        onClick={onCloseMenu}
      />
      <aside className="sidebar" data-open={menuOpen ? "true" : "false"}>
        <Dropzone onFile={onUpload} busy={busy} title="อัปโหลดไฟล์" hint="PDF or .xlsx" />
        <h2>เอกสาร</h2>
        {docs.length === 0 ? (
          <p className="lede">No documents yet.</p>
        ) : (
          <ul className="doc-list">
            {docs.map((d) => (
              <li key={d.id}>
                <a
                  className="doc-item"
                  data-active={d.id === selectedId ? "true" : "false"}
                  href={hashFor({ name: "workspace", id: d.id, tab })}
                  onClick={onCloseMenu}
                >
                  <span className="doc-item-name">{d.filename}</span>
                  <span className="doc-item-meta">{documentCountLabel(d)}</span>
                </a>
              </li>
            ))}
          </ul>
        )}
      </aside>

      <section className="workspace-main">
        <div className="workspace-tabs">
          <a
            href={selectedId ? hashFor({ name: "workspace", id: selectedId, tab: "chat" }) : "#/workspace"}
            data-active={tab === "chat" ? "true" : "false"}
          >
            Ask
          </a>
          <a
            href={selectedId ? hashFor({ name: "workspace", id: selectedId, tab: "quiz" }) : "#/quiz"}
            data-active={tab === "quiz" ? "true" : "false"}
          >
            แบบทดสอบ · Quiz
          </a>
        </div>

        {error ? <ErrorPanel>{error}</ErrorPanel> : null}

        {tab === "quiz" ? (
          <QuizPanel baseUrl={baseUrl} docs={docs} sourceId={selectedId ?? ""} />
        ) : selected?.status === "mapping" ? (
          <div className="quiz-panel">
            <ColumnPicker
              filename={selected.filename}
              sheets={selected.sheets ?? []}
              state={picker}
              onChange={setPicker}
              busy={busy}
              onSubmit={() =>
                onIngest(selected.id, picker.sheet, buildColumnMap(picker, sheetHeaders(selected.sheets, picker.sheet)))
              }
            />
          </div>
        ) : selected?.status === "failed" ? (
          <div className="quiz-panel">
            <ErrorPanel>{selected.error_message || "Ingest failed."}</ErrorPanel>
          </div>
        ) : selected && selected.status !== "done" ? (
          <div className="quiz-panel">
            <EmptyState title="กำลังนำเข้า…">
              <Progress stage={selected.status} />
            </EmptyState>
          </div>
        ) : tab === "chat" ? (
          <>
            <div className="chat-log" ref={logRef}>
              {!selected ? (
                <EmptyState title="เลือกเอกสาร">Pick a source on the left, or upload a file.</EmptyState>
              ) : messages.length === 0 ? (
                <EmptyState title="ถามเอกสารนี้ · Ask this document">
                  Answers stream over SSE with page or sheet/row citations.
                </EmptyState>
              ) : (
                messages.map((m) => (
                  <MessageBubble
                    key={m.id}
                    role={m.role}
                    text={m.text}
                    citations={m.citations}
                    error={m.error}
                    onRetry={canRetryAssistant(m) ? () => onRetry(m.id) : undefined}
                  />
                ))
              )}
            </div>
            <form className="composer" onSubmit={(e) => void onAsk(e)}>
              <Input
                value={question}
                onChange={(e) => setQuestion(e.target.value)}
                placeholder="Ask a question…"
                disabled={!selected || selected.status !== "done" || streaming}
                aria-label="Question"
              />
              <Button type="submit" disabled={!selected || selected.status !== "done" || streaming || !question.trim()}>
                Send
              </Button>
            </form>
          </>
        ) : null}
      </section>
    </div>
  );
}
