export type Route =
  | { name: "landing" }
  | { name: "workspace"; id?: string; tab: "chat" | "quiz" }
  | { name: "error"; message?: string }
  | { name: "not-found" };

export function parseHash(hash: string): Route {
  const trimmed = (hash.startsWith("#") ? hash.slice(1) : hash) || "/";
  const qIndex = trimmed.indexOf("?");
  const path = (qIndex >= 0 ? trimmed.slice(0, qIndex) : trimmed).replace(/\/+$/, "") || "/";
  const query = new URLSearchParams(qIndex >= 0 ? trimmed.slice(qIndex + 1) : "");
  const parts = path.split("/").filter(Boolean);
  if (parts.length === 0) return { name: "landing" };
  if (parts[0] === "quiz") return { name: "workspace", tab: "quiz" };
  if (parts[0] === "workspace") {
    if (!parts[1] || parts[1] === "quiz") {
      return { name: "workspace", tab: parts[1] === "quiz" ? "quiz" : "chat" };
    }
    return { name: "workspace", id: parts[1], tab: parts[2] === "quiz" ? "quiz" : "chat" };
  }
  if (parts[0] === "error") return { name: "error", message: query.get("message") ?? undefined };
  if (parts[0] === "not-found") return { name: "not-found" };
  return { name: "not-found" };
}

export function hashFor(route: Route): string {
  switch (route.name) {
    case "landing":
      return "#/";
    case "workspace":
      if (!route.id) return route.tab === "quiz" ? "#/quiz" : "#/workspace";
      return route.tab === "quiz" ? `#/workspace/${route.id}/quiz` : `#/workspace/${route.id}`;
    case "error":
      return route.message ? `#/error?message=${encodeURIComponent(route.message)}` : "#/error";
    case "not-found":
      return "#/not-found";
  }
}
