import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

interface CronJobListItemProps {
  name: string;
  namespace: string;
  schedule: string;
  suspend: boolean;
  activeJobs: number;
  successfulJobs: number;
  failedJobs: number;
  isSelected: boolean;
  onClick: () => void;
}

export const CronJobListItem = ({
  name,
  namespace,
  schedule,
  suspend,
  activeJobs,
  successfulJobs,
  failedJobs,
  isSelected,
  onClick,
}: CronJobListItemProps) => {
  const getStatusBadge = () => {
    if (suspend) {
      return <Badge variant="secondary" className="bg-red-100 text-red-800 text-xs">🔴 Suspenso</Badge>;
    }
    if (activeJobs > 0) {
      return <Badge variant="default" className="bg-blue-100 text-blue-800 text-xs">🔵 Executando</Badge>;
    }
    if (failedJobs > 0) {
      return <Badge variant="destructive" className="bg-yellow-100 text-yellow-800 text-xs">🟡 Falhou</Badge>;
    }
    return <Badge variant="default" className="bg-green-100 text-green-800 text-xs">🟢 Ativo</Badge>;
  };

  return (
    <Card
      className={`
        p-3 mb-2 cursor-pointer transition-all duration-200
        hover:shadow-md hover:-translate-y-0.5
        ${
          isSelected
            ? "border-2 border-primary bg-accent shadow-md"
            : "border border-border/50 hover:border-primary/50"
        }
      `}
      onClick={onClick}
    >
      <div className="flex items-start justify-between mb-1">
        <div className="flex-1">
          <h4 className="font-semibold text-sm text-foreground">{name}</h4>
          <p className="text-xs text-muted-foreground">{namespace}</p>
        </div>
        {getStatusBadge()}
      </div>
      <div className="flex items-center gap-3 text-xs text-muted-foreground mt-2">
        <span className="font-mono">{schedule}</span>
      </div>
      <div className="flex items-center gap-3 text-xs text-muted-foreground mt-1">
        <span>Ativos: <span className="font-medium text-foreground">{activeJobs}</span></span>
        <span className="text-green-600">Sucessos: <span className="font-medium">{successfulJobs}</span></span>
        <span className="text-red-600">Falhas: <span className="font-medium">{failedJobs}</span></span>
      </div>
    </Card>
  );
};
