// Aba "Armazenamento e Órfãos" do FinOps (Fase 2 do FINOPS-REVISAO-PLAN.md): os volumes do cluster
// (PVCs com custo, do relatório) e os recursos órfãos na nuvem (discos desatachados e, no AKS, os
// demais órfãos dos RGs das jornadas). Um PVC sem pod é um disco desatachado: ele aparece nas duas
// seções, e a de órfãos marca o disco para não ser contado de novo.
import { HardDrive, Trash2 } from "lucide-react";
import type { FinOpsReport } from "./types";
import { StorageTab } from "./StorageTab";
import { UnattachedDisksTab } from "./UnattachedDisksTab";
import { SectionTitle } from "./charts";

export function StorageOrphansTab({ cluster, report, journeys, provider }: {
  cluster: string;
  report?: FinOpsReport;
  journeys: string[];
  provider?: string;
}) {
  const orphanPVCs = new Set((report?.pvcs ?? []).filter(p => p.is_orphaned).map(p => `${p.namespace}/${p.name}`));
  return (
    <div className="space-y-6">
      {report?.storage ? (
        <section>
          <SectionTitle icon={HardDrive} title="Volumes do cluster" sub="PVCs com custo mensal; os sem pod montando aparecem em destaque" />
          <StorageTab cluster={cluster} pvcs={report.pvcs ?? []} storage={report.storage} />
        </section>
      ) : (
        <p className="text-xs text-muted-foreground">Rode <strong>Analisar</strong> para ver o custo dos volumes (PVCs) do cluster.</p>
      )}
      <section>
        <SectionTitle icon={Trash2} title="Recursos órfãos na nuvem"
          sub={provider === "aks" ? "Discos e demais recursos sem uso nos resource groups das jornadas" : "Discos desatachados na conta do provedor"} />
        <UnattachedDisksTab cluster={cluster} journeys={journeys} azureScope={provider === "aks"} orphanPVCs={orphanPVCs} />
      </section>
    </div>
  );
}
