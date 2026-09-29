import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Check,
  ChevronsUpDown,
  ClipboardPaste,
  Copy,
  Eye,
  Loader2,
  Plus,
  Send,
  Trash2,
  Wand2,
} from "lucide-react";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { ProtectedAction } from "@/components/rbac";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useClusters } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { formatBytes } from "@/lib/monitorUtils";
import { cn } from "@/lib/utils";
import type { HttpClientExecutionMode, HttpClientHeader, HttpClientResponse } from "@/lib/api/types";

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];

// Combobox com busca embutida no mesmo popover (o <Select> do Radix fecha ao focar busca externa).
function SearchableSelect({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: string[];
  placeholder: string;
  searchPlaceholder: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" role="combobox" aria-expanded={open} disabled={disabled} className="w-full justify-between font-normal">
          <span className="truncate">{value || placeholder}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>Nada encontrado.</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt}
                  value={opt}
                  onSelect={() => {
                    onChange(opt === value ? "" : opt);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === opt ? "opacity-100" : "opacity-0")} />
                  {opt}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function statusColor(status?: number) {
  if (!status) return "bg-red-500/10 text-red-500 border-red-500/30";
  if (status < 300) return "bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/30";
  if (status < 400) return "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30";
  if (status < 500) return "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30";
  return "bg-red-500/10 text-red-500 border-red-500/30";
}

// Indenta o corpo quando é JSON válido; senão devolve como veio.
function prettyBody(body: string): { text: string; isJson: boolean } {
  const trimmed = body.trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return { text: body, isJson: false };
  try {
    return { text: JSON.stringify(JSON.parse(trimmed), null, 2), isJson: true };
  } catch {
    return { text: body, isJson: false };
  }
}

function copyText(text: string, label: string) {
  navigator.clipboard
    .writeText(text)
    .then(() => toast.success(`${label} copiado!`))
    .catch(() => toast.error("Não foi possível copiar"));
}

export default function HttpClientTab() {
  const { clusters } = useClusters();

  // Onde a requisição é executada.
  const [executionMode, setExecutionMode] = useState<HttpClientExecutionMode>("server");
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [deployment, setDeployment] = useState("");
  const [podName, setPodName] = useState("");
  const [containerName, setContainerName] = useState("");

  // Requisição.
  const [method, setMethod] = useState("GET");
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState<HttpClientHeader[]>([{ key: "", value: "" }]);
  const [body, setBody] = useState("");
  const [followRedirects, setFollowRedirects] = useState(true);
  const [insecure, setInsecure] = useState(false);
  const [timeoutMs, setTimeoutMs] = useState(30000);
  const [requestTab, setRequestTab] = useState<"body" | "headers" | "options">("body");

  // cURL.
  const [curlOpen, setCurlOpen] = useState(false);
  const [curlText, setCurlText] = useState("");
  const [curlParsing, setCurlParsing] = useState(false);

  // Resposta.
  const [sending, setSending] = useState(false);
  const [response, setResponse] = useState<HttpClientResponse | null>(null);
  const [sendError, setSendError] = useState<string | null>(null);
  const [responseTab, setResponseTab] = useState<"body" | "headers">("body");
  const [responseOpen, setResponseOpen] = useState(false);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["http-client-namespaces", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: executionMode === "pod" && !!cluster,
  });
  const { data: deployments = [] } = useQuery({
    queryKey: ["http-client-deployments", cluster, namespace],
    queryFn: () => apiClient.getDeployments(cluster, [namespace]),
    enabled: executionMode === "pod" && !!cluster && !!namespace,
  });
  const { data: podsResponse } = useQuery({
    queryKey: ["http-client-pods", cluster, namespace, deployment],
    queryFn: () => apiClient.getHttpClientPods(cluster, namespace, deployment),
    enabled: executionMode === "pod" && !!cluster && !!namespace && !!deployment,
  });
  const podOptions = podsResponse?.pods ?? [];
  const selectedPod = podOptions.find((p) => p.name === podName);

  const filledHeaders = headers.filter((h) => h.key.trim());
  const bodyAllowed = method !== "GET" && method !== "HEAD";
  const canSend =
    !!url.trim() && !sending && (executionMode === "server" || (!!cluster && !!namespace && !!deployment));

  const send = async () => {
    if (!canSend) return;
    setSending(true);
    setSendError(null);
    setResponse(null);
    try {
      const resp = await apiClient.sendHttpClientRequest({
        execution_mode: executionMode,
        ...(executionMode === "pod"
          ? { cluster, namespace, deployment, pod_name: podName || undefined, container_name: containerName || undefined }
          : {}),
        method,
        url: url.trim(),
        headers: filledHeaders,
        body: bodyAllowed ? body : "",
        follow_redirects: followRedirects,
        insecure_skip_verify: insecure,
        timeout_ms: timeoutMs,
      });
      setResponse(resp);
      setResponseTab("body");
      // Abre o modal direto quando houve resposta HTTP; erro sem resposta fica só no resumo.
      setResponseOpen(!!resp.status);
    } catch (err) {
      setSendError(err instanceof Error ? err.message : "Falha ao enviar a requisição");
    } finally {
      setSending(false);
    }
  };

  const importCurl = async () => {
    if (!curlText.trim()) return;
    setCurlParsing(true);
    try {
      const parsed = await apiClient.parseHttpClientCurl(curlText);
      setMethod(METHODS.includes(parsed.method) ? parsed.method : "GET");
      setUrl(parsed.url);
      setHeaders(parsed.headers.length > 0 ? parsed.headers : [{ key: "", value: "" }]);
      setBody(parsed.body);
      setFollowRedirects(parsed.follow_redirects);
      setInsecure(parsed.insecure_skip_verify);
      if (parsed.timeout_ms) setTimeoutMs(Math.min(120000, parsed.timeout_ms));
      setRequestTab(parsed.body ? "body" : "headers");
      setCurlOpen(false);
      setCurlText("");
      if (parsed.warnings?.length) {
        toast.warning(`cURL importado com avisos: ${parsed.warnings.join("; ")}`);
      } else {
        toast.success("cURL importado");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Não foi possível ler o cURL");
    } finally {
      setCurlParsing(false);
    }
  };

  const formatRequestBody = () => {
    try {
      setBody(JSON.stringify(JSON.parse(body), null, 2));
    } catch {
      toast.error("O corpo não é um JSON válido");
    }
  };

  const updateHeader = (i: number, field: keyof HttpClientHeader, value: string) =>
    setHeaders((prev) => prev.map((h, idx) => (idx === i ? { ...h, [field]: value } : h)));

  const pretty = useMemo(() => (response ? prettyBody(response.body) : { text: "", isJson: false }), [response]);

  return (
    <div className="h-full overflow-y-auto">
      <div className="px-6 py-4 border-b border-border flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold">Cliente HTTP</h2>
          <p className="text-xs text-muted-foreground">
            Monte uma requisição (ou cole um cURL) e veja a resposta. Pode sair do servidor da aplicação ou de dentro
            de um pod do cluster.
          </p>
        </div>

        <RadioGroup
          value={executionMode}
          onValueChange={(v) => setExecutionMode(v as HttpClientExecutionMode)}
          className="flex items-center gap-4"
        >
          <div className="flex items-center gap-1.5">
            <RadioGroupItem value="server" id="http-mode-server" />
            <label htmlFor="http-mode-server" className="text-sm cursor-pointer">Pelo servidor</label>
          </div>
          <div className="flex items-center gap-1.5">
            <RadioGroupItem value="pod" id="http-mode-pod" />
            <label htmlFor="http-mode-pod" className="text-sm cursor-pointer">
              De um pod do cluster <span className="text-muted-foreground">(acessa serviços internos, respeita NetworkPolicy/Istio)</span>
            </label>
          </div>
        </RadioGroup>

        {executionMode === "pod" && (
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-[220px]">
              <ClusterSelectorForTab
                selectedCluster={cluster}
                onClusterChange={(v) => {
                  setCluster(v);
                  setNamespace("");
                  setDeployment("");
                  setPodName("");
                  setContainerName("");
                }}
                clusters={clusters.map((c) => c.context)}
                tabLabel="Cliente HTTP"
                clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
                clusterJourneys={Object.fromEntries(clusters.map((c) => [c.context, c.journey || ""]))}
              />
            </div>
            <div className="min-w-[200px]">
              <label className="text-xs text-muted-foreground block mb-1">Namespace</label>
              <SearchableSelect
                value={namespace}
                onChange={(v) => { setNamespace(v); setDeployment(""); setPodName(""); setContainerName(""); }}
                options={namespaces.map((ns) => ns.name)}
                placeholder="Selecione o namespace"
                searchPlaceholder="Buscar namespace..."
                disabled={!cluster}
              />
            </div>
            <div className="min-w-[240px]">
              <label className="text-xs text-muted-foreground block mb-1">Deployment (de onde a requisição sai)</label>
              <SearchableSelect
                value={deployment}
                onChange={(v) => { setDeployment(v); setPodName(""); setContainerName(""); }}
                options={deployments.map((d) => d.name)}
                placeholder="Selecione o deployment"
                searchPlaceholder="Buscar deployment..."
                disabled={!namespace}
              />
            </div>
            <div className="min-w-[220px]">
              <label className="text-xs text-muted-foreground block mb-1">
                Pod <span className="text-muted-foreground/70">(opcional — padrão: primeiro Running)</span>
              </label>
              <SearchableSelect
                value={podName}
                onChange={(v) => { setPodName(v); setContainerName(""); }}
                options={podOptions.map((p) => p.name)}
                placeholder="Automático (primeiro Running)"
                searchPlaceholder="Buscar pod..."
                disabled={!deployment}
              />
            </div>
            {selectedPod && selectedPod.containers.length > 1 && (
              <div className="min-w-[180px]">
                <label className="text-xs text-muted-foreground block mb-1">Container alvo</label>
                <Select value={containerName} onValueChange={setContainerName}>
                  <SelectTrigger>
                    <SelectValue placeholder={selectedPod.containers[0]} />
                  </SelectTrigger>
                  <SelectContent>
                    {selectedPod.containers.map((ct) => (
                      <SelectItem key={ct} value={ct}>{ct}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <p className="w-full text-xs text-muted-foreground">
              A requisição roda com <code>curl</code> num container efêmero anexado ao pod (imagem curlimages/curl).
              Containers efêmeros ficam listados no pod até ele reiniciar.
            </p>
          </div>
        )}
      </div>

      <div className="px-6 py-4 border-b border-border flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="w-32">
            <Select value={method} onValueChange={setMethod}>
              <SelectTrigger className="font-mono">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {METHODS.map((m) => (
                  <SelectItem key={m} value={m} className="font-mono">{m}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Input
            className="flex-1 min-w-[280px] font-mono text-sm"
            placeholder="https://api.exemplo.com/v1/recurso"
            spellCheck={false}
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") send(); }}
          />
          <Button variant="outline" onClick={() => setCurlOpen(true)} className="gap-1.5">
            <ClipboardPaste className="w-4 h-4" />
            Colar cURL
          </Button>
          <ProtectedAction>
            <Button onClick={send} disabled={!canSend} className="gap-1.5">
              {sending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
              Enviar
            </Button>
          </ProtectedAction>
        </div>

        <div className="flex items-center gap-1 border-b border-border">
          {([
            ["body", "Corpo"],
            ["headers", `Headers${filledHeaders.length ? ` (${filledHeaders.length})` : ""}`],
            ["options", "Opções"],
          ] as const).map(([id, label]) => (
            <button
              key={id}
              type="button"
              onClick={() => setRequestTab(id)}
              className={cn(
                "px-3 py-1.5 text-sm border-b-2 -mb-px transition-colors",
                requestTab === id ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              {label}
            </button>
          ))}
        </div>

        {requestTab === "body" && (
          <div className="flex flex-col gap-2">
            {!bodyAllowed && (
              <p className="text-xs text-muted-foreground">{method} não envia corpo — troque o método para POST/PUT/PATCH/DELETE para usar.</p>
            )}
            <Textarea
              className="font-mono text-xs min-h-[200px]"
              spellCheck={false}
              placeholder='{"chave": "valor"}'
              value={body}
              disabled={!bodyAllowed}
              onChange={(e) => setBody(e.target.value)}
            />
            <div>
              <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={formatRequestBody} disabled={!bodyAllowed || !body.trim()}>
                <Wand2 className="w-3.5 h-3.5" />
                Formatar JSON
              </Button>
            </div>
          </div>
        )}

        {requestTab === "headers" && (
          <div className="flex flex-col gap-2">
            {headers.map((h, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input className="font-mono text-xs w-64" placeholder="Header" value={h.key} onChange={(e) => updateHeader(i, "key", e.target.value)} />
                <Input className="font-mono text-xs flex-1" placeholder="Valor" value={h.value} onChange={(e) => updateHeader(i, "value", e.target.value)} />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  title="Remover header"
                  onClick={() => setHeaders((prev) => (prev.length > 1 ? prev.filter((_, idx) => idx !== i) : [{ key: "", value: "" }]))}
                >
                  <Trash2 className="w-4 h-4" />
                </Button>
              </div>
            ))}
            <div>
              <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => setHeaders((prev) => [...prev, { key: "", value: "" }])}>
                <Plus className="w-3.5 h-3.5" />
                Adicionar header
              </Button>
            </div>
          </div>
        )}

        {requestTab === "options" && (
          <div className="flex flex-wrap items-center gap-6">
            <div className="flex items-center gap-2">
              <Checkbox id="http-follow" checked={followRedirects} onCheckedChange={(v) => setFollowRedirects(!!v)} />
              <label htmlFor="http-follow" className="text-sm cursor-pointer">Seguir redirects</label>
            </div>
            <div className="flex items-center gap-2">
              <Checkbox id="http-insecure" checked={insecure} onCheckedChange={(v) => setInsecure(!!v)} />
              <label htmlFor="http-insecure" className="text-sm cursor-pointer">Ignorar certificado TLS (não recomendado)</label>
            </div>
            <div className="flex items-center gap-2">
              <label className="text-sm text-muted-foreground">Timeout (ms)</label>
              <Input
                type="number"
                className="w-28"
                min={1000}
                max={120000}
                value={timeoutMs}
                onChange={(e) => setTimeoutMs(Math.min(120000, Math.max(1000, Number(e.target.value) || 1000)))}
              />
            </div>
          </div>
        )}
      </div>

      <div className="px-6 py-4 flex flex-col gap-3">
        {sending && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin" />
            {executionMode === "pod" ? "Enviando a partir do pod (pode levar alguns segundos para anexar o container)..." : "Enviando..."}
          </div>
        )}

        {sendError && (
          <div className="rounded-md border border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400 p-3 text-sm">{sendError}</div>
        )}

        {!response && !sending && !sendError && (
          <p className="text-sm text-muted-foreground">Envie uma requisição para ver a resposta aqui.</p>
        )}

        {response && (
          <>
            <div className="flex flex-wrap items-center gap-3 text-sm">
              <Badge variant="outline" className={cn("font-mono", statusColor(response.error ? undefined : response.status))}>
                {response.error && !response.status ? "Sem resposta" : `${response.status} ${response.status_text ?? ""}`}
              </Badge>
              <span className="text-muted-foreground">Tempo: <span className="text-foreground font-mono">{response.duration_ms} ms</span></span>
              <span className="text-muted-foreground">Tamanho: <span className="text-foreground font-mono">{formatBytes(response.size_bytes)}</span></span>
              <span className="text-muted-foreground">Origem: <span className="text-foreground">{response.executed_from}</span></span>
            </div>

            {response.error && (
              <div className="rounded-md border border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400 p-3 text-sm font-mono whitespace-pre-wrap break-all">
                {response.error}
              </div>
            )}

            {(response.status ?? 0) > 0 && (
              <div>
                <Button type="button" variant="outline" className="gap-1.5" onClick={() => setResponseOpen(true)}>
                  <Eye className="w-4 h-4" />
                  Ver resposta
                </Button>
              </div>
            )}
          </>
        )}
      </div>

      {/* Altura FIXA (h-[85vh], não só max-h): a área de conteúdo usa flex-1 min-h-0 + overflow-auto,
          que só rola quando o pai tem altura definida. Abas manuais (shadcn <Tabs> quebra a cadeia flex). */}
      <Dialog open={responseOpen && !!response} onOpenChange={setResponseOpen}>
        <DialogContent className="max-w-5xl h-[85vh] flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle className="flex flex-wrap items-center gap-3">
              Resposta
              {response && (
                <>
                  <Badge variant="outline" className={cn("font-mono", statusColor(response.status))}>
                    {response.status} {response.status_text ?? ""}
                  </Badge>
                  <span className="text-xs font-normal text-muted-foreground">
                    {response.duration_ms} ms · {formatBytes(response.size_bytes)} · {response.executed_from}
                  </span>
                </>
              )}
            </DialogTitle>
          </DialogHeader>

          {response && (
            <>
              <div className="flex items-center justify-between border-b border-border">
                <div className="flex items-center gap-1">
                  {([
                    ["body", "Corpo"],
                    ["headers", `Headers (${response.headers?.length ?? 0})`],
                  ] as const).map(([id, label]) => (
                    <button
                      key={id}
                      type="button"
                      onClick={() => setResponseTab(id)}
                      className={cn(
                        "px-3 py-1.5 text-sm border-b-2 -mb-px transition-colors",
                        responseTab === id ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
                      )}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                {responseTab === "body" && !response.body_is_binary && response.body !== "" && (
                  <Button type="button" variant="outline" size="sm" className="h-7 gap-1.5 mb-1" onClick={() => copyText(pretty.text, "Corpo")}>
                    <Copy className="w-3.5 h-3.5" />
                    Copiar corpo
                  </Button>
                )}
                {responseTab === "headers" && (response.headers?.length ?? 0) > 0 && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1.5 mb-1"
                    onClick={() => copyText((response.headers ?? []).map((h) => `${h.key}: ${h.value}`).join("\n"), "Headers")}
                  >
                    <Copy className="w-3.5 h-3.5" />
                    Copiar headers
                  </Button>
                )}
              </div>

              {responseTab === "body" && response.truncated && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  Resposta grande: exibindo só os primeiros 5 MB de {formatBytes(response.size_bytes)}.
                </p>
              )}

              <div className="flex-1 min-h-0 overflow-auto rounded-md border border-border bg-muted/30">
                {responseTab === "body" &&
                  (response.body_is_binary ? (
                    <p className="p-3 text-sm text-muted-foreground">
                      Resposta binária ({formatBytes(response.size_bytes)}) — não é possível exibir como texto.
                    </p>
                  ) : response.body === "" ? (
                    <p className="p-3 text-sm text-muted-foreground">Resposta sem corpo.</p>
                  ) : (
                    <pre className="text-xs font-mono whitespace-pre-wrap break-all p-3">{pretty.text}</pre>
                  ))}

                {responseTab === "headers" && (
                  <div className="divide-y divide-border">
                    {(response.headers ?? []).map((h, i) => (
                      <div key={i} className="flex gap-3 px-3 py-1.5 text-xs font-mono">
                        <span className="text-muted-foreground shrink-0 w-64 truncate" title={h.key}>{h.key}</span>
                        <span className="break-all">{h.value}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={curlOpen} onOpenChange={setCurlOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Colar cURL</DialogTitle>
          </DialogHeader>
          <p className="text-xs text-muted-foreground">
            Cole um comando cURL (ex.: "Copy as cURL" do Postman ou do navegador). Método, URL, headers e corpo são
            preenchidos automaticamente.
          </p>
          <Textarea
            className="font-mono text-xs min-h-[260px]"
            spellCheck={false}
            placeholder={"curl --location 'https://api.exemplo.com/v1/recurso' \\\n--header 'Content-Type: application/json' \\\n--data '{\"chave\": \"valor\"}'"}
            value={curlText}
            onChange={(e) => setCurlText(e.target.value)}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setCurlOpen(false)}>Cancelar</Button>
            <Button onClick={importCurl} disabled={!curlText.trim() || curlParsing} className="gap-1.5">
              {curlParsing && <Loader2 className="w-4 h-4 animate-spin" />}
              Importar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
