import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Activity } from "lucide-react";
import { DeploymentBehaviorChart } from "@/components/DeploymentBehaviorChart";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  deployment: string;
}

// DeploymentBehaviorModal — reexposição do gráfico de comportamento (já usado no quick-view de
// pod, aba "Comportamento") na aba Deployments, ao lado de "Análise Preditiva"/"Histórico de
// Análises" (ver DEPLOYMENT-BEHAVIOR-GRAPH-PLAN.md, Fase 3). Modal dedicado (não painel inline) —
// mesmo padrão de PredictionHistoryModal.tsx, altura FIXA (não max-h) pra cadeia de scroll interno
// funcionar (ver nota no CLAUDE.md sobre max-height vs height).
export function DeploymentBehaviorModal({ open, onOpenChange, cluster, namespace, deployment }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl h-[85vh] flex flex-col p-0">
        <DialogHeader className="flex-shrink-0 px-6 pt-6 pb-4 border-b border-border">
          <DialogTitle className="flex items-center gap-2">
            <Activity className="w-5 h-5 text-blue-500" />
            Comportamento — {deployment}
          </DialogTitle>
        </DialogHeader>
        <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
          <DeploymentBehaviorChart cluster={cluster} namespace={namespace} deployment={deployment} />
        </div>
      </DialogContent>
    </Dialog>
  );
}
