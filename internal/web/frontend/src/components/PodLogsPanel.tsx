import { useState, useEffect, useRef, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { ChevronLeft, Copy, RefreshCw, Loader2 } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import type { PodSummary } from "@/lib/api/types";
import { toast } from "sonner";

interface PodLogsPanelProps {
  cluster: string;
  pod: PodSummary;
  onBack: () => void;
  backLabel: string;
}

function highlightLogLine(line: string): string {
  const l = line.toUpperCase();
  if (/\b(ERROR|FATAL|EXCEPTION|PANIC)\b/.test(l)) {
    return "text-red-400";
  }
  if (/\b(WARN|WARNING)\b/.test(l)) {
    return "text-yellow-400";
  }
  if (/\b(INFO)\b/.test(l) && !/\b(INFORMATION)\b/.test(l)) {
    return "text-blue-400";
  }
  if (/\b(DEBUG|TRACE)\b/.test(l)) {
    return "text-purple-400";
  }
  if (/ [45]\d{2} /.test(line)) {
    return "text-orange-400";
  }
  if (/ [23]\d{2} /.test(line)) {
    return "text-green-300";
  }
  return "text-gray-300";
}

export const PodLogsPanel = ({ cluster, pod, onBack, backLabel }: PodLogsPanelProps) => {
  const containers = pod.containers?.map((c) => c.name) ?? [];
  const [selectedContainer, setSelectedContainer] = useState(containers[0] ?? "");
  const [tailLines, setTailLines] = useState(500);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [logs, setLogs] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchLogs = useCallback(async () => {
    if (!selectedContainer) return;
    setLoading(true);
    try {
      const result = await apiClient.getPodLogs(cluster, pod.namespace, pod.name, selectedContainer, tailLines);
      setLogs(result.logs ? result.logs.split("\n") : []);
    } catch {
      // silently ignore
    } finally {
      setLoading(false);
    }
  }, [cluster, pod.namespace, pod.name, selectedContainer, tailLines]);

  // Initial fetch
  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  // Auto-refresh
  useEffect(() => {
    if (intervalRef.current) clearInterval(intervalRef.current);
    if (autoRefresh) {
      intervalRef.current = setInterval(fetchLogs, 3000);
    }
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, [autoRefresh, fetchLogs]);

  // Auto-scroll to bottom when autoRefresh is on
  useEffect(() => {
    if (autoRefresh && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, autoRefresh]);

  const handleCopy = () => {
    navigator.clipboard.writeText(logs.join("\n"))
      .then(() => toast.success("Logs copiados"))
      .catch(() => toast.error("Falha ao copiar"));
  };

  return (
    <div className="flex flex-col h-full gap-2">
      {/* Header row 1: back + container + tail */}
      <div className="flex items-center gap-2 flex-shrink-0">
        <Button variant="ghost" size="sm" onClick={onBack} className="text-xs text-muted-foreground h-7 px-2">
          <ChevronLeft className="w-3 h-3 mr-1" />
          {backLabel}
        </Button>
        <div className="flex-1" />
        {containers.length > 1 && (
          <Select value={selectedContainer} onValueChange={setSelectedContainer}>
            <SelectTrigger className="h-7 text-xs w-[150px]">
              <SelectValue placeholder="Container" />
            </SelectTrigger>
            <SelectContent>
              {containers.map((ct) => (
                <SelectItem key={ct} value={ct}>{ct}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <Select value={String(tailLines)} onValueChange={(v) => setTailLines(Number(v))}>
          <SelectTrigger className="h-7 text-xs w-[90px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {[100, 500, 1000, 5000].map((n) => (
              <SelectItem key={n} value={String(n)}>{n} linhas</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Header row 2: auto-refresh + copy */}
      <div className="flex items-center gap-3 flex-shrink-0">
        <div className="flex items-center gap-2">
          <Switch id="auto-refresh-logs" checked={autoRefresh} onCheckedChange={setAutoRefresh} />
          <Label htmlFor="auto-refresh-logs" className="text-xs cursor-pointer">
            Auto-refresh
            {autoRefresh && loading && <Loader2 className="w-3 h-3 ml-1 inline animate-spin" />}
          </Label>
        </div>
        <div className="flex-1" />
        <Button variant="outline" size="sm" className="h-7 text-xs" onClick={fetchLogs} title="Atualizar logs">
          <RefreshCw className="w-3 h-3" />
        </Button>
        <Button variant="outline" size="sm" className="h-7 text-xs" onClick={handleCopy}>
          <Copy className="w-3 h-3 mr-1" />
          Copiar
        </Button>
      </div>

      {/* Log area */}
      <div
        ref={scrollRef}
        className="flex-1 min-h-0 bg-black rounded-md overflow-auto font-mono text-xs p-2"
      >
        {logs.length === 0 && !loading && (
          <span className="text-gray-500">Sem logs disponíveis</span>
        )}
        {logs.map((line, i) => (
          <div key={i} className={`leading-5 whitespace-pre-wrap break-all ${highlightLogLine(line)}`}>
            {line || "\u00A0"}
          </div>
        ))}
      </div>
    </div>
  );
};
