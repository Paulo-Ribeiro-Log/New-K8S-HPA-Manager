import React from "react";

interface ResourceGaugeProps {
  title: string;
  current: number;
  request: number;
  limit: number;
  unit: string;
  formatValue?: (value: number) => string;
  /** Valor sugerido (rightsizing) — desenha uma marca fina no ângulo correspondente, além dos 3
   *  arcos já existentes. Opcional e aditivo: omitir mantém o gauge idêntico ao comportamento
   *  anterior a esta prop (usado por FinOps → Rightsizing, ver RightsizingTab.tsx). */
  recommended?: number;
}

const ResourceGauge: React.FC<ResourceGaugeProps> = ({
  title,
  current,
  request,
  limit,
  unit,
  formatValue,
  recommended,
}) => {
  // Calcular percentuais em relação ao limit
  const maxValue = Math.max(limit, request, current, recommended ?? 0) * 1.1; // 10% de margem
  const requestPercent = (request / maxValue) * 100;
  const currentPercent = (current / maxValue) * 100;
  const limitPercent = (limit / maxValue) * 100;
  const recommendedPercent = recommended !== undefined ? (recommended / maxValue) * 100 : undefined;

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
  const recommendedColor = "#a855f7"; // roxo — distinto de request(azul)/limit(cinza)/current(faixa de cor)

  // Marca radial fina (não um arco) no ângulo do valor recomendado — cruza a faixa dos 3 arcos
  // existentes, servindo de "aqui" sem competir visualmente com eles.
  const getTickLine = (percent: number) => {
    const angle = startAngle + (angleRange * percent) / 100;
    const angleRad = (angle * Math.PI) / 180;
    const inner = radius - strokeWidth / 2 - 3;
    const outer = radius + strokeWidth / 2 + 3;
    return {
      x1: center + inner * Math.cos(angleRad),
      y1: center + inner * Math.sin(angleRad),
      x2: center + outer * Math.cos(angleRad),
      y2: center + outer * Math.sin(angleRad),
    };
  };

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

          {/* Marca do valor recomendado (rightsizing) — sempre por cima, nunca some atrás dos arcos */}
          {recommendedPercent !== undefined && recommendedPercent > 0 && (
            <line
              {...getTickLine(recommendedPercent)}
              stroke={recommendedColor}
              strokeWidth={2}
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

      {/* Legend - valor atual (+ recomendado, quando a prop é passada) */}
      <div className="flex items-center justify-center gap-2 mt-1 text-[10px]">
        <div className="flex items-center gap-1">
          <div className="w-2 h-2 rounded-full" style={{ backgroundColor: currentColor }} />
          <span className="text-muted-foreground">Cur: {format(current)}</span>
        </div>
        {recommended !== undefined && recommended > 0 && (
          <div className="flex items-center gap-1">
            <div className="w-2 h-0.5 rounded-full" style={{ backgroundColor: recommendedColor }} />
            <span className="text-muted-foreground">Rec: {format(recommended)}</span>
          </div>
        )}
      </div>
    </div>
  );
};

export default ResourceGauge;
