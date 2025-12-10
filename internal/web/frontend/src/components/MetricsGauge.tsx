import { Card } from "@/components/ui/card";
import { LucideIcon } from "lucide-react";

interface MetricsGaugeProps {
  icon: LucideIcon;
  label: string;
  value: number;              // % de uso (ex: 48.6%)
  capacityPercent: number;    // % de allocatable vs capacity (ex: 73%)
  unit?: string;
  warningThreshold?: number;
  dangerThreshold?: number;
}

export const MetricsGauge = ({
  icon: Icon,
  label,
  value,
  capacityPercent,
  unit = "%",
  warningThreshold = 70,
  dangerThreshold = 90
}: MetricsGaugeProps) => {
  const getStatusColor = () => {
    if (value >= dangerThreshold) return "text-destructive";
    if (value >= warningThreshold) return "text-warning";
    return "text-success";
  };

  const getProgressColor = () => {
    if (value >= dangerThreshold) return "hsl(var(--destructive))";
    if (value >= warningThreshold) return "hsl(var(--warning))";
    return "hsl(var(--success))";
  };

  // Calcular o overhead do sistema (100% - capacityPercent)
  const systemOverhead = 100 - capacityPercent;

  return (
    <Card className="p-4 bg-gradient-card border-border/50 flex flex-col items-center gap-3">
      <div className="flex items-center gap-2 w-full">
        <div className="p-1.5 bg-primary/10 rounded-lg">
          <Icon className="w-4 h-4 text-primary" />
        </div>
        <h3 className="text-xs font-semibold text-foreground">{label}</h3>
      </div>

      {/* Gauge de dois anéis */}
      <div className="relative w-36 h-36">
        <svg className="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
          {/* Anel externo - Capacity (100%) */}
          <circle
            cx="50"
            cy="50"
            r="42"
            fill="none"
            stroke="hsl(var(--muted))"
            strokeWidth="6"
            opacity="0.3"
          />
          {/* Anel externo - System Reserved (vermelho) */}
          <circle
            cx="50"
            cy="50"
            r="42"
            fill="none"
            stroke="hsl(var(--muted-foreground))"
            strokeWidth="6"
            strokeDasharray={`${(systemOverhead / 100) * 263.8} 263.8`}
            strokeLinecap="round"
            opacity="0.4"
            className="transition-all duration-500"
          />
          {/* Anel externo - Allocatable (azul) */}
          <circle
            cx="50"
            cy="50"
            r="42"
            fill="none"
            stroke="hsl(var(--primary))"
            strokeWidth="6"
            strokeDasharray={`${(capacityPercent / 100) * 263.8} 263.8`}
            strokeDashoffset={`${-(systemOverhead / 100) * 263.8}`}
            strokeLinecap="round"
            opacity="0.6"
            className="transition-all duration-500"
          />

          {/* Anel interno - Usage */}
          <circle
            cx="50"
            cy="50"
            r="32"
            fill="none"
            stroke="hsl(var(--muted))"
            strokeWidth="10"
          />
          <circle
            cx="50"
            cy="50"
            r="32"
            fill="none"
            stroke={getProgressColor()}
            strokeWidth="10"
            strokeDasharray={`${(value / 100) * 201.06} 201.06`}
            strokeLinecap="round"
            className="transition-all duration-500"
          />
        </svg>

        {/* Valor central */}
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className={`text-2xl font-bold ${getStatusColor()}`}>
            {value.toFixed(1)}
          </span>
          <span className="text-[10px] text-muted-foreground">{unit}</span>
          <span className="text-[9px] text-muted-foreground">
            usage
          </span>
        </div>
      </div>

      {/* Legenda */}
      <div className="w-full space-y-1 text-[10px]">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <div className="w-2 h-2 rounded-full bg-primary opacity-60"></div>
            <span className="text-muted-foreground">Allocatable</span>
          </div>
          <span className="font-mono font-semibold">{capacityPercent.toFixed(1)}%</span>
        </div>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <div className="w-2 h-2 rounded-full bg-muted-foreground opacity-40"></div>
            <span className="text-muted-foreground">System Reserved</span>
          </div>
          <span className="font-mono font-semibold">{systemOverhead.toFixed(1)}%</span>
        </div>
        <div className="flex items-center justify-between border-t border-border pt-1 mt-1">
          <div className="flex items-center gap-1.5">
            <div className={`w-2 h-2 rounded-full ${value >= dangerThreshold ? 'bg-destructive' : value >= warningThreshold ? 'bg-warning' : 'bg-success'}`}></div>
            <span className="text-muted-foreground">Current Usage</span>
          </div>
          <span className="font-mono font-semibold">{value.toFixed(1)}%</span>
        </div>
      </div>
    </Card>
  );
};
