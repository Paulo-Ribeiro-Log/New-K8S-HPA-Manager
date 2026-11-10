// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AzureStorageHit, AzureStorageSearchResult } from "@/lib/api/types";
import { contentsKey, hitKey, mergeSearchResults, searchKey } from "./azureStorageSearchStore";

const base = (o: Partial<AzureStorageSearchResult> = {}): AzureStorageSearchResult => ({
  query: "strretirafnc", deep: false, all: true, truncated: false, queried_at: "t1", subscriptions: [], results: [], warnings: [], ...o,
});

const account: AzureStorageHit = {
  type: "storage_account", name: "strretirafnc", resource_group: "rg-retira-fnc-prd", subscription_id: "S1", subscription_name: "PRD - ONLINE",
  clusters: [], aks_managed: false, containers: ["a", "b"],
  lifecycle: { has_policy: true, rules: [{ name: "r1", enabled: true, actions: [], deletes: false }] },
};

const first = base({ subscriptions: [{ id: "S1", name: "PRD - ONLINE", clusters: [], accounts_listed: 46, disks_listed: 565 }], results: [account] });

describe("mergeSearchResults: achar mais, nunca menos", () => {
  it("a varredura de containers acrescenta resultados sem perder ciclo de vida nem containers da conta", () => {
    const deepOne = base({
      all: false, deep: true, warnings: ["X: limitada"],
      subscriptions: [{ id: "S1", name: "PRD - ONLINE", clusters: [], accounts_listed: 46, disks_listed: 0 }],
      results: [
        { ...account, lifecycle: undefined, containers: undefined },
        { type: "container", name: "backup", account: "stdados", resource_group: "rg", subscription_id: "S1", clusters: [], aks_managed: false },
      ],
    });
    const m = mergeSearchResults(first, deepOne);
    expect(m.results).toHaveLength(2);
    const acc = m.results.find((h) => h.type === "storage_account")!;
    expect(acc.lifecycle?.rules).toHaveLength(1);
    expect(acc.containers).toEqual(["a", "b"]);
    expect(m.subscriptions[0].disks_listed).toBe(565); // a varredura não relista discos: o número anterior vale
    expect(m.all && m.deep).toBe(true); // os escopos acumulam
    expect(m.warnings).toContain("X: limitada");
  });

  it("uma busca que só traz erro NUNCA apaga o que já foi achado", () => {
    const falha = base({
      all: false, deep: true, results: [],
      subscriptions: [{ id: "S1", name: "PRD - ONLINE", clusters: [], accounts_listed: 0, disks_listed: 0 }],
      warnings: ["PRD - ONLINE: contas — context deadline exceeded"],
    });
    const m = mergeSearchResults(first, falha);
    expect(m.results).toHaveLength(1);
    expect(m.subscriptions[0].accounts_listed).toBe(46);
    expect(m.warnings.some((w) => w.includes("deadline"))).toBe(true);
  });

  it("aviso repetido não duplica, e a lista de avisos é limitada", () => {
    const falha = base({ warnings: ["mesmo aviso"] });
    const m1 = mergeSearchResults(first, falha);
    expect(mergeSearchResults(m1, falha).warnings).toHaveLength(m1.warnings.length);
    let m = first;
    for (let i = 0; i < 60; i++) m = mergeSearchResults(m, base({ warnings: [`aviso ${i}`] }));
    expect(m.warnings.length).toBeLessThanOrEqual(40);
    expect(m.warnings).toContain("aviso 59"); // fica o mais recente
  });

  it("sem resultado anterior, listas nulas do servidor viram listas vazias (a tela faz .map)", () => {
    const m = mergeSearchResults(null, base({ results: null as never, warnings: null as never, subscriptions: null as never }));
    expect(m.results).toEqual([]);
    expect(m.warnings).toEqual([]);
    expect(m.subscriptions).toEqual([]);
  });

  it("um resultado conhecido recebe os dados novos", () => {
    const m = mergeSearchResults(first, base({ results: [{ ...account, sku: "Standard_LRS", containers: ["a", "b", "c"] }] }));
    expect(m.results).toHaveLength(1);
    expect(m.results[0].sku).toBe("Standard_LRS");
    expect(m.results[0].containers).toHaveLength(3);
  });
});

