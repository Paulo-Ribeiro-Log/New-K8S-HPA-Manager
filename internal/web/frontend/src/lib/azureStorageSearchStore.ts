import type { AzureAccountContents, AzureStorageHit, AzureStorageSearchResult } from "@/lib/api/types";

// Resultado da busca de storage por subscription que PERSISTE: o que foi achado não some ao mudar de
// aba, recarregar a página ou quando uma busca seguinte falha. Cada busca nova se MESCLA ao que já
// havia (nunca substitui), e o conteúdo de cada conta (containers/file shares) fica junto.

export interface StoredSearch {
  result: AzureStorageSearchResult;
  contents: Record<string, AzureAccountContents>; // chave: contentsKey(conta)
  savedAt: string;
}

const STORAGE_KEY = "azure-storage-search-v1";
const MAX_ENTRIES = 8; // buscas guardadas (as mais recentes)
const MAX_BYTES = 1_500_000; // acima disso não persiste no navegador (a memória continua valendo)

export const searchKey = (cluster: string | undefined, q: string) => `${cluster ?? "*"}|${q.trim().toLowerCase()}`;
export const hitKey = (h: Pick<AzureStorageHit, "subscription_id" | "type" | "account" | "name">) =>
  `${h.subscription_id}/${h.type}/${h.account ?? ""}/${h.name}`;
export const contentsKey = (h: Pick<AzureStorageHit, "subscription_id" | "name">) => `${h.subscription_id}/${h.name}`;

const uniq = <T,>(xs: T[]) => Array.from(new Set(xs));

// mergeSearchResults junta uma busca nova ao resultado anterior: acha-se mais, nunca menos. Um resultado
// já conhecido recebe os dados novos, mas não perde o que só o anterior trazia (ciclo de vida, containers).
export function mergeSearchResults(prev: AzureStorageSearchResult | null, next: AzureStorageSearchResult): AzureStorageSearchResult {
  if (!prev) return { ...next, warnings: next.warnings ?? [], results: next.results ?? [], subscriptions: next.subscriptions ?? [] };

  const subs = new Map(prev.subscriptions.map((s) => [s.id, s]));
  for (const s of next.subscriptions ?? []) {
    const old = subs.get(s.id);
    // a varredura de containers de uma subscription não relista discos: o número anterior vale
    subs.set(s.id, { ...(old ?? s), ...s, accounts_listed: s.accounts_listed || old?.accounts_listed || 0, disks_listed: s.disks_listed || old?.disks_listed || 0 });
  }

  const hits = new Map(prev.results.map((h) => [hitKey(h), h]));
  for (const h of next.results ?? []) {
    const old = hits.get(hitKey(h));
    hits.set(hitKey(h), old ? { ...old, ...h, lifecycle: h.lifecycle ?? old.lifecycle, containers: h.containers ?? old.containers } : h);
  }

  return {
    ...prev,
    ...next,
    deep: prev.deep || next.deep,
    all: prev.all || next.all,
    truncated: prev.truncated || next.truncated,
    subscriptions: Array.from(subs.values()),
    results: Array.from(hits.values()),
    warnings: uniq([...(prev.warnings ?? []), ...(next.warnings ?? [])]).slice(-40),
  };
}

// ── armazenamento: memória (vale entre abas desmontadas) + localStorage (vale entre recarregamentos) ──

const memory = new Map<string, StoredSearch>();

type Disk = { order: string[]; entries: Record<string, StoredSearch> };

function readDisk(): Disk {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const d = JSON.parse(raw) as Disk;
      if (d && Array.isArray(d.order) && d.entries) return d;
    }
  } catch { /* sem localStorage (janela privada) ou JSON ruim: segue só com a memória */ }
  return { order: [], entries: {} };
}

export function loadStored(key: string): StoredSearch | null {
  const m = memory.get(key);
  if (m) return m;
  const s = readDisk().entries[key];
  if (s) memory.set(key, s);
  return s ?? null;
}

export function saveStored(key: string, value: Omit<StoredSearch, "savedAt">): StoredSearch {
  const stored: StoredSearch = { ...value, savedAt: new Date().toISOString() };
  memory.set(key, stored);
  try {
    const d = readDisk();
    d.entries[key] = stored;
    d.order = [key, ...d.order.filter((k) => k !== key)];
    for (const old of d.order.splice(MAX_ENTRIES)) delete d.entries[old];
    const json = JSON.stringify(d);
    if (json.length <= MAX_BYTES) localStorage.setItem(STORAGE_KEY, json);
  } catch { /* cota cheia ou sem localStorage: a memória segura */ }
  return stored;
}

export function clearStored(key: string) {
  memory.delete(key);
  try {
    const d = readDisk();
    delete d.entries[key];
    d.order = d.order.filter((k) => k !== key);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(d));
  } catch { /* nada a limpar */ }
}
