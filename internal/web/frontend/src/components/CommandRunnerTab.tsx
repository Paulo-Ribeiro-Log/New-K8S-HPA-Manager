import { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ProtectedAction } from "@/components/rbac";
import {
  Terminal,
  Play,
  Square,
  Trash2,
  Copy,
  Sparkles,
  Loader2,
  Search,
  X,
  Server,
  CheckCircle,
  XCircle,
  Clock,
} from "lucide-react";
import { toast } from "sonner";
import Editor from "@monaco-editor/react";

import { useClusters, useNamespaces } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import type { CommandTarget, CommandType, CommandRunnerSSEEvent } from "@/lib/api/types";
import type { Cluster } from "@/lib/api/types";

interface CommandRunnerTabProps {
  selectedCluster?: string;
}

// Linha de output por cluster
interface OutputLine {
  text: string;
  isError: boolean;
  timestamp: string;
}

type ClusterStatus = "idle" | "running" | "done" | "error";

const CMD_TYPE_LABELS: Record<CommandType, string> = {
  kubectl: "kubectl",
  sh: "Shell (sh)",
  bash: "Bash",
  python: "Python",
  go: "Go",
};

const EXAMPLE_COMMANDS: Record<CommandType, string> = {
  kubectl: "kubectl get pods -n {{namespace}} --context={{cluster}}",
  sh: "echo \"Cluster: {{cluster}}, NS: {{namespace}}\"",
  bash: "for i in 1 2 3; do echo \"$i - {{cluster}}\"; done",
  python: "print(f'Cluster: {{cluster}}, Namespace: {{namespace}}')",
  go: `package main
import "fmt"
func main() {
    fmt.Println("Cluster: {{cluster}}")
}`,
};

