import { useEffect, useRef, useState } from "react";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Loader2,
  Waypoints,
  Play,
  XCircle,
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
} from "lucide-react";
import { ProtectedAction } from "@/components/rbac";
import { useClusters } from "@/hooks/useAPI";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type { KafkaTestResult, KafkaTestSSEEvent, KafkaStageStatus } from "@/lib/api/types";

function StageBadge({ label, status }: { label: string; status: "ok" | "failed" | "skipped" }) {
  const meta = {
    ok: { color: "bg-green-500/10 text-green-500 border-green-500/30", icon: <CheckCircle2 className="w-3 h-3" /> },
    failed: { color: "bg-red-500/10 text-red-500 border-red-500/30", icon: <XCircle className="w-3 h-3" /> },
    skipped: { color: "bg-muted text-muted-foreground border-border", icon: null },
  }[status];
  return (
    <Badge variant="outline" className={`gap-1 ${meta.color}`}>
      {meta.icon}
      {label}
    </Badge>
  );
}

// Deriva os badges de TCP/DNS, Autenticação (só se SASL configurado) e Protocolo Kafka a partir
// da classificação única do estágio de conectividade (ver kafka_test_tool.go).
function deriveConnectivityBadges(status: KafkaStageStatus, saslEnabled: boolean) {
  const badges: { label: string; status: "ok" | "failed" | "skipped" }[] = [];
  badges.push({ label: "TCP/DNS", status: status === "tcp_failed" ? "failed" : "ok" });
  if (saslEnabled) {
    badges.push({
      label: "Autenticação",
      status: status === "tcp_failed" ? "skipped" : status === "auth_failed" ? "failed" : "ok",
    });
  }
  badges.push({ label: "Protocolo Kafka", status: status === "ok" ? "ok" : "failed" });
  return badges;
}

