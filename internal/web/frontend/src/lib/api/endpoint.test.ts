import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { _resetEndpointWarning, normalizeEndpoint } from "./endpoint";

describe("normalizeEndpoint (o apiClient já prefixa /api/v1)", () => {
  beforeEach(() => {
    _resetEndpointWarning();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("não mexe em endpoint normal", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    for (const e of ["/configmaps/c/ns/n", "/azure/storage-search?q=abc", "/dependencies/scan", "/", "/api/v2/x", "/apiary", "/api/v1x/y"]) {
      expect(normalizeEndpoint(e)).toBe(e);
    }
    expect(warn).not.toHaveBeenCalled();
  });

  it("remove o prefixo repetido — o bug da exclusão em lote de ConfigMap/Ingress/Secret", () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(normalizeEndpoint("/api/v1/configmaps/c/ns/n")).toBe("/configmaps/c/ns/n");
    expect(normalizeEndpoint("/api/v1/secrets/x")).toBe("/secrets/x");
    expect(normalizeEndpoint("/api/v1")).toBe("/");
    expect(normalizeEndpoint("/api/v1?x=1")).toBe("?x=1");
  });

  it("avisa uma vez só, para não inundar o console", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    normalizeEndpoint("/api/v1/a");
    normalizeEndpoint("/api/v1/b");
    normalizeEndpoint("/api/v1/c");
    expect(warn).toHaveBeenCalledTimes(1);
    expect(String(warn.mock.calls[0][0])).toContain("/api/v1/a");
  });
});
