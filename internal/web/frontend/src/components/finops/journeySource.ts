import type { JourneySource } from "./types";

// De onde veio a jornada de um recurso órfão (FinOps → Recursos órfãos).
export const JOURNEY_SOURCE_LABEL: Record<JourneySource, string> = {
  resource_tag: "tag do recurso",
  rg_tag: "tag do resource group",
  node_rg: "node RG (MC_) do cluster",
  none: "sem jornada",
};
