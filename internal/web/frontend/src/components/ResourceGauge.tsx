import React from "react";

interface ResourceGaugeProps {
  title: string;
  current: number;
  request: number;
  limit: number;
  unit: string;
  formatValue?: (value: number) => string;
}

const ResourceGauge: React.FC<ResourceGaugeProps> = ({
  title,
  current,
  request,
  limit,
  unit,
  formatValue,
}) => {
  // Calcular percentuais em relação ao limit
  const maxValue = Math.max(limit, request, current) * 1.1; // 10% de margem
  const requestPercent = (request / maxValue) * 100;
  const currentPercent = (current / maxValue) * 100;
  const limitPercent = (limit / maxValue) * 100;

  // Configurações do gauge
  const size = 100;
  const strokeWidth = 8;
  const center = size / 2;
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;

  // Ângulos para semi-círculo (180 graus = -90 a +90)
  const startAngle = -90;
  const endAngle = 90;
  const angleRange = endAngle - startAngle;

  // Calcular arcos
  const getArcPath = (percent: number, isBackground = false) => {
    const angle = startAngle + (angleRange * percent) / 100;
    const endAngleRad = (angle * Math.PI) / 180;
    const startAngleRad = (startAngle * Math.PI) / 180;

    const x1 = center + radius * Math.cos(startAngleRad);
    const y1 = center + radius * Math.sin(startAngleRad);
    const x2 = center + radius * Math.cos(endAngleRad);
    const y2 = center + radius * Math.sin(endAngleRad);

    const largeArcFlag = angle - startAngle > 180 ? 1 : 0;

    return `M ${x1} ${y1} A ${radius} ${radius} 0 ${largeArcFlag} 1 ${x2} ${y2}`;
  };

  // Cores por faixa
  const getColorForPercent = (percent: number) => {
    if (percent < 50) return "#10b981"; // green
    if (percent < 70) return "#fbbf24"; // yellow
    if (percent < 85) return "#f97316"; // orange
    return "#ef4444"; // red
  };

  const requestColor = "#3b82f6"; // blue
  const currentColor = getColorForPercent(currentPercent);
  const limitColor = "#64748b"; // slate

  // Formatar valores
  const format = formatValue || ((v: number) => v.toFixed(0));

  return (
    <div className="flex flex-col items-center">
      <div className="relative" style={{ width: size, height: size * 0.6 }}>
        <svg width={size} height={size * 0.6} className="transform">
          {/* Background track */}
          <path
            d={getArcPath(100)}
            fill="none"
            stroke="#e5e7eb"
            strokeWidth={strokeWidth}
            strokeLinecap="round"
          />

          {/* Limit arc (base) */}
          {limit > 0 && (
            <path
              d={getArcPath(limitPercent)}
              fill="none"
              stroke={limitColor}
              strokeWidth={strokeWidth}
              strokeLinecap="round"
              opacity={0.3}
            />
          )}

          {/* Request arc */}
          {request > 0 && (
            <path
              d={getArcPath(requestPercent)}
              fill="none"
              stroke={requestColor}
              strokeWidth={strokeWidth}
              strokeLinecap="round"
              opacity={0.5}
            />
          )}

          {/* Current arc (top layer) */}
          {current > 0 && (
            <path
              d={getArcPath(currentPercent)}
              fill="none"
              stroke={currentColor}
              strokeWidth={strokeWidth}
              strokeLinecap="round"
            />
          )}
        </svg>

        {/* Center value */}
        <div className="absolute inset-0 flex flex-col items-center justify-center" style={{ top: "10%" }}>
          <div className="text-lg font-bold">{format(current)}</div>
          <div className="text-[10px] text-muted-foreground">{unit}</div>
        </div>
      </div>

      {/* Title */}
      <div className="text-xs font-medium mt-0.5 text-center">{title}</div>

      {/* Legend - Only current value */}
      <div className="flex items-center justify-center gap-1 mt-1 text-[10px]">
        <div className="w-2 h-2 rounded-full" style={{ backgroundColor: currentColor }} />
        <span className="text-muted-foreground">Cur: {format(current)}</span>
      </div>
    </div>
  );
};

export default ResourceGauge;