export default function KafkaTestTab() {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [deployment, setDeployment] = useState("");
  const [broker, setBroker] = useState("");

  const [saslEnabled, setSaslEnabled] = useState(false);
  const [mechanism, setMechanism] = useState<"PLAIN" | "SCRAM-SHA-256" | "SCRAM-SHA-512">("PLAIN");
  const [useTLS, setUseTLS] = useState(false);
  const [skipTLSVerify, setSkipTLSVerify] = useState(false);
  const [credSource, setCredSource] = useState<"manual" | "secret">("manual");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [secretNamespace, setSecretNamespace] = useState("");
  const [secretName, setSecretName] = useState("");
  const [usernameKey, setUsernameKey] = useState("username");
  const [passwordKey, setPasswordKey] = useState("password");

  const [produceConsumeEnabled, setProduceConsumeEnabled] = useState(false);
  const [topic, setTopic] = useState("");
  const [confirmProduce, setConfirmProduce] = useState(false);

  const [timeoutMs, setTimeoutMs] = useState(5000);

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState(0);
  const [phaseMessage, setPhaseMessage] = useState("");
  const [result, setResult] = useState<KafkaTestResult | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [rawOutputOpen, setRawOutputOpen] = useState(false);
  const esRef = useRef<EventSource | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["namespaces-kafka-test", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const { data: deployments = [] } = useQuery({
    queryKey: ["deployments-kafka-test", cluster, namespace],
    queryFn: () => apiClient.getDeployments(cluster, [namespace]),
    enabled: !!cluster && !!namespace,
  });

  const canRun =
    !!cluster &&
    !!namespace &&
    !!deployment &&
    !!broker.trim() &&
    !isRunning &&
    (!produceConsumeEnabled || (!!topic.trim() && confirmProduce));

  const runTest = async () => {
    setResult(null);
    setRunError(null);
    setProgress(0);
    setPhaseMessage("Iniciando teste de Kafka...");
    setIsRunning(true);
    try {
      const { session_id } = await apiClient.runKafkaTest({
        cluster,
        namespace,
        deployment,
        broker: broker.trim(),
        sasl: saslEnabled
          ? {
              mechanism,
              use_tls: useTLS,
              skip_tls_verify: skipTLSVerify,
              ...(credSource === "manual"
                ? { username, password }
                : {
                    secret_ref: {
                      namespace: secretNamespace || namespace,
                      name: secretName,
                      username_key: usernameKey || "username",
                      password_key: passwordKey || "password",
                    },
                  }),
            }
          : undefined,
        produce_consume: produceConsumeEnabled,
        topic: produceConsumeEnabled ? topic.trim() : undefined,
        confirm_produce: produceConsumeEnabled ? confirmProduce : false,
        timeout_ms: timeoutMs,
      });
      setSessionId(session_id);
    } catch (err) {
      setIsRunning(false);
      setRunError(err instanceof Error ? err.message : "Falha ao iniciar o teste");
    }
  };

  const cancelTest = async () => {
    if (!sessionId) return;
    try {
      await apiClient.cancelKafkaTest(sessionId);
    } catch {
      /* ignore — o pod é limpo no servidor de qualquer forma */
    }
    esRef.current?.close();
    setIsRunning(false);
    setPhaseMessage("Teste cancelado.");
  };

  useEffect(() => {
    if (!sessionId) return;
    const es = new EventSource(apiClient.getKafkaTestStreamURL(sessionId));
    esRef.current = es;

    es.onmessage = (e) => {
      try {
        const event: KafkaTestSSEEvent = JSON.parse(e.data);
        setPhaseMessage(event.message);
        setProgress(event.progress);

        if (event.type === "complete" && event.result) {
          setResult(event.result);
          setIsRunning(false);
        }
        if (event.type === "error") {
          setRunError(event.error || event.message);
          setIsRunning(false);
        }
      } catch {
        /* ignore evento malformado */
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  return (
    <div className="flex flex-col h-full overflow-y-auto">
      <div className="px-6 py-3 bg-muted/30 border-b border-border flex flex-wrap items-end gap-3">
        <div className="min-w-[220px]">
          <ClusterSelectorForTab
            selectedCluster={cluster}
            onClusterChange={(v) => {
              setCluster(v);
              setNamespace("");
              setDeployment("");
              setResult(null);
            }}
            clusters={clusters.map((c) => c.context)}
            tabLabel="Teste Kafka"
            clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
          />
        </div>

        <div className="min-w-[180px]">
          <label className="text-xs text-muted-foreground block mb-1">Namespace</label>
          <Select value={namespace} onValueChange={(v) => { setNamespace(v); setDeployment(""); }} disabled={!cluster}>
            <SelectTrigger>
              <SelectValue placeholder="Selecione o namespace" />
            </SelectTrigger>
            <SelectContent>
              {namespaces.map((ns) => (
                <SelectItem key={ns.name} value={ns.name}>
                  {ns.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="min-w-[220px]">
          <label className="text-xs text-muted-foreground block mb-1">Deployment (de onde o teste parte)</label>
          <Select value={deployment} onValueChange={setDeployment} disabled={!namespace}>
            <SelectTrigger>
              <SelectValue placeholder="Selecione o deployment" />
            </SelectTrigger>
            <SelectContent>
              {deployments.map((d) => (
                <SelectItem key={d.name} value={d.name}>
                  {d.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="min-w-[280px] flex-1">
          <label className="text-xs text-muted-foreground block mb-1">Broker (host:porta)</label>
          <Input
            placeholder="kfk-mht-prd01.com:9098"
            value={broker}
            onChange={(e) => setBroker(e.target.value)}
          />
        </div>

        <div className="w-28">
          <label className="text-xs text-muted-foreground block mb-1">Timeout (ms)</label>
          <Input
            type="number"
            min={100}
            max={15000}
            value={timeoutMs}
            onChange={(e) => setTimeoutMs(Math.min(15000, Math.max(100, Number(e.target.value) || 100)))}
          />
        </div>

        <ProtectedAction>
          {!isRunning ? (
            <Button onClick={runTest} disabled={!canRun}>
              <Play className="w-4 h-4 mr-2" />
              Executar Teste
            </Button>
          ) : (
            <Button variant="destructive" onClick={cancelTest}>
              <XCircle className="w-4 h-4 mr-2" />
              Cancelar
            </Button>
          )}
        </ProtectedAction>
      </div>

      <div className="px-6 py-3 border-b border-border flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <Switch checked={saslEnabled} onCheckedChange={setSaslEnabled} id="sasl-toggle" />
          <label htmlFor="sasl-toggle" className="text-sm font-medium cursor-pointer">
            Autenticação SASL
          </label>
        </div>

        {saslEnabled && (
          <div className="flex flex-wrap items-end gap-3 pl-8">
            <div className="w-44">
              <label className="text-xs text-muted-foreground block mb-1">Mecanismo</label>
              <Select value={mechanism} onValueChange={(v) => setMechanism(v as typeof mechanism)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="PLAIN">PLAIN</SelectItem>
                  <SelectItem value="SCRAM-SHA-256">SCRAM-SHA-256</SelectItem>
                  <SelectItem value="SCRAM-SHA-512">SCRAM-SHA-512</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-2">
              <Switch checked={useTLS} onCheckedChange={setUseTLS} id="tls-toggle" />
              <label htmlFor="tls-toggle" className="text-sm cursor-pointer">Usar TLS</label>
            </div>

            {useTLS && (
              <div className="flex items-center gap-2">
                <Checkbox checked={skipTLSVerify} onCheckedChange={(v) => setSkipTLSVerify(!!v)} id="skip-tls" />
                <label htmlFor="skip-tls" className="text-sm text-muted-foreground cursor-pointer">
                  Ignorar verificação de certificado (não recomendado)
                </label>
              </div>
            )}

            <div className="w-full">
              <RadioGroup value={credSource} onValueChange={(v) => setCredSource(v as typeof credSource)} className="flex items-center gap-4">
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="manual" id="cred-manual" />
                  <label htmlFor="cred-manual" className="text-sm cursor-pointer">Digitar manualmente</label>
                </div>
                <div className="flex items-center gap-1.5">
                  <RadioGroupItem value="secret" id="cred-secret" />
                  <label htmlFor="cred-secret" className="text-sm cursor-pointer">Ler de um Secret do K8s</label>
                </div>
              </RadioGroup>
            </div>

            {credSource === "manual" ? (
              <>
                <div className="w-56">
                  <label className="text-xs text-muted-foreground block mb-1">Usuário</label>
                  <Input placeholder="ex: $ConnectionString (Azure Event Hub)" value={username} onChange={(e) => setUsername(e.target.value)} />
                </div>
                <div className="w-56">
                  <label className="text-xs text-muted-foreground block mb-1">Senha</label>
                  <Input placeholder="ex: connection string completa (Event Hub)" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
                </div>
              </>
            ) : (
              <>
                <div className="w-44">
                  <label className="text-xs text-muted-foreground block mb-1">Namespace do Secret</label>
                  <Input
                    placeholder={namespace || "(mesmo do teste)"}
                    value={secretNamespace}
                    onChange={(e) => setSecretNamespace(e.target.value)}
                  />
                </div>
                <div className="w-48">
                  <label className="text-xs text-muted-foreground block mb-1">Nome do Secret</label>
                  <Input value={secretName} onChange={(e) => setSecretName(e.target.value)} />
                </div>
                <div className="w-36">
                  <label className="text-xs text-muted-foreground block mb-1">Chave usuário</label>
                  <Input value={usernameKey} onChange={(e) => setUsernameKey(e.target.value)} />
                </div>
                <div className="w-36">
                  <label className="text-xs text-muted-foreground block mb-1">Chave senha</label>
                  <Input value={passwordKey} onChange={(e) => setPasswordKey(e.target.value)} />
                </div>
              </>
            )}
          </div>
        )}

        <div className="flex items-center gap-2">
          <Switch checked={produceConsumeEnabled} onCheckedChange={setProduceConsumeEnabled} id="pc-toggle" />
          <label htmlFor="pc-toggle" className="text-sm font-medium cursor-pointer">
            Produzir e consumir mensagem de teste
          </label>
        </div>

        {produceConsumeEnabled && (
          <div className="flex flex-wrap items-start gap-3 pl-8">
            <div className="w-56">
              <label className="text-xs text-muted-foreground block mb-1">Tópico</label>
              <Input placeholder="meu-topico-teste" value={topic} onChange={(e) => setTopic(e.target.value)} />
            </div>
            <div className="flex items-center gap-2 pt-5">
              <Checkbox checked={confirmProduce} onCheckedChange={(v) => setConfirmProduce(!!v)} id="confirm-produce" />
              <label htmlFor="confirm-produce" className="text-sm text-amber-600 dark:text-amber-400 cursor-pointer max-w-md">
                Entendo que isso publica uma mensagem real neste tópico
              </label>
            </div>
          </div>
        )}
      </div>

      <div className="p-6 flex flex-col gap-4">
        {isRunning && (
          <div className="rounded-md border border-border p-4">
            <div className="flex items-center gap-2 text-sm mb-2">
              <Loader2 className="w-4 h-4 animate-spin text-primary" />
              {phaseMessage}
            </div>
            <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary transition-all duration-300"
                style={{ width: `${Math.round(progress * 100)}%` }}
              />
            </div>
          </div>
        )}

        {runError && (
          <div className="rounded-md border p-3 text-sm flex items-center gap-2 border-red-500/40 bg-red-500/10 text-red-500">
            <AlertTriangle className="w-4 h-4 shrink-0" />
            {runError}
          </div>
        )}

        {!isRunning && !result && !runError && (
          <div className="text-sm text-muted-foreground flex items-center gap-2">
            <Waypoints className="w-4 h-4" />
            Selecione cluster, namespace, o Deployment de onde o teste deve partir e o broker
            (host:porta) e clique em "Executar Teste" — anexa um container efêmero num pod já
            rodando desse Deployment, refletindo a identidade de rede real dele
            (NetworkPolicy/Istio avaliados por label/service account do pod).
          </div>
        )}

        {result && (
          <div className="flex flex-col gap-4">
            <div className="text-xs text-muted-foreground">
              Testado a partir do pod <span className="font-mono">{result.target_pod}</span>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {deriveConnectivityBadges(result.connectivity.status, saslEnabled).map((b) => (
                <StageBadge key={b.label} label={b.label} status={b.status} />
              ))}
              {produceConsumeEnabled && (
                <StageBadge
                  label="Produzir/Consumir"
                  status={
                    result.produce_consume.status === "ok"
                      ? "ok"
                      : result.produce_consume.status === "skipped"
                      ? "skipped"
                      : "failed"
                  }
                />
              )}
            </div>

            <div className="text-sm text-muted-foreground">{result.connectivity.message}</div>

            {(result.connectivity.broker_count || result.connectivity.topic_count) && (
              <div className="flex items-center gap-3 text-sm">
                {!!result.connectivity.broker_count && (
                  <Badge variant="outline">{result.connectivity.broker_count} broker(s)</Badge>
                )}
                {!!result.connectivity.topic_count && (
                  <Badge variant="outline">{result.connectivity.topic_count} tópico(s)</Badge>
                )}
              </div>
            )}

            {produceConsumeEnabled && (
              <div className="text-sm text-muted-foreground">
                {result.produce_consume.message}
                {result.produce_consume.round_trip_ms !== undefined && (
                  <span className="font-mono ml-2">({result.produce_consume.round_trip_ms}ms round-trip aprox.)</span>
                )}
              </div>
            )}

            <Collapsible open={rawOutputOpen} onOpenChange={setRawOutputOpen}>
              <CollapsibleTrigger asChild>
                <Button variant="ghost" size="sm" className="w-fit gap-1 text-xs text-muted-foreground">
                  {rawOutputOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                  Saída bruta do kcat
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1.5 rounded-md border border-border bg-muted/30 p-3 text-[11px] font-mono whitespace-pre-wrap overflow-x-auto">
                  {result.connectivity.raw_output || "(sem saída)"}
                  {produceConsumeEnabled && result.produce_consume.raw_output && (
                    <>
                      {"\n\n--- produce/consume ---\n"}
                      {result.produce_consume.raw_output}
                    </>
                  )}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          </div>
        )}
      </div>
    </div>
  );
}
