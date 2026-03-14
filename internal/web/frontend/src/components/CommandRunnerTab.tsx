import { useState, useEffect, useRef, useMemo, useCallback } from "react";
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
  Send,
  Bot,
  User,
  Download,
  Layers,
} from "lucide-react";
import { toast } from "sonner";
import Editor from "@monaco-editor/react";

import { useClusters } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import type { CommandTarget, CommandType, CommandRunnerSSEEvent } from "@/lib/api/types";
import type { Cluster } from "@/lib/api/types";

interface CommandRunnerTabProps {
  selectedCluster?: string;
}

interface OutputLine {
  text: string;
  isError: boolean;
  timestamp: string;
}

type ClusterStatus = "idle" | "running" | "done" | "error";

interface ChatMessage {
  id: string;
  role: "user" | "ai";
  text: string;
  command?: string;
  cmdType?: CommandType;
}

const CMD_TYPE_LABELS: Record<CommandType, string> = {
  kubectl: "kubectl",
  sh: "Shell (sh)",
  bash: "Bash",
  python: "Python",
  go: "Go",
};

const EXAMPLE_COMMANDS: Record<CommandType, string> = {
  kubectl: "kubectl get pods -n {{namespace}} --context={{cluster}}",
  sh: 'echo "Cluster: {{cluster}}, NS: {{namespace}}"',
  bash: "for i in 1 2 3; do echo \"$i - {{cluster}}\"; done",
  python: "print(f'Cluster: {{cluster}}, Namespace: {{namespace}}')",
  go: `package main\nimport "fmt"\nfunc main() {\n    fmt.Println("Cluster: {{cluster}}")\n}`,
};

// ── Divisor arrastável horizontal (colunas) ───────────────────────────────────
function ResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientX - lastX.current);
      lastX.current = e.clientX;
    };
    const onUp = () => { dragging.current = false; document.body.style.cursor = ""; document.body.style.userSelect = ""; };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => { window.removeEventListener("mousemove", onMove); window.removeEventListener("mouseup", onUp); };
  }, [onDrag]);

  return (
    <div
      className="w-1 flex-shrink-0 bg-border/40 hover:bg-primary/60 active:bg-primary cursor-col-resize transition-colors"
      onMouseDown={(e) => {
        dragging.current = true;
        lastX.current = e.clientX;
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        e.preventDefault();
      }}
    />
  );
}

// ── Divisor arrastável vertical (linhas) ──────────────────────────────────────
function ResizeHDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastY = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientY - lastY.current);
      lastY.current = e.clientY;
    };
    const onUp = () => { dragging.current = false; document.body.style.cursor = ""; document.body.style.userSelect = ""; };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => { window.removeEventListener("mousemove", onMove); window.removeEventListener("mouseup", onUp); };
  }, [onDrag]);

  return (
    <div
      className="h-1 flex-shrink-0 bg-border/40 hover:bg-primary/60 active:bg-primary cursor-row-resize transition-colors"
      onMouseDown={(e) => {
        dragging.current = true;
        lastY.current = e.clientY;
        document.body.style.cursor = "row-resize";
        document.body.style.userSelect = "none";
        e.preventDefault();
      }}
    />
  );
}

// Mapa de cores ANSI standard + bright para CSS
const ANSI_FG: Record<number, string> = {
  30: "#4e4e4e", 31: "#ff5555", 32: "#50fa7b", 33: "#f1fa8c",
  34: "#6272a4", 35: "#ff79c6", 36: "#8be9fd", 37: "#f8f8f2",
  90: "#6272a4", 91: "#ff6e6e", 92: "#69ff94", 93: "#ffffa5",
  94: "#d6acff", 95: "#ff92df", 96: "#a4ffff", 97: "#ffffff",
};
const ANSI_BG: Record<number, string> = {
  40: "#282a36", 41: "#ff5555", 42: "#50fa7b", 43: "#f1fa8c",
  44: "#6272a4", 45: "#ff79c6", 46: "#8be9fd", 47: "#f8f8f2",
  100: "#6272a4", 101: "#ff6e6e", 102: "#69ff94", 103: "#ffffa5",
  104: "#d6acff", 105: "#ff92df", 106: "#a4ffff", 107: "#ffffff",
};

