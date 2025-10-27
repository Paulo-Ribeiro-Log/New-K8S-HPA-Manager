import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Copy, Download, Trash2, RefreshCw, FileText } from "lucide-react";
import { toast } from "sonner";

interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
  source: string;
}

interface LogViewerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export const LogViewer = ({ open, onOpenChange }: LogViewerProps) => {
  const [logs, setLogs] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);

  const fetchLogs = async () => {
    try {
      setLoading(true);
      const response = await fetch("/api/v1/logs", {
        headers: {
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
      });

      if (!response.ok) {
        throw new Error("Failed to fetch logs");
      }

      const data = await response.json();
      setLogs(data.logs || "");
    } catch (error) {
      console.error("Error fetching logs:", error);
      toast.error("Erro ao carregar logs");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      fetchLogs();
    }
  }, [open]);

  useEffect(() => {
    if (!autoRefresh || !open) return;

    const interval = setInterval(() => {
      fetchLogs();
    }, 3000); // Refresh a cada 3 segundos

    return () => clearInterval(interval);
  }, [autoRefresh, open]);

  const handleCopy = () => {
    navigator.clipboard.writeText(logs);
    toast.success("Logs copiados para a área de transferência!");
  };

  const handleExportCSV = () => {
    try {
      // Parsear logs e converter para CSV
      const lines = logs.split("\n").filter((line) => line.trim());
      const csvData = [
        ["Timestamp", "Level", "Source", "Message"], // Header
      ];

      lines.forEach((line) => {
        // Tentar parsear linha de log
        // Formato esperado: [2025/10/24 16:00:55] [INFO] [server] Message here
        const match = line.match(/\[(.*?)\]\s*\[(.*?)\]\s*\[(.*?)\]\s*(.*)/);
        if (match) {
          csvData.push([
            match[1], // timestamp
            match[2], // level
            match[3], // source
            match[4].replace(/"/g, '""'), // message (escape quotes)
          ]);
        } else {
          // Se não conseguir parsear, adicionar como mensagem simples
          csvData.push(["", "", "", line.replace(/"/g, '""')]);
        }
      });

      const csvContent = csvData
        .map((row) => row.map((cell) => `"${cell}"`).join(","))
        .join("\n");

      const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
      const link = document.createElement("a");
      const url = URL.createObjectURL(blob);
      link.setAttribute("href", url);
      link.setAttribute(
        "download",
        `k8s-hpa-manager-logs-${new Date().toISOString().split("T")[0]}.csv`
      );
      link.style.visibility = "hidden";
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);

      toast.success("Logs exportados para CSV!");
    } catch (error) {
      console.error("Error exporting CSV:", error);
      toast.error("Erro ao exportar CSV");
    }
  };

  const handleClear = async () => {
    if (!confirm("Tem certeza que deseja limpar todos os logs?")) {
      return;
    }

    try {
      const response = await fetch("/api/v1/logs", {
        method: "DELETE",
        headers: {
          Authorization: `Bearer ${localStorage.getItem("auth_token")}`,
        },
      });

      if (!response.ok) {
        throw new Error("Failed to clear logs");
      }

      setLogs("");
      toast.success("Logs limpos com sucesso!");
    } catch (error) {
      console.error("Error clearing logs:", error);
      toast.error("Erro ao limpar logs");
    }
  };

  const getLogStats = () => {
    const lines = logs.split("\n").filter((line) => line.trim());
    const errors = lines.filter((line) => line.includes("[ERROR]") || line.includes("Error:")).length;
    const warnings = lines.filter((line) => line.includes("[WARN]") || line.includes("Warning:")).length;
    const info = lines.filter((line) => line.includes("[INFO]")).length;

    return { total: lines.length, errors, warnings, info };
  };

  const stats = getLogStats();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl h-[85vh] flex flex-col">
        <DialogHeader>
          <div className="flex items-center justify-between">
            <div>
              <DialogTitle className="flex items-center gap-2">
                <FileText className="h-5 w-5" />
                System Logs
              </DialogTitle>
              <DialogDescription>
                Visualize logs da aplicação e do sistema
              </DialogDescription>
            </div>
            <div className="flex gap-2">
              <Badge variant="outline">
                Total: {stats.total}
              </Badge>
              {stats.errors > 0 && (
                <Badge variant="destructive">
                  Errors: {stats.errors}
                </Badge>
              )}
              {stats.warnings > 0 && (
                <Badge variant="secondary">
                  Warnings: {stats.warnings}
                </Badge>
              )}
              {stats.info > 0 && (
                <Badge variant="default">
                  Info: {stats.info}
                </Badge>
              )}
            </div>
          </div>
        </DialogHeader>

        <div className="flex gap-2 flex-wrap">
          <Button
            onClick={fetchLogs}
            disabled={loading}
            variant="outline"
            size="sm"
          >
            <RefreshCw className={`h-4 w-4 mr-2 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </Button>

          <Button
            onClick={() => setAutoRefresh(!autoRefresh)}
            variant={autoRefresh ? "default" : "outline"}
            size="sm"
          >
            <RefreshCw className={`h-4 w-4 mr-2 ${autoRefresh ? "animate-spin" : ""}`} />
            Auto-Refresh {autoRefresh ? "ON" : "OFF"}
          </Button>

          <Button onClick={handleCopy} variant="outline" size="sm">
            <Copy className="h-4 w-4 mr-2" />
            Copiar
          </Button>

          <Button onClick={handleExportCSV} variant="outline" size="sm">
            <Download className="h-4 w-4 mr-2" />
            Exportar CSV
          </Button>

          <Button
            onClick={handleClear}
            variant="destructive"
            size="sm"
          >
            <Trash2 className="h-4 w-4 mr-2" />
            Limpar
          </Button>
        </div>

        <div className="flex-1 overflow-hidden">
          <Textarea
            value={logs}
            readOnly
            className="h-full font-mono text-xs resize-none"
            placeholder={loading ? "Carregando logs..." : "Nenhum log disponível"}
          />
        </div>

        <div className="flex justify-between items-center text-xs text-muted-foreground">
          <div>
            {autoRefresh && (
              <span className="text-green-600">● Auto-refresh ativo (3s)</span>
            )}
          </div>
          <div>
            Última atualização: {new Date().toLocaleTimeString()}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
