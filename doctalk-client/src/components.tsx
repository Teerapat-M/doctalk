import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import type { Citation } from "./quiz";
import { formatCitation } from "./quiz";
import type { ColumnPickerState, ColumnRole } from "./columnPicker";
import { excelColLetter, selectSheet, setRole, sheetHeaders } from "./columnPicker";
import type { SheetInfo } from "./documents";
import type { OptionState } from "./quizToggles";

type Variant = "primary" | "secondary" | "ghost";

export function Button({
  variant = "primary",
  className = "",
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }) {
  const extra = variant === "primary" ? "" : variant === "secondary" ? " dt-btn-secondary" : " dt-btn-ghost";
  return <button type={type} className={`dt-btn${extra} ${className}`.trim()} {...props} />;
}

export function IconButton({ className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button type="button" className={`dt-icon-btn ${className}`.trim()} {...props} />;
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="dt-field">
      {label}
      {children}
    </label>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className="dt-input" {...props} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className="dt-input" {...props} />;
}

export function TextArea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className="dt-input" {...props} />;
}

export function Chip({
  children,
  variant = "default",
  title,
}: {
  children: ReactNode;
  variant?: "default" | "accent" | "danger";
  title?: string;
}) {
  const extra = variant === "accent" ? " dt-chip-accent" : variant === "danger" ? " dt-chip-danger" : "";
  return (
    <span className={`dt-chip${extra}`} title={title}>
      {children}
    </span>
  );
}

export function CitationChip({ citation }: { citation: Citation }) {
  return (
    <Chip variant="accent" title={citation.snippet}>
      {formatCitation(citation)}
      {citation.snippet ? <span className="dt-chip-snippet">{citation.snippet}</span> : null}
    </Chip>
  );
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`dt-card ${className}`.trim()}>{children}</div>;
}

const acceptTypes =
  ".pdf,.xlsx,.csv,application/pdf,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv";

export function Dropzone({
  onFile,
  busy,
  title = "ลากไฟล์ PDF หรือ .xlsx มาวางที่นี่",
  hint = "Drag & drop a PDF or Excel file, or click to browse",
}: {
  onFile: (file: File) => void;
  busy?: boolean;
  title?: string;
  hint?: string;
}) {
  return (
    <label
      className="dt-dropzone"
      data-active="false"
      onDragOver={(e) => {
        e.preventDefault();
        e.currentTarget.dataset.active = "true";
      }}
      onDragLeave={(e) => {
        e.currentTarget.dataset.active = "false";
      }}
      onDrop={(e) => {
        e.preventDefault();
        e.currentTarget.dataset.active = "false";
        const file = e.dataTransfer.files[0];
        if (file) onFile(file);
      }}
    >
      <strong>{busy ? "กำลังอัปโหลด…" : title}</strong>
      <span>{hint}</span>
      <input
        type="file"
        accept={acceptTypes}
        hidden
        disabled={busy}
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) onFile(file);
          e.target.value = "";
        }}
      />
    </label>
  );
}

