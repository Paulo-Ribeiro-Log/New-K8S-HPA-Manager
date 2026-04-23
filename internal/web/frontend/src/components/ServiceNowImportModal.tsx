import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import {
  Loader2,
  Download,
  CheckCircle2,
  AlertCircle,
  FileText,
  GitBranch,
  Package,
  Users,
  ExternalLink,
  Copy,
  ClipboardPaste,
  Globe,
  Plus,
  RefreshCw,
  MessageSquare,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface ExtractedData {
  application: string;
  version: string;
  github_repo: string;
  xlrelease_url: string;
  squad: string;
  branch: string;
  jira_issues: string[];
  product: string;
  project: string;
  xlrelease_title: string;
  severity: string;
  confidence: number;
}

interface ChangeRequest {
  sys_id: string;
  number: string;
  short_description: string;
  description: string;
  state: string;
}

interface ImportResponse {
  success: boolean;
  change_request?: ChangeRequest;
  extracted_data?: ExtractedData;
  error?: string;
}

interface TeamsApprovalItem {
  chg: string;
  servicenow_url?: string;
  approval_url: string;
  description?: string;
  extracted_at: string;
}

interface ServiceNowImportModalProps {
  open: boolean;
  onClose: () => void;
  onImportSuccess: (data: {
    deploymentName: string;
    githubRepo: string;
    newVersion: string;
    xlReleaseUrl?: string;
    changeNumber?: string;
    approvalUrl?: string;
  }) => void;
}

type ActiveTab = "teams" | "playwright" | "manual";

export function ServiceNowImportModal({
  open,
  onClose,
  onImportSuccess,
}: ServiceNowImportModalProps) {
  const [activeTab, setActiveTab] = useState<ActiveTab>("teams");
  const [chgUrl, setChgUrl] = useState("");
  const [description, setDescription] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [result, setResult] = useState<ImportResponse | null>(null);
  const [addedCount, setAddedCount] = useState(0);
  const [playwrightStatus, setPlaywrightStatus] = useState<{
    configured: boolean;
    checked: boolean;
    isWsl: boolean;
    wslMode: boolean;
  }>({ configured: false, checked: false, isWsl: false, wslMode: false });
  const [forceWindowsBrowser, setForceWindowsBrowser] = useState(false);
  const [savingBrowserConfig, setSavingBrowserConfig] = useState(false);

  // Teams state
  const [teamsItems, setTeamsItems] = useState<TeamsApprovalItem[]>([]);
  const [teamsLoading, setTeamsLoading] = useState(false);
  const [teamsLastUpdated, setTeamsLastUpdated] = useState<string | null>(null);
  const [teamsNeedsRefresh, setTeamsNeedsRefresh] = useState(true);
  const [chgSearch, setChgSearch] = useState("");
  // Multi-select: set de CHGs selecionados
  const [selectedTeamsChgs, setSelectedTeamsChgs] = useState<Set<string>>(new Set());
  // CHG pendente para extração via browser (aba Via Browser)
  const [pendingTeamsItem, setPendingTeamsItem] = useState<TeamsApprovalItem | null>(null);
  // Progresso da extração multi-CHG
  const [teamsExtracting, setTeamsExtracting] = useState<{ current: number; total: number; chg: string } | null>(null);

  useEffect(() => {
    if (open) {
      if (!playwrightStatus.checked) checkPlaywrightStatus();
      loadTeamsApprovals();
    }
  }, [open]);

  const loadTeamsApprovals = async () => {
    setTeamsLoading(true);
    try {
      const res = await apiClient.getTeamsApprovalsToday();
      setTeamsItems(res.items || []);
      setTeamsLastUpdated(res.last_updated);
      setTeamsNeedsRefresh(res.needs_refresh);
    } catch {
      setTeamsNeedsRefresh(true);
    } finally {
      setTeamsLoading(false);
    }
  };

  const handleTeamsRefresh = async () => {
    setTeamsLoading(true);
    toast.info("Abrindo Teams no Chrome... aguarde ~60s", { duration: 70000, id: "teams-refresh" });
    try {
      const res = await apiClient.refreshTeamsApprovals();
      if (res.success) {
        setTeamsItems(res.items || []);
        setTeamsLastUpdated(res.last_updated);
        setTeamsNeedsRefresh(false);
        toast.dismiss("teams-refresh");
        toast.success(`${res.items?.length || 0} CHGs capturadas do Teams`);
      } else {
        toast.dismiss("teams-refresh");
        toast.error(res.error || "Falha ao buscar CHGs do Teams");
      }
    } catch (err) {
      toast.dismiss("teams-refresh");
      const msg = err instanceof Error ? err.message : "Erro desconhecido";
      toast.error(msg);
    } finally {
      setTeamsLoading(false);
    }
  };

  const chgSearchNorm = chgSearch.trim().toUpperCase();
  const isValidChgInput = /^CHG\d{5,}$/.test(chgSearchNorm);
  const filteredTeamsItems = chgSearch
    ? teamsItems.filter(i =>
        i.chg.includes(chgSearchNorm) ||
        (i.description || "").toLowerCase().includes(chgSearch.toLowerCase())
      )
    : teamsItems;
  const chgInList = filteredTeamsItems.some(i => i.chg === chgSearchNorm);

  const handleExtractDirectChg = async () => {
    const chg = chgSearchNorm;
    setTeamsExtracting({ current: 0, total: 1, chg });

    // 1. Verificar cache local (últimos 2 dias) — evita re-scan do Teams
    let snUrl = `https://viavarejo.service-now.com/change_request.do?sysparm_query=number=${chg}`;
    let cachedApprovalUrl = "";
    try {
      const cached = await apiClient.searchTeamsCHG(chg);
      if (cached.found && cached.item) {
        if (cached.item.servicenow_url) snUrl = cached.item.servicenow_url;
        cachedApprovalUrl = cached.item.approval_url || "";
      }
    } catch { /* ignora — usa URL construída */ }

    // 2. Extrair dados do ServiceNow
    try {
      const response = await apiClient.extractServiceNowWithPlaywright(snUrl);
      if (response.success && response.extracted_data) {
        const d = response.extracted_data as ExtractedData;
        onImportSuccess({
          deploymentName: d.application || chg,
          githubRepo: d.github_repo || d.application || "",
          newVersion: d.version || "",
          xlReleaseUrl: d.xlrelease_url,
          changeNumber: response.change_number || chg,
          approvalUrl: cachedApprovalUrl,
        });
        setAddedCount(c => c + 1);
        toast.success(`${chg} adicionada a comparações`);
        setChgSearch("");
      } else {
        toast.error(`${chg}: ${response.error || "falha na extração"}`);
      }
    } catch (err) {
      toast.error(`${chg}: ${err instanceof Error ? err.message : "erro"}`);
    }
    setTeamsExtracting(null);
  };

  // Extrai cada CHG selecionada via browser (Rod) para obter githubRepo + versão completos
  const handleImportSelectedTeams = async () => {
    const selected = teamsItems.filter(i => selectedTeamsChgs.has(i.chg));
    if (selected.length === 0) return;

    const successChgs: string[] = [];
    const errorItems: string[] = [];
    let completed = 0;

    setTeamsExtracting({ current: 0, total: selected.length, chg: "iniciando..." });

    const processItem = async (item: TeamsApprovalItem) => {
      const snUrl = item.servicenow_url;
      if (!snUrl) {
        errorItems.push(`${item.chg}: sem URL do ServiceNow`);
        setTeamsExtracting({ current: ++completed, total: selected.length, chg: item.chg });
        return;
      }
      try {
        const response = await apiClient.extractServiceNowWithPlaywright(snUrl);
        if (response.success && response.extracted_data) {
          const d = response.extracted_data as ExtractedData;
          onImportSuccess({
            deploymentName: d.application || item.description || item.chg,
            githubRepo: d.github_repo || d.application || "",
            newVersion: d.version || "",
            xlReleaseUrl: d.xlrelease_url,
            changeNumber: response.change_number || item.chg,
            approvalUrl: item.approval_url,
          });
          successChgs.push(item.chg);
        } else {
          errorItems.push(`${item.chg}: ${response.error || "falha na extração"}`);
        }
      } catch (err) {
        errorItems.push(`${item.chg}: ${err instanceof Error ? err.message : "erro desconhecido"}`);
      }
      setTeamsExtracting({ current: ++completed, total: selected.length, chg: item.chg });
    };

    // Processar em lotes de 3 em paralelo (N CHGs = 1 browser, N abas)
    const BATCH = 3;
    for (let i = 0; i < selected.length; i += BATCH) {
      await Promise.allSettled(selected.slice(i, i + BATCH).map(processItem));
    }

    setTeamsExtracting(null);
    setSelectedTeamsChgs(new Set());

    if (successChgs.length > 0) {
      setAddedCount(c => c + successChgs.length);
      toast.success(`${successChgs.length} CHG${successChgs.length > 1 ? "s" : ""} adicionada${successChgs.length > 1 ? "s" : ""} a comparações`);
    }
    if (errorItems.length > 0) {
      toast.error(`${errorItems.length} com erro: ${errorItems.join("; ")}`);
    }
  };

  const toggleTeamsChg = (chg: string) => {
    setSelectedTeamsChgs(prev => {
      const next = new Set(prev);
      if (next.has(chg)) next.delete(chg); else next.add(chg);
      return next;
    });
  };

  const checkPlaywrightStatus = async () => {
    try {
      const [status, browserCfg] = await Promise.all([
        apiClient.getPlaywrightStatus(),
        apiClient.getServiceNowBrowserConfig(),
      ]);
      setPlaywrightStatus({
        configured: status.playwright_configured && status.script_exists,
        checked: true,
        isWsl: status.is_wsl ?? false,
        wslMode: status.wsl_mode ?? false,
      });
      setForceWindowsBrowser(browserCfg.force_windows_browser);
      if (!status.playwright_configured || !status.script_exists) {
        // não redireciona automaticamente — deixa o usuário na aba teams
      }
    } catch {
      setPlaywrightStatus({ configured: false, checked: true, isWsl: false, wslMode: false });
    }
  };

  const handleOpenServiceNow = () => {
    if (chgUrl.trim()) {
      window.open(chgUrl.trim(), "_blank");
    } else {
      window.open("https://viavarejo.service-now.com/nav_to.do?uri=change_request_list.do", "_blank");
    }
  };

  const runPlaywrightExtraction = async (url: string) => {
    const targetUrl = url || chgUrl;
    if (!targetUrl.trim()) {
      toast.error("URL é obrigatória");
      return;
    }
    if (!targetUrl.includes("service-now.com")) {
      toast.error("URL deve ser do ServiceNow");
      return;
    }

    setIsLoading(true);
    setResult(null);

    try {
      const browserMsg =
        forceWindowsBrowser || playwrightStatus.wslMode
          ? "Abrindo Chrome/Edge do Windows... Faça login no Azure AD se necessário."
          : "Abrindo browser... Faça login no Azure AD se necessário.";
      toast.info(browserMsg, { duration: 10000 });

      const response = await apiClient.extractServiceNowWithPlaywright(targetUrl);

      if (response.success && response.extracted_data) {
        setResult({
          success: true,
          extracted_data: response.extracted_data as ExtractedData,
          change_request: {
            sys_id: "",
            number: response.change_number || "",
            short_description: response.short_description || "",
            description: response.description || "",
            state: response.state || "",
          },
        });
        toast.success(`CHG ${response.change_number || ""} extraída com sucesso!`);
      } else {
        toast.error(response.error || "Falha ao extrair dados");
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : "Erro desconhecido";
      toast.error(errorMsg);
    } finally {
      setIsLoading(false);
    }
  };

  const handleExtractWithPlaywright = () => runPlaywrightExtraction(chgUrl);

  const handlePasteFromClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        setDescription(text);
        toast.success("Texto colado da área de transferência");
      }
    } catch {
      toast.error("Não foi possível acessar a área de transferência");
    }
  };

  const handleParseDescription = async () => {
    if (!description.trim()) {
      toast.error("Cole o texto do 'Motivo da mudança'");
      return;
    }

    setIsLoading(true);
    setResult(null);

    try {
      const response = await fetch("/api/v1/servicenow/parse", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${localStorage.getItem("auth_token")}`,
        },
        body: JSON.stringify({ description: description.trim() }),
      });

      const data = await response.json();

      if (!data.success) {
        const errorMsg = typeof data.error === "string"
          ? data.error
          : data.error?.message || "Erro ao extrair dados";
        toast.error(errorMsg);
        return;
      }

      setResult({ success: true, extracted_data: data.extracted_data });
      toast.success("Dados extraídos com sucesso!");
    } catch {
      toast.error("Erro ao conectar com o servidor");
    } finally {
      setIsLoading(false);
    }
  };

  const handleUseData = () => {
    if (!result?.extracted_data) {
      toast.error("Nenhum dado para usar");
      return;
    }

    const data = result.extracted_data;
    const chgNumber = result.change_request?.number;
    // Se viemos do Teams, pegar a approvalUrl do item pendente
    const approvalUrl = pendingTeamsItem?.approval_url;

    onImportSuccess({
      deploymentName: data.application,
      githubRepo: data.github_repo,
      newVersion: data.version,
      xlReleaseUrl: data.xlrelease_url,
      changeNumber: chgNumber,
      approvalUrl,
    });

    setResult(null);
    setChgUrl("");
    setDescription("");
    setPendingTeamsItem(null);
    setAddedCount((prev) => prev + 1);

    toast.success(
      chgNumber
        ? `${chgNumber} adicionada a comparações`
        : `${data.application || "CHG"} adicionada a comparações`
    );
  };

  const handleClose = () => {
    setChgUrl("");
    setDescription("");
    setResult(null);
    setAddedCount(0);
    setPendingTeamsItem(null);
    onClose();
  };

  const getConfidenceColor = (confidence: number) => {
    if (confidence >= 0.9) return "bg-green-500";
    if (confidence >= 0.7) return "bg-yellow-500";
    return "bg-red-500";
  };

  const getConfidenceText = (confidence: number) => {
    const percent = Math.round(confidence * 100);
    if (confidence >= 0.9) return `${percent}% - Alta`;
    if (confidence >= 0.7) return `${percent}% - Média`;
    return `${percent}% - Baixa`;
  };

  const formatLastUpdated = (ts: string | null) => {
    if (!ts) return null;
    const d = new Date(ts);
    return d.toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
  };

  // ── Tab bar manual (evita problema do shadcn Tabs com flex) ─────────
  const tabs: { id: ActiveTab; label: string; icon: React.ReactNode }[] = [
    { id: "teams", label: "Teams (Mr.ViaBot)", icon: <MessageSquare className="h-3.5 w-3.5" /> },
    { id: "playwright", label: "Via Browser", icon: <Globe className="h-3.5 w-3.5" /> },
    { id: "manual", label: "Texto Manual", icon: <FileText className="h-3.5 w-3.5" /> },
  ];

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Download className="h-5 w-5" />
            Importar de ServiceNow
            {addedCount > 0 && (
              <Badge variant="secondary" className="ml-2 text-xs">
                {addedCount} adicionada(s) a comparações
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription>
            Extraia dados de CHGs e adicione a comparações. Feche quando terminar.
          </DialogDescription>
        </DialogHeader>

        {/* Tab bar manual */}
        <div className="flex border-b border-border gap-0">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              disabled={tab.id === "playwright" && !playwrightStatus.configured}
              className={`flex items-center gap-1.5 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors
                ${activeTab === tab.id
                  ? "border-primary text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
                }
                disabled:opacity-40 disabled:cursor-not-allowed`}
            >
              {tab.icon}
              {tab.label}
              {tab.id === "teams" && teamsItems.length > 0 && (
                <Badge variant="secondary" className="ml-0.5 text-[10px] px-1 py-0 h-4">
                  {teamsItems.length}
                </Badge>
              )}
            </button>
          ))}
        </div>

        {/* ── Aba: Teams ─────────────────────────────────────────────── */}
        {activeTab === "teams" && (
          <div className="space-y-3 pt-1">
            <div className="flex items-center justify-between">
              <div className="text-xs text-muted-foreground">
                CHGs do dia atual — mensagens do <strong>Mr.ViaBot</strong>
                {teamsLastUpdated && (
                  <span className="ml-1 opacity-70">· atualizado {formatLastUpdated(teamsLastUpdated)}</span>
                )}
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={handleTeamsRefresh}
                disabled={teamsLoading}
                className="h-7 text-xs"
              >
                {teamsLoading ? (
                  <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />
                ) : (
                  <RefreshCw className="h-3.5 w-3.5 mr-1" />
                )}
                {teamsItems.length === 0 && teamsNeedsRefresh ? "Buscar do Teams" : "Atualizar"}
              </Button>
            </div>

            {teamsLoading && teamsItems.length === 0 && (
              <div className="flex items-center justify-center py-8 text-sm text-muted-foreground gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Aguardando... o Chrome abrirá o Teams automaticamente (~60s)
              </div>
            )}

            {!teamsLoading && teamsNeedsRefresh && teamsItems.length === 0 && (
              <div className="text-center py-6 space-y-2">
                <MessageSquare className="h-8 w-8 mx-auto text-muted-foreground opacity-50" />
                <p className="text-sm text-muted-foreground">
                  Nenhuma CHG carregada ainda
                </p>
                <p className="text-xs text-muted-foreground">
                  Clique em "Buscar do Teams" para abrir o Teams e capturar as mensagens do Mr.ViaBot
                </p>
              </div>
            )}

            {/* Campo de busca por CHG — sempre visível na aba Teams */}
            <div className="flex gap-2">
              <input
                type="text"
                value={chgSearch}
                onChange={e => setChgSearch(e.target.value)}
                onKeyDown={e => e.key === "Enter" && isValidChgInput && !chgInList && !teamsExtracting && handleExtractDirectChg()}
                placeholder="Buscar CHG (ex: CHG0455046)"
                className="flex-1 h-8 px-2.5 text-xs rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
                disabled={!!teamsExtracting}
              />
              {isValidChgInput && !chgInList && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleExtractDirectChg}
                  disabled={!!teamsExtracting}
                  className="h-8 text-xs whitespace-nowrap"
                >
                  Extrair {chgSearchNorm}
                </Button>
              )}
            </div>

            {teamsItems.length > 0 && (
              <>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-xs text-muted-foreground">
                    {selectedTeamsChgs.size > 0
                      ? `${selectedTeamsChgs.size} selecionada${selectedTeamsChgs.size > 1 ? "s" : ""}`
                      : "Selecione uma ou mais CHGs"}
                  </span>
                  {selectedTeamsChgs.size < filteredTeamsItems.length ? (
                    <button
                      onClick={() => setSelectedTeamsChgs(new Set(filteredTeamsItems.map(i => i.chg)))}
                      className="text-xs text-primary hover:underline"
                    >
                      Selecionar todas
                    </button>
                  ) : (
                    <button
                      onClick={() => setSelectedTeamsChgs(new Set())}
                      className="text-xs text-muted-foreground hover:underline"
                    >
                      Limpar seleção
                    </button>
                  )}
                </div>
                <div className="space-y-1.5 max-h-64 overflow-y-auto pr-1">
                  {filteredTeamsItems.map((item) => {
                    const checked = selectedTeamsChgs.has(item.chg);
                    return (
                      <div
                        key={item.chg}
                        onClick={() => toggleTeamsChg(item.chg)}
                        className={`flex items-center gap-2.5 p-2.5 rounded-md border cursor-pointer transition-colors
                          ${checked ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"}`}
                      >
                        {/* Checkbox */}
                        <div className={`h-4 w-4 rounded border-2 flex-shrink-0 flex items-center justify-center transition-colors
                          ${checked ? "border-primary bg-primary" : "border-muted-foreground"}`}
                        >
                          {checked && <svg className="h-2.5 w-2.5 text-white" fill="none" viewBox="0 0 12 12"><path d="M2 6l3 3 5-5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/></svg>}
                        </div>
                        {/* Conteúdo */}
                        <div className="min-w-0 flex-1">
                          <span className="font-mono text-xs text-muted-foreground">{item.chg}</span>
                          {item.description ? (
                            <p className="text-sm font-medium truncate" title={item.description}>{item.description}</p>
                          ) : (
                            <p className="text-sm text-muted-foreground italic">sem descrição</p>
                          )}
                        </div>
                        {/* Link ServiceNow */}
                        {item.servicenow_url && (
                          <a
                            href={item.servicenow_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={e => e.stopPropagation()}
                            className="text-[10px] text-muted-foreground hover:text-foreground flex-shrink-0"
                            title="Abrir no ServiceNow"
                          >
                            <ExternalLink className="h-3 w-3" />
                          </a>
                        )}
                      </div>
                    );
                  })}
                </div>
                {(selectedTeamsChgs.size > 0 || teamsExtracting) && (
                  <Button
                    onClick={handleImportSelectedTeams}
                    disabled={!!teamsExtracting}
                    className="w-full mt-2"
                    size="lg"
                  >
                    {teamsExtracting ? (
                      <>
                        <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                        Extraindo {teamsExtracting.chg}... ({teamsExtracting.current}/{teamsExtracting.total})
                      </>
                    ) : (
                      <>
                        <Download className="h-4 w-4 mr-2" />
                        Importar {selectedTeamsChgs.size} CHG{selectedTeamsChgs.size > 1 ? "s" : ""} para comparações
                      </>
                    )}
                  </Button>
                )}
              </>
            )}
          </div>
        )}

        {/* ── Aba: Via Browser ───────────────────────────────────────── */}
        {activeTab === "playwright" && (
          <div className="space-y-4 pt-1">
            {pendingTeamsItem && (
              <div className="flex items-center gap-2 p-2 rounded-md bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 text-xs text-blue-800 dark:text-blue-200">
                <MessageSquare className="h-3.5 w-3.5 flex-shrink-0" />
                CHG do Teams selecionada: <strong>{pendingTeamsItem.chg}</strong>
                <button
                  className="ml-auto text-blue-500 hover:text-blue-700"
                  onClick={() => { setPendingTeamsItem(null); setChgUrl(""); }}
                >
                  ×
                </button>
              </div>
            )}
            <div className="space-y-2">
              <Label htmlFor="chg-url-playwright">URL da CHG</Label>
              <Input
                id="chg-url-playwright"
                placeholder="https://viavarejo.service-now.com/change_request.do?sys_id=..."
                value={chgUrl}
                onChange={(e) => setChgUrl(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !isLoading && chgUrl.trim()) handleExtractWithPlaywright();
                }}
                disabled={isLoading}
              />
            </div>

            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-3">
              <div className="text-sm text-blue-800 dark:text-blue-200">
                <p className="font-medium">Como funciona:</p>
                <ol className="list-decimal list-inside mt-1 space-y-1 text-blue-700 dark:text-blue-300">
                  {forceWindowsBrowser || playwrightStatus.wslMode ? (
                    <>
                      <li>O sistema abre o Chrome/Edge <strong>do Windows</strong> para autenticação</li>
                      <li>Faça login no Azure AD se solicitado</li>
                      <li>Os dados serão extraídos automaticamente</li>
                    </>
                  ) : (
                    <>
                      <li>O sistema abre um navegador Chromium</li>
                      <li>Se necessário, faça login no Azure AD</li>
                      <li>Os dados serão extraídos automaticamente</li>
                    </>
                  )}
                </ol>
              </div>
            </div>

            <Button
              onClick={handleExtractWithPlaywright}
              disabled={isLoading || !chgUrl.trim()}
              className="w-full"
              size="lg"
            >
              {isLoading ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Extraindo... (aguarde login se necessário)
                </>
              ) : (
                <>
                  <Globe className="h-4 w-4 mr-2" />
                  Extrair via Browser
                </>
              )}
            </Button>
          </div>
        )}

        {/* ── Aba: Texto Manual ──────────────────────────────────────── */}
        {activeTab === "manual" && (
          <div className="space-y-4 pt-1">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="rounded-full">1</Badge>
                <Label className="font-medium">Abrir a CHG no ServiceNow</Label>
              </div>
              <div className="flex gap-2">
                <Input
                  placeholder="Cole a URL da CHG (opcional)"
                  value={chgUrl}
                  onChange={(e) => setChgUrl(e.target.value)}
                  className="flex-1"
                />
                <Button variant="outline" onClick={handleOpenServiceNow}>
                  <ExternalLink className="h-4 w-4 mr-2" />
                  Abrir
                </Button>
              </div>
            </div>

            <Separator />

            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="rounded-full">2</Badge>
                <Label className="font-medium">Copiar o campo "Motivo da mudança"</Label>
              </div>
              <Alert>
                <Copy className="h-4 w-4" />
                <AlertDescription>
                  Na página da CHG, localize o campo <strong>"Motivo da mudança"</strong> e copie todo o conteúdo (Ctrl+A, Ctrl+C).
                </AlertDescription>
              </Alert>
            </div>

            <Separator />

            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Badge variant="outline" className="rounded-full">3</Badge>
                  <Label className="font-medium">Colar o texto aqui</Label>
                </div>
                <Button variant="ghost" size="sm" onClick={handlePasteFromClipboard}>
                  <ClipboardPaste className="h-4 w-4 mr-1" />
                  Colar
                </Button>
              </div>
              <Textarea
                placeholder="Cole aqui o conteúdo do campo 'Motivo da mudança'..."
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                disabled={isLoading}
                rows={8}
                className="font-mono text-xs"
              />
            </div>

            <Button
              onClick={handleParseDescription}
              disabled={isLoading || !description.trim()}
              className="w-full"
              size="lg"
            >
              {isLoading ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Extraindo dados...
                </>
              ) : (
                <>
                  <FileText className="h-4 w-4 mr-2" />
                  Extrair Dados da CHG
                </>
              )}
            </Button>
          </div>
        )}

        {/* Resultado (todas as abas) */}
        {result && result.success && result.extracted_data && (
          <>
            <Separator />

            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="font-medium text-sm flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 text-green-500" />
                  Dados Extraídos
                  {pendingTeamsItem && (
                    <Badge variant="outline" className="text-[10px] bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-300 border-blue-200">
                      + URL de aprovação SRE
                    </Badge>
                  )}
                </span>
                <Badge className={getConfidenceColor(result.extracted_data.confidence)}>
                  Confiança: {getConfidenceText(result.extracted_data.confidence)}
                </Badge>
              </div>

              <div className="grid grid-cols-2 gap-3 text-sm bg-muted p-4 rounded-lg">
                {result.change_request?.number && (
                  <>
                    <div className="flex items-center gap-2">
                      <FileText className="h-4 w-4 text-muted-foreground" />
                      <span className="text-muted-foreground">Número:</span>
                    </div>
                    <span className="font-mono font-medium text-primary">
                      {result.change_request.number}
                    </span>
                  </>
                )}

                <div className="flex items-center gap-2">
                  <Package className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Aplicação:</span>
                </div>
                <span className="font-mono truncate font-medium">
                  {result.extracted_data.application || "-"}
                </span>

                <div className="flex items-center gap-2">
                  <GitBranch className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Versão:</span>
                </div>
                <span className="font-mono font-medium">
                  {result.extracted_data.version || "-"}
                </span>

                <div className="flex items-center gap-2">
                  <GitBranch className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Repositório:</span>
                </div>
                <span className="font-mono truncate font-medium">
                  {result.extracted_data.github_repo || "-"}
                </span>

                <div className="flex items-center gap-2">
                  <Users className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Squad:</span>
                </div>
                <span className="truncate font-medium">
                  {result.extracted_data.squad || "-"}
                </span>
              </div>

              {result.extracted_data.jira_issues && result.extracted_data.jira_issues.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {result.extracted_data.jira_issues.map((issue, idx) => (
                    <Badge key={idx} variant="secondary" className="text-xs">
                      {issue}
                    </Badge>
                  ))}
                </div>
              )}

              {result.extracted_data.xlrelease_url && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={() => window.open(result.extracted_data!.xlrelease_url, "_blank")}
                >
                  <ExternalLink className="h-4 w-4 mr-2" />
                  Abrir no XL Release
                </Button>
              )}

              {(!result.extracted_data.application || !result.extracted_data.version) && (
                <Alert>
                  <AlertCircle className="h-4 w-4" />
                  <AlertDescription>
                    Alguns campos não foram encontrados. Você precisará preenchê-los manualmente.
                  </AlertDescription>
                </Alert>
              )}

              <div className="flex gap-2 justify-end">
                <Button variant="outline" onClick={handleClose}>
                  Fechar
                </Button>
                <Button
                  onClick={handleUseData}
                  disabled={!result.extracted_data?.application && !result.extracted_data?.github_repo}
                >
                  <Plus className="h-4 w-4 mr-2" />
                  Adicionar a Comparações
                </Button>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