/** Converte uma linha com escape codes ANSI em spans React coloridos */
function AnsiLine({ text }: { text: string }) {
  const parts: { text: string; style: React.CSSProperties }[] = [];
  // Remove carriage returns (saída de comandos com \r\n)
  const clean = text.replace(/\r/g, "");
  const ansiRe = /\x1b\[([0-9;]*)m/g;
  let style: React.CSSProperties = {};
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = ansiRe.exec(clean)) !== null) {
    if (m.index > last) parts.push({ text: clean.slice(last, m.index), style: { ...style } });
    const codes = m[1].split(";").map(Number);
    for (let i = 0; i < codes.length; i++) {
      const c = codes[i];
      if (c === 0) { style = {}; }
      else if (c === 1) { style = { ...style, fontWeight: "bold" }; }
      else if (c === 2) { style = { ...style, opacity: 0.7 }; }
      else if (c === 3) { style = { ...style, fontStyle: "italic" }; }
      else if (c === 4) { style = { ...style, textDecoration: "underline" }; }
      else if (ANSI_FG[c]) { style = { ...style, color: ANSI_FG[c] }; }
      else if (ANSI_BG[c]) { style = { ...style, backgroundColor: ANSI_BG[c] }; }
      else if (c === 38 && codes[i + 1] === 5) { style = { ...style, color: `var(--ansi-256-${codes[i + 2]})` }; i += 2; }
      else if (c === 38 && codes[i + 1] === 2) { style = { ...style, color: `rgb(${codes[i+2]},${codes[i+3]},${codes[i+4]})` }; i += 4; }
    }
    last = m.index + m[0].length;
  }
  if (last < clean.length) parts.push({ text: clean.slice(last), style: { ...style } });
  if (parts.length === 0) return <span>{clean}</span>;
  return <>{parts.map((p, i) => <span key={i} style={p.style}>{p.text}</span>)}</>;
}