export function MessageBubble({
  role,
  text,
  citations,
  error,
  onRetry,
}: {
  role: "user" | "assistant";
  text: string;
  citations?: Citation[];
  error?: string;
  onRetry?: () => void;
}) {
  return (
    <article className={`dt-msg dt-msg-${role}`}>
      {error ? (
        <div className="dt-error-panel" role="alert">
          <p>{error}</p>
          {onRetry ? (
            <Button variant="secondary" onClick={onRetry}>
              ↻ ลองใหม่ · Retry
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="dt-msg-bubble">{text || (role === "assistant" ? "…" : "")}</div>
      )}
      {citations && citations.length > 0 ? (
        <div className="dt-msg-citations">
          {citations.map((c) => (
            <CitationChip key={c.chunk_id} citation={c} />
          ))}
        </div>
      ) : null}
    </article>
  );
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="dt-empty">
      <strong>{title}</strong>
      {children}
    </div>
  );
}

export function ErrorPanel({ children }: { children: ReactNode }) {
  return (
    <div className="dt-error-panel" role="alert">
      {children}
    </div>
  );
}

export function Header({
  active,
  theme,
  onToggleTheme,
  onMenu,
}: {
  active: "docs" | "quiz";
  theme: "light" | "dark";
  onToggleTheme: () => void;
  onMenu?: () => void;
}) {
  return (
    <header className="dt-header">
      {onMenu ? (
        <IconButton className="menu-btn" aria-label="Open documents" onClick={onMenu}>
          ☰
        </IconButton>
      ) : null}
      <a className="dt-brand" href="#/">
        <span className="dt-brand-mark" aria-hidden>
          D
        </span>
        DocTalk
      </a>
      <nav className="dt-nav" aria-label="Primary">
        <a href="#/" data-active={active === "docs" ? "true" : "false"}>
          เอกสาร
        </a>
        <a href="#/quiz" data-active={active === "quiz" ? "true" : "false"}>
          แบบทดสอบ · Quiz
        </a>
      </nav>
      <div className="header-actions">
        <Toggle
          label={theme === "dark" ? "Dark" : "Light"}
          on={theme === "dark"}
          onChange={onToggleTheme}
        />
      </div>
    </header>
  );
}

export function Toggle({ label, on, onChange }: { label: string; on: boolean; onChange: () => void }) {
  return (
    <label className="dt-toggle">
      <button type="button" className="dt-toggle-track" data-on={on ? "true" : "false"} aria-pressed={on} onClick={onChange} />
      {label}
    </label>
  );
}

export function AlertBanner({
  tone = "info",
  children,
}: {
  tone?: "info" | "warning" | "danger";
  children: ReactNode;
}) {
  return (
    <div className={`dt-alert dt-alert-${tone}`} role={tone === "danger" ? "alert" : "status"}>
      {children}
    </div>
  );
}

export function OptionRow({
  letter,
  text,
  state,
  onClick,
}: {
  letter: string;
  text: string;
  state: OptionState;
  onClick?: () => void;
}) {
  const tag =
    state === "correct"
      ? "✓ ถูกต้อง"
      : state === "incorrect"
        ? "✕ ไม่ถูก"
        : state === "revealed"
          ? "เฉลย"
          : state === "selected"
            ? "เลือกแล้ว"
            : null;
  return (
    <button type="button" className={`dt-option dt-option-${state}`} onClick={onClick} disabled={!onClick}>
      <span className="option-badge">{letter}</span>
      <span className="option-text">{text}</span>
      {tag ? <span className="option-tag">{tag}</span> : null}
    </button>
  );
}

export function Stepper({
  value,
  min = 1,
  max = 20,
  onChange,
}: {
  value: number;
  min?: number;
  max?: number;
  onChange: (n: number) => void;
}) {
  const atMax = value >= max;
  return (
    <div className="dt-stepper" data-state={atMax ? "max" : "default"}>
      <div className="dt-stepper-frame">
        <button type="button" onClick={() => onChange(Math.max(min, value - 1))} disabled={value <= min} aria-label="Decrease">
          −
        </button>
        <span>{value}</span>
        <button type="button" onClick={() => onChange(Math.min(max, value + 1))} disabled={atMax} aria-label="Increase">
          +
        </button>
      </div>
      <p className="dt-stepper-hint">{atMax ? `สูงสุด ${max} ข้อ · Max ${max}` : `1–${max} ข้อ · questions`}</p>
    </div>
  );
}

export function Skeleton({ type = "line" }: { type?: "line" | "question" }) {
  if (type === "line") return <div className="dt-skeleton dt-skeleton-line" aria-hidden />;
  return (
    <div className="dt-skeleton dt-skeleton-question" aria-hidden>
      <div className="dt-skeleton-line" />
      <div className="dt-skeleton-line" />
      <div className="dt-skeleton-line dt-skeleton-short" />
      <div className="dt-skeleton-line" />
      <div className="dt-skeleton-line" />
    </div>
  );
}

const progressStages = [
  { key: "uploading", label: "อัปโหลด" },
  { key: "extracting", label: "ดึงข้อความ" },
  { key: "embedding", label: "Embedding" },
  { key: "done", label: "พร้อม" },
] as const;

export function Progress({ stage }: { stage: string }) {
  const order = progressStages.map((s) => s.key);
  const idx = order.indexOf(stage as (typeof order)[number]);
  const current = stage === "done" ? 3 : Math.max(0, idx);
  return (
    <ol className="dt-progress">
      {progressStages.map((s, i) => {
        const state = i < current ? "done" : i === current ? "current" : "todo";
        return (
          <li key={s.key} data-state={state}>
            {state === "done" ? "✓" : state === "current" ? "●" : "○"} {s.label}
          </li>
        );
      })}
    </ol>
  );
}

const roleLabels: Record<ColumnRole, string> = {
  question: "เนื้อหา/คำถาม · Question",
  answer: "คำตอบ · Answer (opt.)",
  explanation: "คำอธิบาย · Explain (opt.)",
  ignore: "ไม่ใช้ · Ignore",
};

export function ColumnPicker({
  filename,
  sheets,
  state,
  onChange,
  onSubmit,
  busy,
}: {
  filename: string;
  sheets: SheetInfo[];
  state: ColumnPickerState;
  onChange: (next: ColumnPickerState) => void;
  onSubmit: () => void;
  busy?: boolean;
}) {
  const headers = sheetHeaders(sheets, state.sheet);
  const rows = sheets.find((s) => s.name === state.sheet)?.row_count;
  return (
    <Card>
      <h2>เลือกชีตและคอลัมน์</h2>
      <p className="lede">
        {filename}
        {sheets.length ? ` · ${sheets.length} sheets` : ""}
        {rows != null ? ` · ${rows} rows` : ""}
      </p>
      <div className="sheet-tabs">
        {sheets.map((s) => (
          <button
            key={s.name}
            type="button"
            className="sheet-tab"
            data-active={s.name === state.sheet ? "true" : "false"}
            onClick={() => onChange(selectSheet(sheets, s.name))}
          >
            {s.name}
          </button>
        ))}
      </div>
      <p className="lede" style={{ marginTop: "var(--dt-space-4)" }}>
        จับคู่คอลัมน์ · Map columns
      </p>
      <div className="mapper-grid">
        {headers.map((h, i) => (
          <label key={h} className="dt-mapper" data-role={state.roles[h] ?? "ignore"}>
            <span className="dt-mapper-col">
              {excelColLetter(i)} · {h}
            </span>
            <select
              className="dt-input"
              value={state.roles[h] ?? "ignore"}
              onChange={(e) => onChange(setRole(state, h, e.target.value as ColumnRole))}
            >
              {(Object.keys(roleLabels) as ColumnRole[]).map((role) => (
                <option key={role} value={role}>
                  {roleLabels[role]}
                </option>
              ))}
            </select>
          </label>
        ))}
      </div>
      <div className="row" style={{ marginTop: "var(--dt-space-4)" }}>
        <Button onClick={onSubmit} disabled={busy || !state.sheet}>
          นำเข้า · Ingest
        </Button>
      </div>
    </Card>
  );
}
