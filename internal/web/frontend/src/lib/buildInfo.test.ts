import { describe, expect, it } from "vitest";
import { buildLabel, describeBuild, isFrontendStale, loadedBundleName, type ServerBuild } from "./buildInfo";

describe("loadedBundleName", () => {
  it("acha o bundle principal entre os scripts da página", () => {
    expect(loadedBundleName([
      "http://localhost:8080/assets/vendor-AbCd_-12.js",
      "http://localhost:8080/assets/index-DHpr11oR.js",
    ])).toBe("index-DHpr11oR.js");
    expect(loadedBundleName(["/assets/index-X_y-9.js?v=2"])).toBe("index-X_y-9.js");
  });

  it("devolve vazio quando não há bundle (vite dev serve módulos soltos)", () => {
    expect(loadedBundleName([])).toBe("");
    expect(loadedBundleName(["http://localhost:5173/src/main.tsx", "http://localhost:5173/@vite/client"])).toBe("");
    expect(loadedBundleName(["/assets/nodepools-Byoks1Wo.js", "/assets/index-DHpr11oR.css"])).toBe("");
  });
});

describe("isFrontendStale", () => {
  it("só afirma desatualizada quando os dois nomes existem e diferem", () => {
    expect(isFrontendStale("index-AAA.js", "index-BBB.js")).toBe(true);
    expect(isFrontendStale("index-AAA.js", "index-AAA.js")).toBe(false);
  });

  it("sem um dos lados não dá para afirmar (dev, servidor antigo): não é desatualizada", () => {
    expect(isFrontendStale("", "index-BBB.js")).toBe(false);
    expect(isFrontendStale("index-AAA.js", "")).toBe(false);
    expect(isFrontendStale("index-AAA.js", undefined)).toBe(false);
    expect(isFrontendStale("", undefined)).toBe(false);
  });
});

describe("buildLabel / describeBuild", () => {
  const b: ServerBuild = { version: "dev", commit_short: "4133705", dirty: false, built_at: "2026-11-10T14:00:00Z", frontend_bundle: "index-DHpr11oR.js" };

  it("mostra o commit, com * quando havia mudanças sem commit", () => {
    expect(buildLabel(b)).toBe("4133705");
    expect(buildLabel({ ...b, dirty: true })).toBe("4133705*");
  });

  it("sem commit usa a versão (ou dev), e sem build devolve vazio", () => {
    expect(buildLabel({ version: "1.3.49" })).toBe("1.3.49");
    expect(buildLabel({ version: "dev" })).toBe("dev");
    expect(buildLabel(undefined)).toBe("");
    expect(buildLabel(null)).toBe("");
  });

  it("o tooltip junta commit, binário e os dois bundles do frontend", () => {
    const text = describeBuild({ ...b, dirty: true }, "index-OLD.js");
    expect(text).toContain("Commit: 4133705 (com mudanças sem commit)");
    expect(text).toContain("Frontend do servidor: index-DHpr11oR.js");
    expect(text).toContain("Frontend desta página: index-OLD.js");
    expect(describeBuild(undefined)).toBe("Build do servidor indisponível");
  });
});
