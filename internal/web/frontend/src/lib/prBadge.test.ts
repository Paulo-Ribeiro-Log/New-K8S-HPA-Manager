import { describe, expect, it } from "vitest";
import { prBadge } from "./prBadge";

describe("prBadge", () => {
  it("sem status: azul, só aberto", () => {
    expect(prBadge({ draft: false })).toMatchObject({ tone: "blue", label: "" });
  });
  it("conflito vence tudo", () => {
    expect(prBadge({ draft: false, mergeable: "conflict", checks: "success", review: "approved" }).tone).toBe("red");
  });
  it("checks falhando é vermelho mesmo aprovado", () => {
    expect(prBadge({ draft: false, checks: "failure", review: "approved" }).tone).toBe("red");
  });
  it("alterações pedidas é laranja", () => {
    expect(prBadge({ draft: false, checks: "success", review: "changes_requested" }).tone).toBe("orange");
  });
  it("rascunho é cinza, mas checks falhando ainda é vermelho", () => {
    expect(prBadge({ draft: true }).tone).toBe("gray");
    expect(prBadge({ draft: true, checks: "failure" }).tone).toBe("red");
  });
  it("checks rodando é âmbar; aprovado com checks ok é verde", () => {
    expect(prBadge({ draft: false, checks: "pending", review: "approved" }).tone).toBe("amber");
    expect(prBadge({ draft: false, checks: "success", review: "approved" })).toMatchObject({ tone: "green", label: "aprovado" });
  });
  it("detalhes listam cada item", () => {
    expect(prBadge({ draft: true, checks: "pending", review: "review_required" }).details).toEqual(["Rascunho", "Checks em andamento", "Aguardando revisão"]);
  });
});