describe("chaves", () => {
  it("searchKey ignora a caixa e separa por cluster", () => {
    expect(searchKey(undefined, "StrRetiraFnc")).toBe("*|strretirafnc");
    expect(searchKey("c1", " abc ")).toBe("c1|abc");
  });
  it("hitKey e contentsKey são estáveis", () => {
    expect(hitKey(account)).toBe("S1/storage_account//strretirafnc");
    expect(contentsKey(account)).toBe("S1/strretirafnc");
  });
});

// A persistência usa memória + localStorage: o módulo é recarregado para simular recarregar a página.
describe("persistência (memória + localStorage)", () => {
  beforeEach(() => {
    vi.unstubAllGlobals(); // desfaz o localStorage falso do teste anterior ANTES de limpar o real
    localStorage.clear();
    vi.resetModules();
  });
  const load = () => import("./azureStorageSearchStore");

  it("grava, e depois de 'recarregar a página' (módulo novo) lê do localStorage, com o conteúdo da conta", async () => {
    const a = await load();
    a.saveStored("*|x", { result: first, contents: { "S1/strretirafnc": { account: "strretirafnc" } as never } });
    expect(localStorage.getItem("azure-storage-search-v1")).toContain("strretirafnc");

    vi.resetModules();
    const b = await load(); // memória vazia, como numa página recarregada
    const s = b.loadStored("*|x");
    expect(s?.result.results).toHaveLength(1);
    expect(s?.contents["S1/strretirafnc"]).toBeTruthy();
    expect(s?.savedAt).toBeTruthy();
  });

  it("guarda só as 8 buscas mais recentes", async () => {
    const m = await load();
    m.saveStored("*|primeira", { result: first, contents: {} });
    for (let i = 0; i < 10; i++) m.saveStored(`*|busca${i}`, { result: first, contents: {} });
    const disk = JSON.parse(localStorage.getItem("azure-storage-search-v1")!);
    expect(disk.order).toHaveLength(8);
    expect(disk.entries["*|primeira"]).toBeUndefined();
    expect(disk.order[0]).toBe("*|busca9");
  });

  it("clearStored esquece da memória e do disco", async () => {
    const m = await load();
    m.saveStored("*|x", { result: first, contents: {} });
    m.clearStored("*|x");
    expect(m.loadStored("*|x")).toBeNull();
    expect(JSON.parse(localStorage.getItem("azure-storage-search-v1")!).entries["*|x"]).toBeUndefined();
  });

  it("sem localStorage utilizável (janela privada, cota cheia) segue só com a memória, sem lançar", async () => {
    vi.stubGlobal("localStorage", {
      getItem: () => { throw new Error("bloqueado"); },
      setItem: () => { throw new Error("cota"); },
      removeItem: () => { throw new Error("bloqueado"); },
    });
    const m = await load();
    expect(() => m.saveStored("*|y", { result: first, contents: {} })).not.toThrow();
    expect(m.loadStored("*|y")?.result.results).toHaveLength(1);
    expect(() => m.clearStored("*|y")).not.toThrow();
  });

  it("não persiste no navegador o que passa do limite de tamanho (a memória continua valendo)", async () => {
    const m = await load();
    const grande = base({ results: [{ ...account, containers: Array.from({ length: 200_000 }, (_, i) => `container-${i}`) }] });
    m.saveStored("*|grande", { result: grande, contents: {} });
    expect(localStorage.getItem("azure-storage-search-v1") ?? "").not.toContain("container-199999");
    expect(m.loadStored("*|grande")?.result.results[0].containers).toHaveLength(200_000);
  });
});
