import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { LucideIcon } from "lucide-react";

interface MetricsGaugeProps {
  icon: LucideIcon;
  label: string;
  value: number;
  unit?: string;
  maxValue?: number;
  warningThreshold?: number;
  dangerThreshold?: number;
}

export const MetricsGauge = ({ 
  icon: Icon, 
  label, 
  value, 
  unit = "%",
  maxValue = 100,
  warningThreshold = 70,
  dangerThreshold = 90
}: MetricsGaugeProps) => {
  const percentage = (value / maxValue) * 100;
  
  const getStatusColor = () => {
    if (percentage >= dangerThreshold) return "text-destructive";
    if (percentage >= warningThreshold) return "text-warning";
    return "text-success";
  };

  const getProgressVariant = () => {
    if (percentage >= dangerThreshold) return "destructive";
    if (percentage >= warningThreshold) return "warning";
    return "success";
  };

  return (
    <Card className="p-6 bg-gradient-card border-border/50 flex flex-col items-center gap-4">
      <div className="flex items-center gap-2 w-full">
        <div className="p-2 bg-primary/10 rounded-lg">
          <Icon className="w-5 h-5 text-primary" />
        </div>
        <h3 className="text-sm font-semibold text-foreground">{label}</h3>
      </div>
      
      <div className="relative w-32 h-32">
        <svg className="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
          {/* Background circle */}
          <circle
            cx="50"
            cy="50"
            r="40"
            fill="none"
            stroke="hsl(var(--muted))"
            strokeWidth="8"
          />
          {/* Progress circle */}
          <circle
            cx="50"
            cy="50"
            r="40"
            fill="none"
            stroke={`hsl(var(--${getProgressVariant()}))`}
            strokeWidth="8"
            strokeDasharray={`${(percentage / 100) * 251.2} 251.2`}
            strokeLinecap="round"
            className="transition-all duration-500"
          />
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className={`text-2xl font-bold ${getStatusColor()}`}>
            {value.toFixed(1)}
          </span>
          <span className="text-xs text-muted-foreground">{unit}</span>
        </div>
      </div>

      <div className="w-full">
        <Progress 
          value={percentage} 
          className="h-2"
        />
      </div>
    </Card>
  );
};