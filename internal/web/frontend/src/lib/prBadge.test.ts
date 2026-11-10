import { describe, expect, it } from "vitest";
import { prBadge } from "./prBadge";

describe("prBadge (padrão do GitHub)", () => {
  it("aberto é verde, rascunho é cinza, mergeado roxo e fechado vermelho", () => {
    expect(prBadge({ draft: false }).tone).toBe("green");
    expect(prBadge({ state: "open", draft: false }).tone).toBe("green");
    expect(prBadge({ draft: true }).tone).toBe("gray");
    expect(prBadge({ state: "merged", draft: false }).tone).toBe("purple");
    expect(prBadge({ state: "closed", draft: false }).tone).toBe("red");
  });
  it("fechado e mergeado ficam esmaecidos; aberto não", () => {
    expect(prBadge({ state: "closed", draft: false }).dimmed).toBe(true);
    expect(prBadge({ state: "merged", draft: false }).dimmed).toBe(true);
    expect(prBadge({ draft: false }).dimmed).toBe(false);
  });
  it("checks, revisão e conflito não mudam a cor do estado", () => {
    const b = prBadge({ draft: false, checks: "failure", review: "changes_requested", mergeable: "conflict" });
    expect(b.tone).toBe("green");
    expect(b.indicators).toEqual({ checks: "failure", review: "changes_requested", conflict: true });
  });
  it("indicadores de checks e aprovação", () => {
    expect(prBadge({ draft: false, checks: "pending" }).indicators.checks).toBe("pending");
    expect(prBadge({ draft: false, checks: "success", review: "approved" }).indicators).toEqual({ checks: "success", review: "approved", conflict: false });
    expect(prBadge({ draft: false, checks: "none", review: "none", mergeable: "unknown" }).indicators).toEqual({ conflict: false });
  });
  it("fechado e mergeado ignoram checks e revisão", () => {
    expect(prBadge({ state: "closed", draft: false, checks: "failure", mergeable: "conflict" }).indicators).toEqual({ conflict: false });
    expect(prBadge({ state: "merged", draft: false, review: "approved" }).details).toEqual(["Mergeado"]);
  });
  it("detalhes listam estado e cada item", () => {
    expect(prBadge({ draft: true, checks: "pending", review: "review_required" }).details).toEqual(["Rascunho", "Checks em andamento", "Aguardando revisão"]);
  });
});
