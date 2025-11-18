import { useMemo } from "react";
import type { HPA } from "@/lib/api/types";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

interface HPATableViewProps {
  hpas: HPA[];
  onSelectHPA: (hpa: HPA) => void;
}

export const HPATableView = ({ hpas, onSelectHPA }: HPATableViewProps) => {
  // Agrupar HPAs por namespace
  const hpasByNamespace = useMemo(() => {
    const grouped: Record<string, HPA[]> = {};
    
    hpas.forEach((hpa) => {
      if (!grouped[hpa.namespace]) {
        grouped[hpa.namespace] = [];
      }
      grouped[hpa.namespace].push(hpa);
    });
    
    // Ordenar namespaces alfabeticamente
    return Object.keys(grouped)
      .sort()
      .map((namespace) => ({
        namespace,
        hpas: grouped[namespace].sort((a, b) => a.name.localeCompare(b.name)),
      }));
  }, [hpas]);

  if (hpas.length === 0) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        Nenhum HPA encontrado neste cluster
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {hpasByNamespace.map(({ namespace, hpas: namespaceHPAs }) => (
        <div key={namespace} className="space-y-2">
          {/* Cabeçalho do Namespace */}
          <h3 className="text-lg font-bold text-primary border-b-2 border-primary/30 pb-1">
            {namespace}
          </h3>

          {/* Tabela de HPAs */}
          <div className="rounded-lg border border-border/60">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/50">
                  <TableHead className="font-semibold">Nome do HPA</TableHead>
                  <TableHead className="text-center font-semibold w-[150px]">Versão</TableHead>
                  <TableHead className="text-center font-semibold w-[120px]">Min Replicas</TableHead>
                  <TableHead className="text-center font-semibold w-[120px]">Max Replicas</TableHead>
                  <TableHead className="text-center font-semibold w-[120px]">Replicas</TableHead>
                  <TableHead className="text-center font-semibold w-[120px]">CPU Target (%)</TableHead>
                  <TableHead className="text-center font-semibold w-[140px]">Memory Target (%)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {namespaceHPAs.map((hpa) => (
                  <TableRow
                    key={`${hpa.cluster}-${hpa.namespace}-${hpa.name}`}
                    className="cursor-pointer hover:bg-primary/10 transition-colors"
                    onClick={() => onSelectHPA(hpa)}
                  >
                    <TableCell className="font-medium">{hpa.name}</TableCell>
                    <TableCell className="text-center">
                      {hpa.image_version ? (
                        <code className="text-xs bg-muted px-2 py-1 rounded">
                          {hpa.image_version}
                        </code>
                      ) : (
                        <span className="text-muted-foreground">-</span>
                      )}
                    </TableCell>
                    <TableCell className="text-center">{hpa.min_replicas ?? 0}</TableCell>
                    <TableCell className="text-center">{hpa.max_replicas ?? 1}</TableCell>
                    <TableCell className="text-center font-semibold">{hpa.current_replicas ?? 0}</TableCell>
                    <TableCell className="text-center">
                      {hpa.target_cpu !== null && hpa.target_cpu !== undefined ? `${hpa.target_cpu}%` : "-"}
                    </TableCell>
                    <TableCell className="text-center">
                      {hpa.target_memory !== null && hpa.target_memory !== undefined ? `${hpa.target_memory}%` : "-"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      ))}
    </div>
  );
};
