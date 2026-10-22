export type ThemeMode = "light" | "dark";

const storageKey = "dt-theme";

export function preferredTheme(): ThemeMode {
  if (typeof window === "undefined") return "light";
  const saved = window.localStorage.getItem(storageKey);
  if (saved === "light" || saved === "dark") return saved;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function applyTheme(theme: ThemeMode): void {
  document.documentElement.dataset.theme = theme;
  window.localStorage.setItem(storageKey, theme);
}

export function toggleTheme(theme: ThemeMode): ThemeMode {
  return theme === "dark" ? "light" : "dark";
}
