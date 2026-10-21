// Filtro por nome ou ID de grupo: "vv_cloud_*_*" e "az_c_*" funcionam como curinga (* = qualquer
// trecho); sem curinga é "contém". Sem diferenciar maiúsculas. Casa se QUALQUER texto bater.
export function globMatcher(term: string): (...texts: string[]) => boolean {
  const t = term.trim().toLowerCase();
  if (!t) return () => true;
  if (!t.includes("*")) return (...texts) => texts.some((x) => x.toLowerCase().includes(t));
  const re = new RegExp(t.split("*").map((p) => p.replace(/[.+?^${}()|[\]\\]/g, "\\$&")).join(".*"), "i");
  return (...texts) => texts.some((x) => re.test(x));
}
