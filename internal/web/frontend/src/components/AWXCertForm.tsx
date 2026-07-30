import { useState, useEffect, useRef, useMemo } from "react";
import { Loader2, CheckCircle2, XCircle, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { AWXCertificate } from "@/lib/api/types";

export interface AWXCertFormProps {
  cluster: string;
  namespace: string;
  onCancel?: () => void;
  onSuccess?: () => void;
  /** Chamado antes de lançar o job AWX — usado por CertificateRenewModal.tsx (Fase 2 do
   *  CERT-ROLLBACK-VALIDATION-PLAN.md) para salvar um backup do Secret atual antes da renovação,
   *  já que a mutação via AWX acontece fora do controle deste backend (o playbook Ansible mexe no
   *  Secret direto, sem passar por Scanner.UploadCertificate). Melhor-esforço: uma falha aqui é só
   *  avisada, nunca bloqueia o lançamento do job. */
  onBeforeLaunch?: () => Promise<void>;
}

type JobStatus = "idle" | "running" | "successful" | "failed";

interface SSEEvent {
  type: "log" | "status" | "complete" | "error";
  data: string;
}

export function AWXCertForm({ cluster, namespace, onCancel, onSuccess, onBeforeLaunch }: AWXCertFormProps) {
  const appName = (() => {
    let name = cluster;
    name = name.replace(/-admin$/i, "");
    name = name.replace(/-(?:hlg|prd|prod|dev|stg|hom|uat|qa)$/i, "");
    name = name.replace(/^aks(?:priv|pub)?-/i, "");
    return name;
  })();

  const [subsEnv, setSubsEnv] = useState("");
  const [resourceGroup, setResourceGroup] = useState("");
  const [infoLoading, setInfoLoading] = useState(false);

  const [certs, setCerts] = useState<AWXCertificate[]>([]);
  const [certsLoading, setCertsLoading] = useState(false);
  const [certSearch, setCertSearch] = useState("");
  const [certTLS, setCertTLS] = useState("");

  const [jobStatus, setJobStatus] = useState<JobStatus>("idle");
  const [logs, setLogs] = useState<string[]>([]);
  const [currentStatus, setCurrentStatus] = useState("");

  const logsEndRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    setSubsEnv("");
    setResourceGroup("");
    setCertTLS("");
    setLogs([]);
    setJobStatus("idle");
    setCurrentStatus("");

    if (!cluster) return;

    setInfoLoading(true);
    apiClient.getAWXClusterInfo(cluster)
      .then((info) => {
        setResourceGroup(info.resource_group);
        setSubsEnv(info.subs_env);
      })
      .catch(() => {})
      .finally(() => setInfoLoading(false));

    setCertsLoading(true);
    apiClient.getAWXTemplateSurvey(25)
      .then((choices) => setCerts(choices.map((name, i) => ({ id: i, name }))))
      .catch((err) => toast.error("Erro ao carregar certificados: " + err.message))
      .finally(() => setCertsLoading(false));

    return () => {
      eventSourceRef.current?.close();
      eventSourceRef.current = null;
    };
  }, [cluster]);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs]);

  const filteredCerts = useMemo(() => {
    if (!certSearch.trim()) return certs;
    const q = certSearch.toLowerCase();
    return certs.filter((c) => c.name.toLowerCase().includes(q));
  }, [certs, certSearch]);

  function resetJob() {
    setJobStatus("idle");
    setLogs([]);
    setCurrentStatus("");
    eventSourceRef.current?.close();
    eventSourceRef.current = null;
  }

  async function launchJob(templateId: number) {
    if (!subsEnv.trim() || !resourceGroup.trim() || !certTLS) {
      toast.error("Preencha todos os campos antes de lançar o job.");
      return;
    }

    resetJob();
    setJobStatus("running");

    if (onBeforeLaunch) {
      try {
        await onBeforeLaunch();
      } catch (err) {
        // Melhor-esforço — falha no backup nunca deve impedir a renovação em si.
        toast.warning("Não foi possível salvar backup do certificado atual antes da renovação", {
          description: err instanceof Error ? err.message : "Erro desconhecido",
        });
      }
    }

    try {
      const resp = await apiClient.launchAWXCertJob({
        template_id: templateId,
        app_name: appName,
        subs_env: subsEnv.trim(),
        cluster_resource_group_name: resourceGroup.trim(),
        namespace,
        cert_tls: certTLS,
      });

      const es = new EventSource(apiClient.getAWXJobStreamURL(resp.job_id));
      eventSourceRef.current = es;

      // `finished` (variável local, não estado React) resolve um bug real: `es.onerror` abaixo
      // rodava dentro do mesmo closure de `launchJob`, então `jobStatus` ali sempre refletia o
      // valor de ANTES do clique (setState é assíncrono, não muda o binding já capturado) — a
      // checagem `jobStatus === "running"` era efetivamente sempre falsa, então uma queda de
      // conexão SSE (proxy corporativo, rede instável, servidor reiniciando) nunca disparava toast
      // nem tirava a UI do estado "running": o job ficava girando "Aguardando output..." pra
      // sempre, sem nenhum aviso — a falha silenciosa relatada. `finished` é reatribuído pelos
      // próprios handlers abaixo, no mesmo escopo, então reflete o estado real na hora do onerror.
      let finished = false;

      es.onmessage = (event) => {
        try {
          const parsed: SSEEvent = JSON.parse(event.data);
          if (parsed.type === "log") {
            setLogs((prev) => [...prev, parsed.data]);
          } else if (parsed.type === "status") {
            setCurrentStatus(parsed.data);
          } else if (parsed.type === "complete") {
            finished = true;
            setJobStatus("successful");
            setCurrentStatus("successful");
            es.close();
            toast.success("Certificado instalado/atualizado com sucesso!");
            onSuccess?.();
          } else if (parsed.type === "error") {
            finished = true;
            setJobStatus("failed");
            setCurrentStatus(parsed.data);
            setLogs((prev) => [...prev, `[ERRO] ${parsed.data}`]);
            es.close();
            toast.error("Job AWX falhou: " + parsed.data);
          }
        } catch {
          // linha sem JSON válido
        }
      };

      es.onerror = () => {
        if (!finished) {
          finished = true;
          setJobStatus("failed");
          toast.error("Conexão com o job AWX foi perdida antes de terminar — verifique manualmente no AWX se o job concluiu.");
        }
        es.close();
      };
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err);
      setJobStatus("failed");
      toast.error("Erro ao lançar job: " + message);
    }
  }

  const isRunning = jobStatus === "running";
  const canSubmit = !isRunning && subsEnv.trim() !== "" && resourceGroup.trim() !== "" && certTLS !== "";

  return (
    <div className="space-y-4">
      {/* Campos readonly */}
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">app_name</Label>
          <Input value={appName} readOnly className="bg-muted text-sm" />
        </div>
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">namespace</Label>
          <Input value={namespace} readOnly className="bg-muted text-sm" />
        </div>
      </div>

      {/* Campos auto-preenchidos (editáveis) */}
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <Label htmlFor="awx-subs-env" className="text-xs">
            subs_env <span className="text-red-500">*</span>
          </Label>
          <div className="relative">
            <Input
              id="awx-subs-env"
              placeholder={infoLoading ? "Carregando..." : "ex: DEV_HLG_ONLINE"}
              value={subsEnv}
              onChange={(e) => setSubsEnv(e.target.value)}
              disabled={isRunning || infoLoading}
              className="text-sm"
            />
            {infoLoading && (
              <Loader2 className="absolute right-2 top-2.5 h-3 w-3 animate-spin text-muted-foreground" />
            )}
          </div>
        </div>
        <div className="space-y-1">
          <Label htmlFor="awx-resource-group" className="text-xs">
            cluster_resource_group_name <span className="text-red-500">*</span>
          </Label>
          <div className="relative">
            <Input
              id="awx-resource-group"
              placeholder={infoLoading ? "Carregando..." : "ex: rg-app-hlg"}
              value={resourceGroup}
              onChange={(e) => setResourceGroup(e.target.value)}
              disabled={isRunning || infoLoading}
              className="text-sm"
            />
            {infoLoading && (
              <Loader2 className="absolute right-2 top-2.5 h-3 w-3 animate-spin text-muted-foreground" />
            )}
          </div>
        </div>
      </div>

      {/* Seleção de certificado */}
      <div className="space-y-1">
        <Label className="text-xs">
          Certificado TLS <span className="text-red-500">*</span>
        </Label>
        {certsLoading ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin" />
            Carregando credenciais do AWX...
          </div>
        ) : (
          <div className="space-y-1.5">
            {certs.length > 5 && (
              <div className="relative">
                <Search className="absolute left-2 top-2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Filtrar certificados..."
                  value={certSearch}
                  onChange={(e) => setCertSearch(e.target.value)}
                  className="pl-8 h-8 text-sm"
                  disabled={isRunning}
                />
              </div>
            )}
            <Select value={certTLS} onValueChange={setCertTLS} disabled={isRunning}>
              <SelectTrigger className="text-sm">
                <SelectValue placeholder={
                  certs.length === 0
                    ? "Nenhuma credencial encontrada"
                    : filteredCerts.length === 0
                    ? "Nenhum resultado para a busca"
                    : "Selecione um certificado..."
                } />
              </SelectTrigger>
              <SelectContent>
                {filteredCerts.length === 0 ? (
                  <SelectItem value="__none__" disabled>
                    {certs.length === 0 ? "Nenhuma credencial no AWX" : "Nenhum resultado"}
                  </SelectItem>
                ) : (
                  filteredCerts.map((c) => (
                    <SelectItem key={c.id} value={c.name}>
                      {c.name}
                    </SelectItem>
                  ))
                )}
              </SelectContent>
            </Select>
            {certs.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {filteredCerts.length} de {certs.length} credencial(is)
              </p>
            )}
          </div>
        )}
      </div>

      {/* Botões de ação */}
      <div className="flex items-center justify-between pt-1">
        <Button variant="outline" onClick={onCancel} disabled={isRunning}>
          Cancelar
        </Button>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => launchJob(25)}
            disabled={!canSubmit}
            className="border-blue-500 text-blue-600 hover:bg-blue-50"
          >
            {isRunning && <Loader2 className="w-4 h-4 animate-spin mr-1" />}
            Instalar (Job 25)
          </Button>
          <Button onClick={() => launchJob(26)} disabled={!canSubmit}>
            {isRunning && <Loader2 className="w-4 h-4 animate-spin mr-1" />}
            Atualizar (Job 26)
          </Button>
        </div>
      </div>

      {/* Painel de logs */}
      {(logs.length > 0 || isRunning || jobStatus !== "idle") && (
        <div className="space-y-2 pt-1">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium text-muted-foreground">Logs do Job AWX</span>
            {currentStatus && (
              <Badge
                variant={
                  jobStatus === "successful" ? "default" :
                  jobStatus === "failed" ? "destructive" :
                  "secondary"
                }
                className="text-xs"
              >
                {currentStatus}
              </Badge>
            )}
            {jobStatus === "successful" && <CheckCircle2 className="w-4 h-4 text-green-500" />}
            {jobStatus === "failed" && <XCircle className="w-4 h-4 text-red-500" />}
          </div>
          <ScrollArea className="h-40 rounded border bg-black/80 p-3">
            <div className="font-mono text-xs text-green-300 space-y-0.5">
              {logs.map((line, i) => (
                <div key={i}>{line}</div>
              ))}
              {isRunning && (
                <div className="flex items-center gap-1 text-yellow-400">
                  <Loader2 className="w-3 h-3 animate-spin" />
                  Aguardando output do job...
                </div>
              )}
              <div ref={logsEndRef} />
            </div>
          </ScrollArea>
        </div>
      )}
    </div>
  );
}
