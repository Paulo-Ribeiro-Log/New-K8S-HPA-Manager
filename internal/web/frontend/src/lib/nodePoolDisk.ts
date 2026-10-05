import type { NodePool } from "@/lib/api/types";

export interface OSDiskInfo {
  /** true/false quando o provider informou o tipo; null quando desconhecido */
  isEphemeral: boolean | null;
  /** rótulo curto para badge: "Ephemeral", "Managed", "pd-balanced", ... */
  label: string | null;
  sizeGB: number | null;
}

// Tipo de disco de OS do pool, vindo do provider (AKS osDiskType / GKE diskType). É a fonte
// confiável: os labels do node não distinguem disco efêmero de gerenciado no AKS.
export function getOSDiskInfo(pool: Pick<NodePool, "disk_type" | "disk_size_gb"> | null | undefined): OSDiskInfo {
  const raw = pool?.disk_type?.trim() || "";
  const lower = raw.toLowerCase();
  return {
    isEphemeral: lower === "ephemeral" ? true : raw ? false : null,
    label: raw || null,
    sizeGB: pool?.disk_size_gb && pool.disk_size_gb > 0 ? pool.disk_size_gb : null,
  };
}
