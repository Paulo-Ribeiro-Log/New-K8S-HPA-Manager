// ChartZoomModal - Modal com zoom para gráficos Recharts
import { useState, useRef } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ZoomIn, ZoomOut, Maximize2 } from "lucide-react";
import {
  ResponsiveContainer,
  AreaChart,
  LineChart,
  Area,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ReferenceLine,
  ReferenceArea,
} from "recharts";

interface ChartZoomModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  chartType: "cpu" | "memory" | "replicas";
  chartData: any[];
  chartConfig: {
    // Dados do snapshot
    cpuTarget?: number;
    memoryTarget?: number;
    cpuRequest?: string | null;
    cpuLimit?: string | null;
    memoryRequest?: string | null;
    memoryLimit?: string | null;
    replicasMin?: number;
    replicasMax?: number;
    // Função auxiliar
    absoluteToPercent?: (absoluteValue: string, limit: string, isCpu: boolean) => number | null;
  };
}

export function ChartZoomModal({
  open,
  onOpenChange,
  title,
  chartType,
  chartData,
  chartConfig,
}: ChartZoomModalProps) {
  const [zoomLevel, setZoomLevel] = useState(1);
  const containerRef = useRef<HTMLDivElement>(null);

  // Controles de zoom
  const handleZoomIn = () => {
    setZoomLevel(prev => Math.min(prev + 0.25, 3)); // Max 3x zoom
  };

  const handleZoomOut = () => {
    setZoomLevel(prev => Math.max(prev - 0.25, 1)); // Min 1x zoom
  };

  const handleResetZoom = () => {
    setZoomLevel(1);
  };

  // Calcular altura baseada no zoom
  const chartHeight = 500 * zoomLevel;

  // Renderizar gráfico de CPU
  const renderCPUChart = () => {
    const { cpuTarget, cpuRequest, cpuLimit, absoluteToPercent } = chartConfig;

    return (
      <ResponsiveContainer width="100%" height={chartHeight}>
        <AreaChart data={chartData}>
          <defs>
            <linearGradient id="cpuGradientZoom" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 12 }}
            className="text-xs"
          />
          <YAxis
            tick={{ fontSize: 12 }}
            label={{ value: "CPU (%)", angle: -90, position: "insideLeft", fontSize: 12 }}
            domain={[0, 150]}
          />
          <Tooltip />
          <Legend />

          {/* Reference Lines */}
          {cpuRequest && cpuLimit && absoluteToPercent && (
            <ReferenceLine
              y={absoluteToPercent(cpuRequest, cpuLimit, true) || 0}
              stroke="#f97316"
              strokeDasharray="3 3"
              label={{ value: `Request: ${cpuRequest}`, position: "right", fontSize: 11, fill: "#f97316" }}
            />
          )}
          {cpuLimit && (
            <ReferenceLine
              y={100}
              stroke="#ef4444"
              strokeDasharray="3 3"
              label={{ value: `Limit: ${cpuLimit}`, position: "right", fontSize: 11, fill: "#ef4444" }}
            />
          )}

          {/* Zonas de alerta */}
          <ReferenceArea
            y1={90}
            y2={100}
            fill="#ef4444"
            fillOpacity={0.1}
            label={{ value: "Zona Crítica", position: "insideTopRight", fontSize: 11 }}
          />
          <ReferenceArea
            y1={80}
            y2={90}
            fill="#f59e0b"
            fillOpacity={0.1}
            label={{ value: "Zona de Alerta", position: "insideTopRight", fontSize: 11 }}
          />

          {/* Target Line */}
          {cpuTarget && (
            <ReferenceLine
              y={cpuTarget}
              stroke="#10b981"
              strokeWidth={2}
              strokeDasharray="5 5"
              label={{ value: `Target: ${cpuTarget}%`, fill: '#10b981', position: 'insideTopRight' }}
            />
          )}

          {/* Linha D-1 */}
          <Line
            type="monotone"
            dataKey="cpuYesterday"
            name="CPU D-1"
            stroke="#9ca3af"
            strokeWidth={2}
            strokeDasharray="8 4"
            dot={false}
            unit="%"
            connectNulls
            isAnimationActive={false}
          />

          {/* Area principal */}
          <Area
            type="monotone"
            dataKey="cpuCurrent"
            name="CPU Atual"
            stroke="#3b82f6"
            strokeWidth={2}
            fill="url(#cpuGradientZoom)"
            unit="%"
          />
        </AreaChart>
      </ResponsiveContainer>
    );
  };

  // Renderizar gráfico de Memory
  const renderMemoryChart = () => {
    const { memoryTarget, memoryRequest, memoryLimit, absoluteToPercent } = chartConfig;

    return (
      <ResponsiveContainer width="100%" height={chartHeight}>
        <AreaChart data={chartData}>
          <defs>
            <linearGradient id="memoryGradientZoom" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#8b5cf6" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#8b5cf6" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 12 }}
            className="text-xs"
          />
          <YAxis
            tick={{ fontSize: 12 }}
            label={{ value: "Memória (%)", angle: -90, position: "insideLeft", fontSize: 12 }}
            domain={[0, 150]}
          />
          <Tooltip />
          <Legend />

          {/* Reference Lines */}
          {memoryRequest && memoryLimit && absoluteToPercent && (
            <ReferenceLine
              y={absoluteToPercent(memoryRequest, memoryLimit, false) || 0}
              stroke="#f97316"
              strokeDasharray="3 3"
              label={{ value: `Request: ${memoryRequest}`, position: "right", fontSize: 11, fill: "#f97316" }}
            />
          )}
          {memoryLimit && (
            <ReferenceLine
              y={100}
              stroke="#ef4444"
              strokeDasharray="3 3"
              label={{ value: `Limit: ${memoryLimit}`, position: "right", fontSize: 11, fill: "#ef4444" }}
            />
          )}

          {/* Zonas de alerta */}
          <ReferenceArea
            y1={90}
            y2={100}
            fill="#ef4444"
            fillOpacity={0.1}
            label={{ value: "Zona Crítica", position: "insideTopRight", fontSize: 11 }}
          />
          <ReferenceArea
            y1={80}
            y2={90}
            fill="#f59e0b"
            fillOpacity={0.1}
            label={{ value: "Zona de Alerta", position: "insideTopRight", fontSize: 11 }}
          />

          {/* Target Line */}
          {memoryTarget && (
            <ReferenceLine
              y={memoryTarget}
              stroke="#10b981"
              strokeWidth={2}
              strokeDasharray="5 5"
              label={{ value: `Target: ${memoryTarget}%`, fill: '#10b981', position: 'insideTopRight' }}
            />
          )}

          {/* Linha D-1 */}
          <Line
            type="monotone"
            dataKey="memoryYesterday"
            name="Memory D-1"
            stroke="#9ca3af"
            strokeWidth={2}
            strokeDasharray="8 4"
            dot={false}
            unit="%"
            connectNulls
            isAnimationActive={false}
          />

          {/* Area principal */}
          <Area
            type="monotone"
            dataKey="memoryCurrent"
            name="Memória Atual"
            stroke="#8b5cf6"
            strokeWidth={2}
            fill="url(#memoryGradientZoom)"
            unit="%"
          />
        </AreaChart>
      </ResponsiveContainer>
    );
  };

  // Renderizar gráfico de Replicas
  const renderReplicasChart = () => {
    const { replicasMin, replicasMax } = chartConfig;

    return (
      <ResponsiveContainer width="100%" height={chartHeight}>
        <LineChart data={chartData}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 12 }}
            className="text-xs"
          />
          <YAxis
            tick={{ fontSize: 12 }}
            label={{ value: "Réplicas", angle: -90, position: "insideLeft", fontSize: 12 }}
            allowDecimals={false}
          />
          <Tooltip />
          <Legend />

          {/* Min/Max Lines */}
          {replicasMin !== undefined && (
            <ReferenceLine
              y={replicasMin}
              stroke="#f97316"
              strokeDasharray="5 5"
              label={{ value: `Min: ${replicasMin}`, position: "right", fontSize: 11, fill: "#f97316" }}
            />
          )}
          {replicasMax !== undefined && (
            <ReferenceLine
              y={replicasMax}
              stroke="#ef4444"
              strokeDasharray="5 5"
              label={{ value: `Max: ${replicasMax}`, position: "right", fontSize: 11, fill: "#ef4444" }}
            />
          )}

          {/* Linha D-1 */}
          <Line
            type="stepAfter"
            dataKey="replicasYesterday"
            name="Réplicas D-1"
            stroke="#9ca3af"
            strokeWidth={2}
            strokeDasharray="8 4"
            dot={false}
            connectNulls
            isAnimationActive={false}
          />

          {/* Linhas principais */}
          <Line
            type="stepAfter"
            dataKey="replicasCurrent"
            name="Réplicas Atuais"
            stroke="#3b82f6"
            strokeWidth={2}
            dot={{ r: 4 }}
            isAnimationActive={false}
          />
          <Line
            type="stepAfter"
            dataKey="replicasDesired"
            name="Réplicas Desejadas"
            stroke="#10b981"
            strokeWidth={2}
            dot={{ r: 4 }}
            strokeDasharray="5 5"
          />
        </LineChart>
      </ResponsiveContainer>
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[90vw] max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <div className="flex items-center justify-between">
            <DialogTitle>{title}</DialogTitle>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleZoomOut}
                disabled={zoomLevel <= 1}
              >
                <ZoomOut className="h-4 w-4 mr-1" />
                Zoom Out
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleResetZoom}
                disabled={zoomLevel === 1}
              >
                <Maximize2 className="h-4 w-4 mr-1" />
                Reset
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleZoomIn}
                disabled={zoomLevel >= 3}
              >
                <ZoomIn className="h-4 w-4 mr-1" />
                Zoom In
              </Button>
              <span className="text-sm text-muted-foreground ml-2">
                {(zoomLevel * 100).toFixed(0)}%
              </span>
            </div>
          </div>
        </DialogHeader>

        <div
          ref={containerRef}
          className="flex-1 overflow-auto border rounded-lg p-4 bg-card"
          style={{
            maxHeight: "calc(90vh - 120px)",
          }}
        >
          {chartType === "cpu" && renderCPUChart()}
          {chartType === "memory" && renderMemoryChart()}
          {chartType === "replicas" && renderReplicasChart()}
        </div>

        <div className="flex-shrink-0 text-xs text-muted-foreground mt-2">
          Dica: Use os botões acima para ajustar o zoom. Você também pode rolar para ver mais detalhes.
        </div>
      </DialogContent>
    </Dialog>
  );
}
