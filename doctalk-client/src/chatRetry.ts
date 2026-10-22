export type ChatRetryItem = {
  id: string;
  role: "user" | "assistant";
  text: string;
  error?: string;
};

export function retryQuestionOf<T extends ChatRetryItem>(items: T[], asstId: string): string | undefined {
  const idx = items.findIndex((m) => m.id === asstId);
  if (idx <= 0) return undefined;
  const prev = items[idx - 1];
  if (prev.role !== "user" || !prev.text.trim()) return undefined;
  return prev.text;
}

export function resetAssistantForRetry<T extends ChatRetryItem>(items: T[], asstId: string): T[] {
  return items.map((m) => (m.id === asstId ? { ...m, text: "", error: undefined } : m));
}

export function canRetryAssistant(item: ChatRetryItem): boolean {
  return item.role === "assistant" && Boolean(item.error);
}
