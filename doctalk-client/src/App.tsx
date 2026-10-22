import { useCallback, useEffect, useState } from "react";
import { Header } from "./components";
import {
  fetchDocument,
  fetchDocuments,
  ingestDocument,
  uploadDocument,
  type ColumnMap,
  type DocumentRecord,
} from "./documents";
import ErrorScreen from "./ErrorScreen";
import Landing from "./Landing";
import { fetchQuota, formatQuotaLabel, type QuotaResponse } from "./quizQuota";
import { applyTheme, preferredTheme, toggleTheme, type ThemeMode } from "./theme";
import { hashFor, parseHash, type Route } from "./route";
import Workspace from "./Workspace";

const apiBase = import.meta.env.VITE_API_URL ?? "";

export default function App() {
  const [theme, setTheme] = useState<ThemeMode>(() =>
    typeof window === "undefined" ? "light" : preferredTheme(),
  );
  const [route, setRoute] = useState<Route>(() =>
    typeof window === "undefined" ? { name: "landing" } : parseHash(window.location.hash),
  );
  const [docs, setDocs] = useState<DocumentRecord[]>([]);
  const [docsReady, setDocsReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [quota, setQuota] = useState<QuotaResponse | null>(null);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    const onHash = () => {
      setRoute(parseHash(window.location.hash));
      setMenuOpen(false);
    };
    window.addEventListener("hashchange", onHash);
    if (!window.location.hash) window.location.hash = "#/";
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const refresh = useCallback(async () => {
    const list = await fetchDocuments(apiBase);
    setDocs(list);
    setDocsReady(true);
    return list;
  }, []);

  const refreshQuota = useCallback(async () => {
    try {
      setQuota(await fetchQuota(apiBase));
    } catch {
      /* keep last snapshot */
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    refresh().catch((err: unknown) => {
      if (!cancelled) setError(err instanceof Error ? err.message : "could not list documents");
    });
    void refreshQuota();
    return () => {
      cancelled = true;
    };
  }, [refresh, refreshQuota]);

  const mappingDoc = docs.find((d) => d.status === "mapping") ?? null;
  const selectedId = route.name === "workspace" ? route.id : undefined;
  const onMissing = useCallback(() => {
    window.location.hash = hashFor({ name: "not-found" });
  }, []);

  useEffect(() => {
    if (!docs.some((d) => d.status === "uploading" || d.status === "extracting" || d.status === "embedding")) {
      return;
    }
    const t = window.setInterval(() => {
      void refresh().catch(() => undefined);
    }, 1000);
    return () => window.clearInterval(t);
  }, [docs, refresh]);

  async function pollUntilSettled(id: string) {
    for (let i = 0; i < 30; i++) {
      const list = await refresh();
      const doc = list.find((d) => d.id === id) ?? (await fetchDocument(apiBase, id).catch(() => null));
      if (!doc || doc.status === "done" || doc.status === "failed" || doc.status === "mapping") {
        return doc;
      }
      await new Promise((r) => setTimeout(r, 1000));
    }
    return fetchDocument(apiBase, id).catch(() => null);
  }

  async function onUpload(file: File) {
    setError(null);
    setBusy(true);
    try {
      const doc = await uploadDocument(apiBase, file);
      await refresh();
      if (doc.status === "mapping") {
        window.location.hash = hashFor({ name: "landing" });
        return;
      }
      const settled = await pollUntilSettled(doc.id);
      if (settled?.status === "done") {
        window.location.hash = hashFor({ name: "workspace", id: settled.id, tab: "chat" });
      } else if (settled?.status === "failed") {
        setError(settled.error_message || "ingest failed");
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "upload failed");
    } finally {
      setBusy(false);
    }
  }

  async function onIngest(id: string, sheet: string, columns: ColumnMap) {
    setBusy(true);
    setError(null);
    try {
      await ingestDocument(apiBase, id, sheet, columns);
      const settled = await pollUntilSettled(id);
      if (settled?.status === "done") {
        window.location.hash = hashFor({ name: "workspace", id, tab: "chat" });
      } else if (settled?.status === "failed") {
        setError(settled.error_message || "ingest failed");
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "ingest failed");
    } finally {
      setBusy(false);
    }
  }

  const navActive = route.name === "workspace" && route.tab === "quiz" ? "quiz" : "docs";

  return (
    <div className="app-shell">
      <Header
        active={navActive}
        theme={theme}
        quotaLabel={formatQuotaLabel(quota)}
        onToggleTheme={() => setTheme((t) => {
          const next = toggleTheme(t);
          applyTheme(next);
          return next;
        })}
        onMenu={route.name === "workspace" ? () => setMenuOpen((v) => !v) : undefined}
      />
      <div className="app-body">
        {route.name === "not-found" ? (
          <ErrorScreen kind="not-found" />
        ) : route.name === "error" ? (
          <ErrorScreen kind="error" message={route.message} />
        ) : route.name === "workspace" ? (
          <Workspace
            baseUrl={apiBase}
            docs={docs}
            docsReady={docsReady}
            selectedId={selectedId}
            tab={route.tab}
            busy={busy}
            error={error}
            menuOpen={menuOpen}
            onCloseMenu={() => setMenuOpen(false)}
            onUpload={(f) => void onUpload(f)}
            onIngest={(id, sheet, columns) => void onIngest(id, sheet, columns)}
            onMissing={onMissing}
            quota={quota}
            refreshQuota={() => void refreshQuota()}
          />
        ) : (
          <Landing
            docs={docs}
            busy={busy}
            error={error}
            mappingDoc={mappingDoc}
            onUpload={(f) => void onUpload(f)}
            onIngest={(id, sheet, columns) => void onIngest(id, sheet, columns)}
          />
        )}
      </div>
    </div>
  );
}
