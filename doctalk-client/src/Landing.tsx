import { useEffect, useState } from "react";
import { buildColumnMap, defaultColumnPicker, sheetHeaders, type ColumnPickerState } from "./columnPicker";
import { Button, Card, ColumnPicker, Dropzone, EmptyState, ErrorPanel, Progress } from "./components";
import { documentCountLabel, type DocumentRecord } from "./documents";
import { hashFor } from "./route";

type Props = {
  docs: DocumentRecord[];
  busy: boolean;
  error: string | null;
  mappingDoc: DocumentRecord | null;
  onUpload: (file: File) => void;
  onIngest: (id: string, sheet: string, columns: ReturnType<typeof buildColumnMap>) => void;
};

export default function Landing({ docs, busy, error, mappingDoc, onUpload, onIngest }: Props) {
  const importingDoc =
    docs.find((d) => d.status === "uploading" || d.status === "extracting" || d.status === "embedding") ?? null;
  const [picker, setPicker] = useState<ColumnPickerState>(defaultColumnPicker(mappingDoc?.sheets));

  useEffect(() => {
    setPicker(defaultColumnPicker(mappingDoc?.sheets));
  }, [mappingDoc?.id, mappingDoc?.sheets]);

  const step = importingDoc ? 3 : mappingDoc ? 2 : 1;

  return (
    <div className="landing">
      <section className="landing-hero">
        <p className="steps">
          <span data-active={step === 1 ? "true" : "false"}>1. อัปโหลด</span>
          <span data-active={step === 2 ? "true" : "false"}>2. เลือกชีตและคอลัมน์</span>
          <span data-active={step === 3 ? "true" : "false"}>3. นำเข้า</span>
        </p>
        <h1>{step === 2 ? "เลือกชีตและคอลัมน์" : step === 3 ? "กำลังนำเข้า…" : "ถามเอกสารของคุณ"}</h1>
        <p className="lede">
          {step === 2
            ? "Map columns, then ingest. Question / Answer / Explanation / Ignore."
            : step === 3
              ? `Importing “${importingDoc?.filename ?? ""}”`
              : "Ask questions about your PDFs and spreadsheets. Upload a file, then chat or generate an A–D quiz."}
        </p>
      </section>

      {importingDoc ? (
        <Card>
          <p>
            <strong>{importingDoc.filename}</strong>
          </p>
          <Progress stage={importingDoc.status} />
        </Card>
      ) : mappingDoc ? (
        <ColumnPicker
          filename={mappingDoc.filename}
          sheets={mappingDoc.sheets ?? []}
          state={picker}
          onChange={setPicker}
          busy={busy}
          onSubmit={() =>
            onIngest(mappingDoc.id, picker.sheet, buildColumnMap(picker, sheetHeaders(mappingDoc.sheets, picker.sheet)))
          }
        />
      ) : (
        <>
          <Dropzone onFile={onUpload} busy={busy} />
          <p className="lede">ตัวอย่าง: PDF ทีละหน้า หรือแถวแรกของ Excel เป็นหัวคอลัมน์ เช่น คำถาม | คำตอบ | คำอธิบาย</p>
        </>
      )}

      {error ? <ErrorPanel>{error}</ErrorPanel> : null}

      <section>
        <h2>เอกสาร</h2>
        {docs.length === 0 ? (
          <EmptyState title="ยังไม่มีเอกสาร">Upload a PDF or .xlsx to get started.</EmptyState>
        ) : (
          <ul className="doc-list landing-docs">
            {docs.map((d) => (
              <li key={d.id}>
                <Card>
                  <a href={hashFor({ name: "workspace", id: d.id, tab: "chat" })}>
                    <strong>{d.filename}</strong>
                  </a>
                  <p className="doc-item-meta">{documentCountLabel(d)}</p>
                  {d.status === "failed" && d.error_message ? <p className="error">{d.error_message}</p> : null}
                  {d.status === "done" ? (
                    <p className="row" style={{ marginTop: "var(--dt-space-2)" }}>
                      <Button
                        variant="secondary"
                        onClick={() => (window.location.hash = hashFor({ name: "workspace", id: d.id, tab: "chat" }))}
                      >
                        Ask
                      </Button>
                      <Button
                        variant="ghost"
                        onClick={() => (window.location.hash = hashFor({ name: "workspace", id: d.id, tab: "quiz" }))}
                      >
                        Quiz
                      </Button>
                    </p>
                  ) : null}
                </Card>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