export function CommandRunnerTab({ selectedCluster }: CommandRunnerTabProps) {
  const { clusters: allClusters } = useClusters();

  // ── seleção de targets ───────────────────────────────────────────────────
  const [selectedClusters, setSelectedClusters] = useState<string[]>(
    selectedCluster ? [selectedCluster] : []
  );
  const [clusterSearch, setClusterSearch] = useState("");
  const [targetNamespace, setTargetNamespace] = useState("default");

  // namespace do primeiro cluster selecionado (para sugestão)
  const { namespaces: availableNamespaces } = useNamespaces(selectedClusters[0] || "");

  // ── comando ──────────────────────────────────────────────────────────────
  const [cmdType, setCmdType] = useState<CommandType>("kubectl");
  const [command, setCommand] = useState(EXAMPLE_COMMANDS.kubectl);
  const [timeoutSec, setTimeoutSec] = useState(300);

  // ── execução ─────────────────────────────────────────────────────────────
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [outputs, setOutputs] = useState<Record<string, OutputLine[]>>({});
  const [clusterStatus, setClusterStatus] = useState<Record<string, ClusterStatus>>({});
  const [activeOutputTab, setActiveOutputTab] = useState<string>("");
  const esRef = useRef<EventSource | null>(null);
  const outputEndRef = useRef<Record<string, HTMLDivElement | null>>({});

  // ── AI ───────────────────────────────────────────────────────────────────
  const [aiPrompt, setAiPrompt] = useState("");
  const [isGenerating, setIsGenerating] = useState(false);
  const [aiEmail, setAiEmail] = useState(() => localStorage.getItem("ai_email") || "");
  const [showAiPanel, setShowAiPanel] = useState(false);

  // ── clusters agrupados ───────────────────────────────────────────────────
  const clusterNames = useMemo(() => {
    if (!allClusters) return [] as string[];
    return allClusters.map((c: Cluster) => c.context);
  }, [allClusters]);

  const clusterGroups = useMemo(() => {
    const groups = { prd: [] as string[], hlg: [] as string[], dev: [] as string[], other: [] as string[] };
    clusterNames.forEach((name: string) => {
      const lower = name.toLowerCase();
      if (lower.includes("-prd") || lower.includes("-prod")) groups.prd.push(name);
      else if (lower.includes("-hlg") || lower.includes("-uat") || lower.includes("-sit")) groups.hlg.push(name);
      else if (lower.includes("-dev")) groups.dev.push(name);
      else groups.other.push(name);
    });
    return groups;
  }, [clusterNames]);

  const filteredClusterNames = useMemo(() => {
    if (!clusterSearch) return clusterNames;
    const q = clusterSearch.toLowerCase();
    return clusterNames.filter((n: string) => n.toLowerCase().includes(q));
  }, [clusterNames, clusterSearch]);

  // ── efeito: auto-scroll por aba ──────────────────────────────────────────
  useEffect(() => {
    const el = outputEndRef.current[activeOutputTab];
    if (el) el.scrollIntoView({ behavior: "smooth" });
  }, [outputs, activeOutputTab]);

  // ── conectar SSE quando sessionId mudar ──────────────────────────────────
  useEffect(() => {
    if (!sessionId) return;

    const url = apiClient.getCommandRunnerStreamURL(sessionId);
    const es = new EventSource(url);
    esRef.current = es;

    es.onmessage = (e) => {
      try {
        const event: CommandRunnerSSEEvent = JSON.parse(e.data);
        handleSSEEvent(event);
      } catch {
        // ignore parse errors
      }
    };

    es.onerror = () => {
      es.close();
      setIsRunning(false);
    };

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [sessionId]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleSSEEvent = useCallback((event: CommandRunnerSSEEvent) => {
    const cluster = event.cluster || "__global__";

    if (event.type === "output" || event.type === "output_error") {
      setOutputs((prev) => ({
        ...prev,
        [cluster]: [
          ...(prev[cluster] || []),
          {
            text: event.message,
            isError: event.type === "output_error",
            timestamp: event.timestamp,
          },
        ],
      }));
      setActiveOutputTab((prev) => prev || cluster);
    }

    if (event.type === "cluster_done") {
      setClusterStatus((prev) => ({
        ...prev,
        [cluster]: prev[cluster] === "error" ? "error" : "done",
      }));
    }

    if (event.type === "output_error") {
      setClusterStatus((prev) => ({ ...prev, [cluster]: "error" }));
    }

    if (event.type === "complete" || event.type === "error") {
      setIsRunning(false);
      esRef.current?.close();
      if (event.type === "error") {
        toast.error("Execução finalizada com erros");
      } else {
        toast.success(event.message);
      }
    }
  }, []);

  // ── executar ─────────────────────────────────────────────────────────────
  const handleExecute = async () => {
    if (selectedClusters.length === 0) {
      toast.error("Selecione pelo menos um cluster");
      return;
    }
    if (!command.trim()) {
      toast.error("Escreva um comando");
      return;
    }

    const targets: CommandTarget[] = selectedClusters.map((cluster) => ({
      cluster,
      namespace: targetNamespace || "default",
    }));

    // Inicializar estado
    const initialStatus: Record<string, ClusterStatus> = {};
    const initialOutputs: Record<string, OutputLine[]> = {};
    targets.forEach((t) => {
      initialStatus[t.cluster] = "running";
      initialOutputs[t.cluster] = [];
    });
    setClusterStatus(initialStatus);
    setOutputs(initialOutputs);
    setActiveOutputTab(targets[0]?.cluster || "");
    setIsRunning(true);

    try {
      const { session_id } = await apiClient.executeCommand({
        targets,
        command,
        type: cmdType,
        timeout_sec: timeoutSec,
      });
      setSessionId(session_id);
    } catch (err) {
      setIsRunning(false);
      toast.error("Erro ao iniciar execução", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  const handleStop = () => {
    esRef.current?.close();
    setIsRunning(false);
    toast.info("Streaming interrompido (processos em andamento no servidor podem continuar)");
  };

  const handleClearOutputs = () => {
    setOutputs({});
    setClusterStatus({});
    setSessionId(null);
  };

  // ── AI generate ──────────────────────────────────────────────────────────
  const handleGenerateCommand = async () => {
    if (!aiPrompt.trim()) {
      toast.error("Descreva o que deseja fazer");
      return;
    }
    if (!aiEmail.trim()) {
      toast.error("Informe seu email de AI (configurado em AI Settings)");
      return;
    }

    setIsGenerating(true);
    try {
      const { command: generated, type } = await apiClient.generateCommand({
        prompt: aiPrompt,
        cluster: selectedClusters[0] || "",
        namespace: targetNamespace || "default",
        ai_email: aiEmail,
        cmd_type: cmdType,
      });
      setCommand(generated);
      setCmdType(type);
      setShowAiPanel(false);
      toast.success("Comando gerado — revise antes de executar");
    } catch (err) {
      toast.error("Erro ao gerar comando", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsGenerating(false);
    }
  };

  const copyOutput = (cluster: string) => {
    const lines = (outputs[cluster] || []).map((l) => l.text).join("\n");
    navigator.clipboard.writeText(lines);
    toast.success("Output copiado");
  };

  // ── helpers de UI ─────────────────────────────────────────────────────────
  const toggleCluster = (cluster: string) =>
    setSelectedClusters((prev) =>
      prev.includes(cluster) ? prev.filter((c) => c !== cluster) : [...prev, cluster]
    );

  const selectGroup = (group: "prd" | "hlg" | "dev" | "all") => {
    if (group === "all") setSelectedClusters(clusterNames);
    else setSelectedClusters(clusterGroups[group]);
  };

  const statusIcon = (status: ClusterStatus) => {
    switch (status) {
      case "running": return <Loader2 className="h-3 w-3 animate-spin text-blue-400" />;
      case "done": return <CheckCircle className="h-3 w-3 text-green-400" />;
      case "error": return <XCircle className="h-3 w-3 text-red-400" />;
      default: return <Clock className="h-3 w-3 text-muted-foreground" />;
    }
  };

  const totalOutputClusters = Object.keys(outputs).length;

  // ── painel esquerdo: seleção de targets ───────────────────────────────────
  const leftContent = (
    <div className="space-y-4">
      {/* Clusters */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Clusters</Label>
        <div className="flex flex-wrap gap-1 mb-2">
          <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectGroup("all")}>
            Todos
          </Button>
          {clusterGroups.prd.length > 0 && (
            <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectGroup("prd")}>
              Produção
            </Button>
          )}
          {clusterGroups.hlg.length > 0 && (
            <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectGroup("hlg")}>
              Homolog.
            </Button>
          )}
          {clusterGroups.dev.length > 0 && (
            <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectGroup("dev")}>
              Dev
            </Button>
          )}
          {selectedClusters.length > 0 && (
            <Button
              size="sm"
              variant="ghost"
              className="text-xs h-6 text-red-400 hover:text-red-300"
              onClick={() => setSelectedClusters([])}
            >
              <X className="h-3 w-3 mr-1" />
              Limpar ({selectedClusters.length})
            </Button>
          )}
        </div>
        <div className="relative mb-2">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          <Input
            placeholder="Filtrar clusters..."
            value={clusterSearch}
            onChange={(e) => setClusterSearch(e.target.value)}
            className="pl-7 h-7 text-xs"
          />
          {clusterSearch && (
            <button
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => setClusterSearch("")}
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <ScrollArea className="h-[160px]">
          <div className="space-y-1">
            {filteredClusterNames.map((cluster: string) => (
              <div key={cluster} className="flex items-center gap-2">
                <Checkbox
                  id={`cr-cluster-${cluster}`}
                  checked={selectedClusters.includes(cluster)}
                  onCheckedChange={() => toggleCluster(cluster)}
                />
                <Label htmlFor={`cr-cluster-${cluster}`} className="text-xs cursor-pointer truncate flex items-center gap-1">
                  <Server className="h-3 w-3 text-blue-400 flex-shrink-0" />
                  {cluster}
                </Label>
              </div>
            ))}
            {filteredClusterNames.length === 0 && (
              <p className="text-xs text-muted-foreground text-center py-2">Nenhum cluster</p>
            )}
          </div>
        </ScrollArea>
      </div>

      {/* Namespace */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Namespace</Label>
        {availableNamespaces && availableNamespaces.length > 0 ? (
          <Select value={targetNamespace} onValueChange={setTargetNamespace}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue placeholder="Namespace..." />
            </SelectTrigger>
            <SelectContent>
              {availableNamespaces.map((ns) => (
                <SelectItem key={ns.name} value={ns.name}>{ns.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Input
            value={targetNamespace}
            onChange={(e) => setTargetNamespace(e.target.value)}
            placeholder="default"
            className="h-8 text-sm"
          />
        )}
        <p className="text-xs text-muted-foreground mt-1">Use {`{{namespace}}`} no comando</p>
      </div>

      {/* Tipo de comando */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Linguagem</Label>
        <Select value={cmdType} onValueChange={(v) => {
          const t = v as CommandType;
          setCmdType(t);
          if (!command || command === EXAMPLE_COMMANDS[cmdType]) {
            setCommand(EXAMPLE_COMMANDS[t]);
          }
        }}>
          <SelectTrigger className="h-8 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(Object.keys(CMD_TYPE_LABELS) as CommandType[]).map((t) => (
              <SelectItem key={t} value={t}>{CMD_TYPE_LABELS[t]}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Timeout */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Timeout (segundos)</Label>
        <Input
          type="number"
          min={10}
          max={1800}
          value={timeoutSec}
          onChange={(e) => setTimeoutSec(Number(e.target.value))}
          className="h-8 text-sm"
        />
      </div>

      {/* Botão executar */}
      <ProtectedAction>
        <Button
          className="w-full"
          onClick={isRunning ? handleStop : handleExecute}
          disabled={selectedClusters.length === 0}
          variant={isRunning ? "destructive" : "default"}
        >
          {isRunning ? (
            <><Square className="h-4 w-4 mr-2" />Interromper</>
          ) : (
            <><Play className="h-4 w-4 mr-2" />Executar em {selectedClusters.length} cluster(s)</>
          )}
        </Button>
      </ProtectedAction>

      {/* Resumo de execução */}
      {Object.keys(clusterStatus).length > 0 && (
        <div className="space-y-1 pt-2 border-t border-border/50">
          <p className="text-xs text-muted-foreground">Status por cluster</p>
          <ScrollArea className="max-h-[120px]">
            {Object.entries(clusterStatus).map(([cluster, status]) => (
              <div key={cluster} className="flex items-center gap-2 py-0.5">
                {statusIcon(status)}
                <span className="text-xs truncate">{cluster}</span>
              </div>
            ))}
          </ScrollArea>
          <Button
            variant="ghost"
            size="sm"
            className="w-full text-xs h-6 text-muted-foreground"
            onClick={handleClearOutputs}
          >
            <Trash2 className="h-3 w-3 mr-1" />
            Limpar outputs
          </Button>
        </div>
      )}
    </div>
  );

  // ── painel direito: editor + AI colapsável + output por cluster ───────────
  const rightContent = (
    <div className="flex flex-col gap-3 h-full">
      {/* Editor de comando */}
      <div className="flex-shrink-0">
        <div className="flex items-center justify-between mb-1">
          <Label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
            Comando ({CMD_TYPE_LABELS[cmdType]})
          </Label>
          <div className="flex gap-1">
            <Button
              size="sm"
              variant={showAiPanel ? "secondary" : "ghost"}
              className="h-6 text-xs"
              onClick={() => setShowAiPanel((v) => !v)}
            >
              <Sparkles className="h-3 w-3 mr-1" />
              AI
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-6 text-xs"
              onClick={() => {
                navigator.clipboard.writeText(command);
                toast.success("Copiado");
              }}
            >
              <Copy className="h-3 w-3 mr-1" />
              Copiar
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-6 text-xs"
              onClick={() => setCommand(EXAMPLE_COMMANDS[cmdType])}
            >
              Exemplo
            </Button>
          </div>
        </div>
        <div className="border border-border/50 rounded overflow-hidden" style={{ height: "180px" }}>
          <Editor
            height="180px"
            language={cmdType === "python" ? "python" : cmdType === "go" ? "go" : "shell"}
            value={command}
            onChange={(v) => setCommand(v || "")}
            theme="vs-dark"
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              lineNumbers: "on",
              wordWrap: "on",
              scrollBeyondLastLine: false,
              padding: { top: 8, bottom: 8 },
            }}
          />
        </div>
        <p className="text-xs text-muted-foreground mt-1">
          Placeholders: <code className="bg-muted px-1 rounded">{"{{cluster}}"}</code> e <code className="bg-muted px-1 rounded">{"{{namespace}}"}</code> serão substituídos por cluster.
        </p>
      </div>

      {/* AI colapsável */}
      {showAiPanel && (
        <div className="flex-shrink-0 border border-border/50 rounded p-3 space-y-2 bg-muted/30">
          <div className="flex items-center justify-between mb-1">
            <span className="text-xs font-medium flex items-center gap-1">
              <Sparkles className="h-3 w-3 text-purple-400" />
              Gerar comando com AI ({CMD_TYPE_LABELS[cmdType]})
            </span>
          </div>
          <Input
            placeholder="Email (configurado em AI Settings)"
            value={aiEmail}
            onChange={(e) => {
              setAiEmail(e.target.value);
              localStorage.setItem("ai_email", e.target.value);
            }}
            className="h-7 text-xs"
          />
          <Textarea
            placeholder={`Descreva o que deseja fazer em ${CMD_TYPE_LABELS[cmdType]}...`}
            value={aiPrompt}
            onChange={(e) => setAiPrompt(e.target.value)}
            className="text-xs resize-none"
            rows={3}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                handleGenerateCommand();
              }
            }}
          />
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              className="flex-1 h-7 text-xs"
              onClick={handleGenerateCommand}
              disabled={isGenerating || !aiPrompt.trim()}
            >
              {isGenerating ? (
                <><Loader2 className="h-3 w-3 mr-1 animate-spin" />Gerando...</>
              ) : (
                <><Sparkles className="h-3 w-3 mr-1" />Gerar (Ctrl+Enter)</>
              )}
            </Button>
            <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setShowAiPanel(false)}>
              <X className="h-3 w-3" />
            </Button>
          </div>
        </div>
      )}

      {/* Output por cluster */}
      <div className="flex-1 min-h-0 flex flex-col">
        {totalOutputClusters === 0 ? (
          <div className="flex flex-col items-center justify-center flex-1 text-muted-foreground">
            <Terminal className="h-12 w-12 mb-3 opacity-20" />
            <p className="text-sm font-medium">Nenhuma execução ainda</p>
            <p className="text-xs">Selecione clusters, escreva um comando e clique em Executar</p>
          </div>
        ) : (
          <div className="flex flex-col h-full">
            {/* Tabs por cluster */}
            <div className="flex gap-1 flex-wrap mb-2 flex-shrink-0">
              {Object.keys(outputs).map((cluster) => (
                <button
                  key={cluster}
                  onClick={() => setActiveOutputTab(cluster)}
                  className={`flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors ${
                    activeOutputTab === cluster
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted hover:bg-muted/80 text-muted-foreground"
                  }`}
                >
                  {statusIcon(clusterStatus[cluster] || "idle")}
                  <span className="max-w-[120px] truncate">{cluster}</span>
                  <Badge variant="secondary" className="text-xs px-1 ml-1">
                    {(outputs[cluster] || []).length}
                  </Badge>
                </button>
              ))}
            </div>

            {/* Output do cluster ativo */}
            {activeOutputTab && outputs[activeOutputTab] && (
              <div className="flex-1 min-h-0 flex flex-col">
                <div className="flex items-center justify-between mb-1 flex-shrink-0">
                  <span className="text-xs text-muted-foreground font-mono">{activeOutputTab}</span>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 text-xs"
                    onClick={() => copyOutput(activeOutputTab)}
                  >
                    <Copy className="h-3 w-3 mr-1" />
                    Copiar
                  </Button>
                </div>
                <ScrollArea className="flex-1 min-h-0 bg-black/80 rounded p-2 font-mono text-xs">
                  {outputs[activeOutputTab].map((line, i) => (
                    <div
                      key={i}
                      className={`leading-relaxed whitespace-pre-wrap break-all ${
                        line.isError ? "text-red-400" : "text-green-300"
                      }`}
                    >
                      {line.text}
                    </div>
                  ))}
                  <div ref={(el) => { outputEndRef.current[activeOutputTab] = el; }} />
                </ScrollArea>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );

  return (
    <SplitView
      leftPanel={{
        title: "Command Runner",
        titleAction: selectedClusters.length > 0 ? (
          <Badge variant="secondary" className="text-xs">
            {selectedClusters.length} cluster(s)
          </Badge>
        ) : undefined,
        content: leftContent,
      }}
      rightPanel={{
        title: isRunning ? "Executando..." : totalOutputClusters > 0 ? `Output (${totalOutputClusters} clusters)` : "Editor & Output",
        titleAction: isRunning ? (
          <Loader2 className="h-4 w-4 animate-spin text-blue-400" />
        ) : undefined,
        content: rightContent,
      }}
    />
  );
}