export function CommandRunnerTab({ selectedCluster }: CommandRunnerTabProps) {
  const { clusters: allClusters } = useClusters();

  // ── larguras/alturas dos painéis (px) ────────────────────────────────────
  const [leftWidth, setLeftWidth] = useState(280);
  const [rightWidth, setRightWidth] = useState(360);
  const [editorHeight, setEditorHeight] = useState(220);

  // ── target selection ──────────────────────────────────────────────────────
  const [selectedClusters, setSelectedClusters] = useState<string[]>(
    selectedCluster ? [selectedCluster] : []
  );
  const [clusterSearch, setClusterSearch] = useState("");
  const [selectedNamespaces, setSelectedNamespaces] = useState<string[]>([]);
  const [nsSearch, setNsSearch] = useState("");
  const [allNs, setAllNs] = useState(false);

  // Namespaces dinâmicos: agrega de TODOS os clusters selecionados
  const [mergedNamespaces, setMergedNamespaces] = useState<string[]>([]);
  useEffect(() => {
    if (selectedClusters.length === 0) { setMergedNamespaces([]); setSelectedNamespaces([]); return; }
    let cancelled = false;
    Promise.all(selectedClusters.map((c) => apiClient.getNamespaces(c).catch(() => [])))
      .then((results) => {
        if (cancelled) return;
        const all = results.flat().map((ns: { name: string }) => ns.name);
        const available = [...new Set(all)].sort();
        setMergedNamespaces(available);
        // Manter apenas seleção prévia que ainda existe no novo cluster
        setSelectedNamespaces((prev) => prev.filter((ns) => available.includes(ns)));
      });
    return () => { cancelled = true; };
  }, [selectedClusters.join(",")]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── command ───────────────────────────────────────────────────────────────
  const [cmdType, setCmdType] = useState<CommandType>("kubectl");
  const [command, setCommand] = useState(EXAMPLE_COMMANDS.kubectl);
  const [timeoutSec, setTimeoutSec] = useState(300);

  // ── execution ─────────────────────────────────────────────────────────────
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [outputs, setOutputs] = useState<Record<string, OutputLine[]>>({});
  const [clusterStatus, setClusterStatus] = useState<Record<string, ClusterStatus>>({});
  const [activeOutputTab, setActiveOutputTab] = useState<string>("");
  const esRef = useRef<EventSource | null>(null);
  const terminalEndRef = useRef<HTMLDivElement>(null);

  // ── chat AI ───────────────────────────────────────────────────────────────
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([]);
  const [chatInput, setChatInput] = useState("");
  const [isGenerating, setIsGenerating] = useState(false);
  const [aiEmail] = useState(() => localStorage.getItem("ai_email") || "");
  const chatEndRef = useRef<HTMLDivElement>(null);

  // ── derived ───────────────────────────────────────────────────────────────
  const clusterNames = useMemo(() => {
    if (!allClusters) return [] as string[];
    return allClusters.map((c: Cluster) => c.context);
  }, [allClusters]);

  const clusterGroups = useMemo(() => {
    const groups = {
      prd: [] as string[],
      hlg: [] as string[],
      dev: [] as string[],
      other: [] as string[],
    };
    clusterNames.forEach((name: string) => {
      const lower = name.toLowerCase();
      if (lower.includes("-prd") || lower.includes("-prod")) groups.prd.push(name);
      else if (lower.includes("-hlg") || lower.includes("-uat") || lower.includes("-sit"))
        groups.hlg.push(name);
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

  const filteredNamespaces = useMemo(() => {
    if (!nsSearch) return mergedNamespaces;
    const q = nsSearch.toLowerCase();
    return mergedNamespaces.filter((ns) => ns.toLowerCase().includes(q));
  }, [mergedNamespaces, nsSearch]);

  const totalOutputClusters = Object.keys(outputs).length;

  // ── auto-scroll ───────────────────────────────────────────────────────────
  useEffect(() => {
    terminalEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [outputs, activeOutputTab]);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [chatMessages]);

  // ── SSE ───────────────────────────────────────────────────────────────────
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
        /* ignore */
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

  const getOutputKey = (cluster: string, namespace?: string) =>
    namespace && namespace !== "" ? `${cluster}/${namespace}` : cluster;

  const handleSSEEvent = useCallback((event: CommandRunnerSSEEvent) => {
    const key = getOutputKey(event.cluster || "__global__", event.details);

    if (event.type === "output" || event.type === "output_error") {
      setOutputs((prev) => ({
        ...prev,
        [key]: [
          ...(prev[key] || []),
          { text: event.message, isError: event.type === "output_error", timestamp: event.timestamp },
        ],
      }));
      setActiveOutputTab((prev) => prev || key);
    }

    if (event.type === "cluster_done") {
      setClusterStatus((prev) => ({
        ...prev,
        [key]: prev[key] === "error" ? "error" : "done",
      }));
    }

    if (event.type === "output_error") {
      setClusterStatus((prev) => ({ ...prev, [key]: "error" }));
    }

    if (event.type === "complete" || event.type === "error") {
      setIsRunning(false);
      esRef.current?.close();
      if (event.type === "error") toast.error("Execução finalizada com erros");
      else toast.success(event.message);
    }
  }, []);

  // ── execute ───────────────────────────────────────────────────────────────
  const buildTargets = (): CommandTarget[] => {
    // Sem namespace selecionado = um target por cluster sem restrição de namespace
    if (allNs || selectedNamespaces.length === 0) {
      return selectedClusters.map((cluster) => ({ cluster, namespace: "" }));
    }
    const result: CommandTarget[] = [];
    for (const cluster of selectedClusters) {
      for (const ns of selectedNamespaces) {
        result.push({ cluster, namespace: ns });
      }
    }
    return result;
  };

  const handleExecute = async () => {
    if (selectedClusters.length === 0) {
      toast.error("Selecione pelo menos um cluster");
      return;
    }
    if (!command.trim()) {
      toast.error("Escreva um comando");
      return;
    }

    const targets = buildTargets();
    const initialStatus: Record<string, ClusterStatus> = {};
    const initialOutputs: Record<string, OutputLine[]> = {};
    targets.forEach((t) => {
      const key = getOutputKey(t.cluster, t.namespace);
      initialStatus[key] = "running";
      initialOutputs[key] = [];
    });
    setClusterStatus(initialStatus);
    setOutputs(initialOutputs);
    setActiveOutputTab(Object.keys(initialOutputs)[0] || "");
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

  // ── chat ──────────────────────────────────────────────────────────────────
  const handleChatSend = async () => {
    if (!chatInput.trim()) return;
    if (!aiEmail) {
      toast.error("Configure o email de AI em AI Settings");
      return;
    }

    const userMsg: ChatMessage = {
      id: Date.now().toString(),
      role: "user",
      text: chatInput.trim(),
    };
    setChatMessages((prev) => [...prev, userMsg]);
    const prompt = chatInput.trim();
    setChatInput("");
    setIsGenerating(true);

    try {
      const res = await apiClient.generateCommand({
        prompt,
        cluster: selectedClusters[0] || "",
        namespace: allNs ? "" : (selectedNamespaces[0] || "default"),
        clusters: selectedClusters,
        namespaces: allNs ? ["*"] : selectedNamespaces,
        ai_email: aiEmail,
        cmd_type: cmdType,
        explain: true,
      });

      const aiMsg: ChatMessage = {
        id: (Date.now() + 1).toString(),
        role: "ai",
        text: res.explanation || "",
        command: res.command,
        cmdType: res.type,
      };
      setChatMessages((prev) => [...prev, aiMsg]);
    } catch (err) {
      const errMsg: ChatMessage = {
        id: (Date.now() + 1).toString(),
        role: "ai",
        text: `Erro: ${err instanceof Error ? err.message : "Erro desconhecido"}`,
      };
      setChatMessages((prev) => [...prev, errMsg]);
    } finally {
      setIsGenerating(false);
    }
  };

  // ── helpers ───────────────────────────────────────────────────────────────
  const toggleCluster = (c: string) =>
    setSelectedClusters((prev) =>
      prev.includes(c) ? prev.filter((x) => x !== c) : [...prev, c]
    );

  const toggleNamespace = (ns: string) =>
    setSelectedNamespaces((prev) =>
      prev.includes(ns) ? prev.filter((x) => x !== ns) : [...prev, ns]
    );

  const selectClusterGroup = (group: "prd" | "hlg" | "dev" | "all") => {
    if (group === "all") setSelectedClusters(clusterNames);
    else setSelectedClusters(clusterGroups[group]);
  };

  const statusIcon = (status: ClusterStatus) => {
    switch (status) {
      case "running":
        return <Loader2 className="h-3 w-3 animate-spin text-blue-400" />;
      case "done":
        return <CheckCircle className="h-3 w-3 text-green-400" />;
      case "error":
        return <XCircle className="h-3 w-3 text-red-400" />;
      default:
        return <Clock className="h-3 w-3 text-muted-foreground" />;
    }
  };

  const copyOutput = (key: string) => {
    const lines = (outputs[key] || []).map((l) => l.text).join("\n");
    navigator.clipboard.writeText(lines);
    toast.success("Output copiado");
  };

  // ── render ────────────────────────────────────────────────────────────────
  return (
    <div className="flex h-full overflow-hidden bg-background">

      {/* ═══ LEFT: Target selection ═══════════════════════════════════════ */}
      <div className="flex-shrink-0 border-r flex flex-col overflow-hidden" style={{ width: leftWidth }}>
        <div className="px-3 py-2 border-b flex-shrink-0">
          <h3 className="text-sm font-semibold">Command Runner</h3>
        </div>
        <ScrollArea className="flex-1">
          <div className="p-3 space-y-4">

            {/* Clusters */}
            <div>
              <div className="flex items-center justify-between mb-1">
                <Label className="text-xs font-medium">Clusters</Label>
                {selectedClusters.length > 0 && (
                  <button
                    className="text-xs text-red-400 hover:text-red-300"
                    onClick={() => setSelectedClusters([])}
                  >
                    Limpar ({selectedClusters.length})
                  </button>
                )}
              </div>

              <div className="flex flex-wrap gap-1 mb-1.5">
                <button
                  className="text-xs px-1.5 py-0.5 rounded border border-border hover:bg-muted"
                  onClick={() => selectClusterGroup("all")}
                >
                  Todos
                </button>
                {clusterGroups.prd.length > 0 && (
                  <button
                    className="text-xs px-1.5 py-0.5 rounded border border-border hover:bg-muted"
                    onClick={() => selectClusterGroup("prd")}
                  >
                    Prod
                  </button>
                )}
                {clusterGroups.hlg.length > 0 && (
                  <button
                    className="text-xs px-1.5 py-0.5 rounded border border-border hover:bg-muted"
                    onClick={() => selectClusterGroup("hlg")}
                  >
                    HLG
                  </button>
                )}
                {clusterGroups.dev.length > 0 && (
                  <button
                    className="text-xs px-1.5 py-0.5 rounded border border-border hover:bg-muted"
                    onClick={() => selectClusterGroup("dev")}
                  >
                    Dev
                  </button>
                )}
              </div>

              <div className="relative mb-1.5">
                <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                <Input
                  placeholder="Filtrar clusters..."
                  value={clusterSearch}
                  onChange={(e) => setClusterSearch(e.target.value)}
                  className="pl-6 h-6 text-xs"
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

              <div className="space-y-0.5 max-h-36 overflow-y-auto pr-1">
                {filteredClusterNames.map((c: string) => (
                  <div key={c} className="flex items-center gap-1.5">
                    <Checkbox
                      id={`cr-c-${c}`}
                      checked={selectedClusters.includes(c)}
                      onCheckedChange={() => toggleCluster(c)}
                      className="h-3 w-3"
                    />
                    <label
                      htmlFor={`cr-c-${c}`}
                      className="text-xs cursor-pointer truncate flex items-center gap-1"
                    >
                      <Server className="h-3 w-3 text-blue-400 flex-shrink-0" />
                      {c}
                    </label>
                  </div>
                ))}
                {filteredClusterNames.length === 0 && (
                  <p className="text-xs text-muted-foreground py-1 text-center">Nenhum cluster</p>
                )}
              </div>
            </div>

            {/* Namespaces */}
            <div>
              <div className="flex items-center justify-between mb-1">
                <Label className="text-xs font-medium">Namespaces</Label>
                <div className="flex gap-1 items-center">
                  <button
                    className={`text-xs px-1.5 py-0.5 rounded border transition-colors ${
                      allNs
                        ? "bg-primary text-primary-foreground border-primary"
                        : "border-border hover:bg-muted"
                    }`}
                    onClick={() => {
                      setAllNs(true);
                      setSelectedNamespaces([]);
                    }}
                  >
                    Todos
                  </button>
                  {allNs && (
                    <button
                      className="text-red-400 hover:text-red-300"
                      onClick={() => {
                        setAllNs(false);
                        setSelectedNamespaces(["default"]);
                      }}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  )}
                  {!allNs && selectedNamespaces.length > 0 && (
                    <button
                      className="text-xs text-red-400 hover:text-red-300"
                      onClick={() => setSelectedNamespaces([])}
                    >
                      Limpar
                    </button>
                  )}
                </div>
              </div>

              {allNs ? (
                <p className="text-xs text-muted-foreground">
                  Use <code className="bg-muted px-0.5 rounded">-A</code> ou{" "}
                  <code className="bg-muted px-0.5 rounded">--all-namespaces</code> no comando.
                </p>
              ) : (
                <>
                  <div className="relative mb-1.5">
                    <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                    <Input
                      placeholder="Filtrar NS..."
                      value={nsSearch}
                      onChange={(e) => setNsSearch(e.target.value)}
                      className="pl-6 h-6 text-xs"
                    />
                  </div>

                  <div className="space-y-0.5 max-h-28 overflow-y-auto pr-1">
                    {filteredNamespaces.length > 0 ? (
                      filteredNamespaces.map((ns) => (
                        <div key={ns} className="flex items-center gap-1.5">
                          <Checkbox
                            id={`cr-ns-${ns}`}
                            checked={selectedNamespaces.includes(ns)}
                            onCheckedChange={() => toggleNamespace(ns)}
                            className="h-3 w-3"
                          />
                          <label
                            htmlFor={`cr-ns-${ns}`}
                            className="text-xs cursor-pointer truncate flex items-center gap-1"
                          >
                            <Layers className="h-3 w-3 text-yellow-400 flex-shrink-0" />
                            {ns}
                          </label>
                        </div>
                      ))
                    ) : (
                      <Input
                        placeholder="Digite e pressione Enter..."
                        className="h-6 text-xs"
                        onKeyDown={(e) => {
                          if (e.key === "Enter" && e.currentTarget.value.trim()) {
                            toggleNamespace(e.currentTarget.value.trim());
                            e.currentTarget.value = "";
                          }
                        }}
                      />
                    )}
                  </div>

                  {selectedNamespaces.length > 0 && (
                    <div className="flex flex-wrap gap-1 mt-1.5">
                      {selectedNamespaces.map((ns) => (
                        <Badge key={ns} variant="secondary" className="text-xs px-1 py-0 gap-1">
                          {ns}
                          <button onClick={() => toggleNamespace(ns)}>
                            <X className="h-2.5 w-2.5" />
                          </button>
                        </Badge>
                      ))}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Language */}
            <div>
              <Label className="text-xs font-medium mb-1 block">Linguagem</Label>
              <Select
                value={cmdType}
                onValueChange={(v) => {
                  const t = v as CommandType;
                  setCmdType(t);
                  if (!command || command === EXAMPLE_COMMANDS[cmdType])
                    setCommand(EXAMPLE_COMMANDS[t]);
                }}
              >
                <SelectTrigger className="h-7 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(CMD_TYPE_LABELS) as CommandType[]).map((t) => (
                    <SelectItem key={t} value={t}>
                      {CMD_TYPE_LABELS[t]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Timeout */}
            <div>
              <Label className="text-xs font-medium mb-1 block">Timeout (s)</Label>
              <Input
                type="number"
                min={10}
                max={1800}
                value={timeoutSec}
                onChange={(e) => setTimeoutSec(Number(e.target.value))}
                className="h-7 text-xs"
              />
            </div>

            {/* Execute */}
            <ProtectedAction>
              <Button
                className="w-full"
                size="sm"
                onClick={
                  isRunning
                    ? async () => {
                        esRef.current?.close();
                        setIsRunning(false);
                        if (sessionId) {
                          try {
                            await apiClient.cancelCommand(sessionId);
                            toast.warning("Execução interrompida");
                          } catch (err) {
                            toast.error("Falha ao parar processo no servidor", {
                              description: err instanceof Error ? err.message : "Verifique permissões",
                            });
                          }
                        }
                      }
                    : handleExecute
                }
                disabled={!isRunning && selectedClusters.length === 0}
                variant={isRunning ? "destructive" : "default"}
              >
                {isRunning ? (
                  <>
                    <Square className="h-3 w-3 mr-1.5" />
                    Interromper
                  </>
                ) : (
                  <>
                    <Play className="h-3 w-3 mr-1.5" />
                    Executar ({selectedClusters.length} &times;{" "}
                    {allNs ? "∞ NS" : selectedNamespaces.length + " NS"})
                  </>
                )}
              </Button>
            </ProtectedAction>

            {/* Status por cluster */}
            {Object.keys(clusterStatus).length > 0 && (
              <div className="space-y-1 pt-2 border-t border-border/50">
                <div className="flex items-center justify-between">
                  <p className="text-xs text-muted-foreground">Status</p>
                  <button
                    className="text-muted-foreground hover:text-foreground"
                    onClick={() => {
                      setOutputs({});
                      setClusterStatus({});
                      setSessionId(null);
                    }}
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
                {Object.entries(clusterStatus).map(([key, status]) => (
                  <div key={key} className="flex items-center gap-1.5 py-0.5">
                    {statusIcon(status)}
                    <span className="text-xs truncate">{key}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </ScrollArea>
      </div>

      <ResizeDivider onDrag={(d) => setLeftWidth((w) => Math.max(200, Math.min(480, w + d)))} />

      {/* ═══ CENTER: Terminal + Editor ════════════════════════════════════ */}
      <div className="flex-1 flex flex-col overflow-hidden min-w-0">

        {/* Terminal output — topo, flex-1 */}
        <div className="flex-1 flex flex-col overflow-hidden px-3 pt-3 pb-2">
          {totalOutputClusters === 0 ? (
            <div className="flex flex-col items-center justify-center flex-1 text-muted-foreground">
              <Terminal className="h-10 w-10 mb-2 opacity-20" />
              <p className="text-sm font-medium">Terminal</p>
              <p className="text-xs">Selecione targets e clique em Executar</p>
            </div>
          ) : (
            <>
              <div className="flex gap-1 flex-wrap mb-2 flex-shrink-0 items-center">
                {Object.keys(outputs).map((key) => (
                  <button
                    key={key}
                    onClick={() => setActiveOutputTab(key)}
                    className={`flex items-center gap-1 px-2 py-0.5 rounded text-xs transition-colors ${
                      activeOutputTab === key
                        ? "bg-primary text-primary-foreground"
                        : "bg-muted hover:bg-muted/80 text-muted-foreground"
                    }`}
                  >
                    {statusIcon(clusterStatus[key] || "idle")}
                    <span className="max-w-[140px] truncate">{key}</span>
                    <Badge variant="secondary" className="text-xs px-1 ml-0.5">
                      {(outputs[key] || []).length}
                    </Badge>
                  </button>
                ))}
                <div className="ml-auto flex gap-1">
                  {isRunning && sessionId && (
                    <Button
                      size="sm"
                      variant="destructive"
                      className="h-6 text-xs"
                      onClick={async () => {
                        esRef.current?.close();
                        setIsRunning(false);
                        if (sessionId) {
                          try { await apiClient.cancelCommand(sessionId); } catch { /* ignorar */ }
                        }
                        toast.warning("Processos cancelados no servidor");
                      }}
                    >
                      <Square className="h-3 w-3 mr-1" />
                      Forçar parada
                    </Button>
                  )}
                  {activeOutputTab && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-6 text-xs"
                      onClick={() => copyOutput(activeOutputTab)}
                    >
                      <Copy className="h-3 w-3 mr-1" />
                      Copiar
                    </Button>
                  )}
                </div>
              </div>

              {activeOutputTab && outputs[activeOutputTab] && (
                <ScrollArea className="flex-1 bg-[#1e1e2e] rounded p-2 font-mono text-xs">
                  {outputs[activeOutputTab].map((line, i) => (
                    <div
                      key={i}
                      className={`leading-relaxed whitespace-pre-wrap break-all ${
                        line.isError ? "text-red-400" : "text-[#cdd6f4]"
                      }`}
                    >
                      <AnsiLine text={line.text} />
                    </div>
                  ))}
                  <div ref={terminalEndRef} />
                </ScrollArea>
              )}
            </>
          )}
        </div>

        <ResizeHDivider onDrag={(d) => setEditorHeight((h) => Math.max(120, Math.min(600, h - d)))} />

        {/* Editor — baixo, altura ajustável */}
        <div className="flex-shrink-0 px-3 pt-2 pb-3" style={{ height: editorHeight + 64 }}>
          <div className="flex items-center justify-between mb-1.5">
            <Label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              {CMD_TYPE_LABELS[cmdType]}
            </Label>
            <div className="flex gap-1">
              <Button
                size="sm"
                variant="ghost"
                className="h-6 text-xs"
                onClick={() => { navigator.clipboard.writeText(command); toast.success("Copiado"); }}
              >
                <Copy className="h-3 w-3 mr-1" />Copiar
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
          <div className="border border-border/50 rounded overflow-hidden">
            <Editor
              height={`${editorHeight}px`}
              language={cmdType === "python" ? "python" : cmdType === "go" ? "go" : "shell"}
              value={command}
              onChange={(v) => setCommand(v || "")}
              theme="vs-dark"
              options={{
                minimap: { enabled: false },
                fontSize: 12,
                lineNumbers: "on",
                wordWrap: "on",
                scrollBeyondLastLine: false,
                padding: { top: 6, bottom: 6 },
              }}
            />
          </div>
          <p className="text-xs text-muted-foreground mt-1">
            <code className="bg-muted px-0.5 rounded">{"{{cluster}}"}</code> e{" "}
            <code className="bg-muted px-0.5 rounded">{"{{namespace}}"}</code> são substituídos em cada target.
          </p>
        </div>
      </div>

      <ResizeDivider onDrag={(d) => setRightWidth((w) => Math.max(240, Math.min(900, w - d)))} />

      {/* ═══ RIGHT: Chat AI ═══════════════════════════════════════════════ */}
      <div className="flex-shrink-0 flex flex-col overflow-hidden" style={{ width: rightWidth }}>

        {/* Header */}
        <div className="px-3 py-2 border-b flex-shrink-0 flex items-center gap-2">
          <Sparkles className="h-4 w-4 text-purple-400" />
          <h3 className="text-sm font-semibold">Assistente AI</h3>
          {!aiEmail && (
            <Badge variant="outline" className="text-xs ml-auto text-yellow-400 border-yellow-400/50">
              Sem email
            </Badge>
          )}
        </div>

        {/* Messages */}
        <ScrollArea className="flex-1 p-3">
          {chatMessages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-32 text-muted-foreground">
              <Bot className="h-8 w-8 mb-2 opacity-30" />
              <p className="text-xs text-center leading-relaxed">
                Peça ao assistente para gerar ou explicar um comando. O resultado pode ser inserido diretamente no editor.
              </p>
            </div>
          ) : (
            <div className="space-y-3">
              {chatMessages.map((msg) => (
                <div
                  key={msg.id}
                  className={`flex gap-2 ${msg.role === "user" ? "flex-row-reverse" : ""}`}
                >
                  <div
                    className={`flex-shrink-0 h-6 w-6 rounded-full flex items-center justify-center ${
                      msg.role === "user" ? "bg-primary" : "bg-purple-600"
                    }`}
                  >
                    {msg.role === "user" ? (
                      <User className="h-3 w-3 text-primary-foreground" />
                    ) : (
                      <Bot className="h-3 w-3 text-white" />
                    )}
                  </div>

                  <div className={`flex-1 min-w-0 space-y-1.5 ${msg.role === "user" ? "items-end flex flex-col" : ""}`}>
                    {msg.role === "user" ? (
                      <div className="inline-block bg-primary text-primary-foreground rounded-lg px-3 py-1.5 text-xs text-left max-w-full">
                        {msg.text}
                      </div>
                    ) : (
                      <>
                        {msg.text && (
                          <div className="bg-muted rounded-lg px-3 py-1.5 text-xs text-foreground/90 whitespace-pre-wrap leading-relaxed">
                            {msg.text}
                          </div>
                        )}
                        {msg.command && (
                          <div className="bg-black/70 rounded border border-border/50 overflow-hidden">
                            <div className="flex items-center justify-between px-2 py-1 border-b border-border/30">
                              <span className="text-xs text-muted-foreground font-mono">
                                {msg.cmdType || cmdType}
                              </span>
                              <button
                                className="text-muted-foreground hover:text-foreground"
                                onClick={() => {
                                  navigator.clipboard.writeText(msg.command!);
                                  toast.success("Copiado");
                                }}
                              >
                                <Copy className="h-3 w-3" />
                              </button>
                            </div>
                            <pre className="p-2 text-xs text-green-300 font-mono overflow-x-auto whitespace-pre-wrap break-all">
                              {msg.command}
                            </pre>
                            <div className="px-2 pb-2">
                              <Button
                                size="sm"
                                className="w-full h-6 text-xs"
                                onClick={() => {
                                  setCommand(msg.command!);
                                  if (msg.cmdType) setCmdType(msg.cmdType);
                                  toast.success("Comando inserido no editor");
                                }}
                              >
                                <Download className="h-3 w-3 mr-1" />
                                Inserir no editor
                              </Button>
                            </div>
                          </div>
                        )}
                      </>
                    )}
                  </div>
                </div>
              ))}

              {isGenerating && (
                <div className="flex gap-2">
                  <div className="flex-shrink-0 h-6 w-6 rounded-full bg-purple-600 flex items-center justify-center">
                    <Bot className="h-3 w-3 text-white" />
                  </div>
                  <div className="bg-muted rounded-lg px-3 py-2 flex items-center">
                    <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                  </div>
                </div>
              )}

              <div ref={chatEndRef} />
            </div>
          )}
        </ScrollArea>

        {/* Input */}
        <div className="p-3 border-t flex-shrink-0 space-y-2">
          {!aiEmail && (
            <p className="text-xs text-yellow-400">
              Configure seu email em <strong>AI Settings</strong>.
            </p>
          )}
          <Textarea
            placeholder={`Descreva o que quer fazer em ${CMD_TYPE_LABELS[cmdType]}... (Ctrl+Enter)`}
            value={chatInput}
            onChange={(e) => setChatInput(e.target.value)}
            className="text-xs resize-none"
            rows={3}
            disabled={!aiEmail || isGenerating}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                e.preventDefault();
                handleChatSend();
              }
            }}
          />
          <div className="flex items-center justify-between">
            {chatMessages.length > 0 ? (
              <button
                className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
                onClick={() => setChatMessages([])}
              >
                <Trash2 className="h-3 w-3" />
                Limpar
              </button>
            ) : (
              <span />
            )}
            <Button
              size="sm"
              onClick={handleChatSend}
              disabled={!chatInput.trim() || !aiEmail || isGenerating}
              className="h-7 text-xs"
            >
              {isGenerating ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <>
                  <Send className="h-3 w-3 mr-1" />
                  Enviar
                </>
              )}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
