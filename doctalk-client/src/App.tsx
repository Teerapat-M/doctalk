import { useEffect, useState } from "react";
import { fetchHealth, type HealthResponse } from "./health";
import QuizPanel from "./QuizPanel";

const apiBase = import.meta.env.VITE_API_URL ?? "";

export default function App() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchHealth(apiBase)
      .then((payload) => {
        if (!cancelled) setHealth(payload);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "unknown error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <main>
      <h1>DocTalk</h1>
      <p className="lede">Ask questions about your PDFs and spreadsheets.</p>
      <section aria-live="polite">
        <h2>API health</h2>
        {error ? (
          <p className="error" role="alert">
            Could not reach API: {error}
          </p>
        ) : null}
        {health ? (
          <ul>
            <li>status: {health.status}</li>
            <li>database: {health.checks.database}</li>
          </ul>
        ) : null}
        {!error && !health ? <p>Checking…</p> : null}
      </section>
      <QuizPanel />
    </main>
  );
}
