import { afterEach, describe, expect, it, vi } from "vitest";
import { uploadDocument } from "./documents";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("uploadDocument", () => {
  it("shows the server max size from a 413 body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 413,
        json: async () => ({ error: "file too large (max 10 MB)", max_bytes: 10485760 }),
      })),
    );
    await expect(uploadDocument("", new File(["x"], "big.pdf"))).rejects.toThrow("file too large (max 10 MB)");
  });
});
