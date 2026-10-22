import { Button, ErrorPanel, EmptyState } from "./components";

export default function ErrorScreen({
  kind,
  message,
}: {
  kind: "not-found" | "error";
  message?: string;
}) {
  if (kind === "not-found") {
    return (
      <div className="landing">
        <EmptyState title="ยังไม่พบเอกสาร">
          <p>Document not found. The file may have been removed, or the link is wrong.</p>
          <p>
            <Button onClick={() => (window.location.hash = "#/")}>กลับหน้าแรก</Button>
          </p>
        </EmptyState>
      </div>
    );
  }
  return (
    <div className="landing">
      <ErrorPanel>
        <strong>เกิดข้อผิดพลาด</strong>
        <p>{message || "Something went wrong."}</p>
      </ErrorPanel>
      <p>
        <Button variant="secondary" onClick={() => (window.location.hash = "#/")}>
          กลับหน้าแรก
        </Button>
      </p>
    </div>
  );
}
