import { useCallback, useEffect, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, RefreshCw, Copy, Trash2, ScanEye, Binary, AlertTriangle, Save } from "lucide-react";
import { toast } from "sonner";
import yaml from "js-yaml";
import { apiClient } from "@/lib/api/client";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { ProtectedAction } from "@/components/rbac";
import type { AKVDiscoveredKey } from "@/lib/api/types";

interface AKVDiscoveryModalProps {
  cluster: string;
  namespace: string;
  name: string;
  targetName: string;
  onClose: () => void;
  // defaultTargetSecretName — nome do Secret REAL sugerido pra "Salvar como Secret" (ver abaixo),
  // normalmente o target_name da referência de ExternalSecret já existente no namespace
  // (AKVDiscoveryTab.tsx → selectedRefSummary.target_name). Nunca é o `targetName` do parâmetro
  // acima (esse é sempre o Secret TEMPORÁRIO desta consulta, apagado ao fechar) — editável, então
  // fica em branco quando não há referência conhecida.
  defaultTargetSecretName?: string;
}

const POLL_INTERVAL_MS = 2000;
const POLL_MAX_ATTEMPTS = 30; // ~60s de tentativa automática antes de exigir clique manual

/**
 * AKVDiscoveryModal — resultado da Descoberta AKV, num modal próprio e resizable (mesmo padrão
 * de handles de PodQuickViewModal.tsx). Reaproveita MonacoYamlEditor pra ganhar de graça o
 * Ctrl+Shift+E/D (encode/decode base64 no texto selecionado, menu de contexto) — mesmo mecanismo
 * já usado no Monaco da aba Secrets. O botão "tudo" alterna a representação inteira entre
 * Base64/Decodificado sem precisar selecionar nada.
 *
 * Ciclo de vida do ExternalSecret de descoberta é responsabilidade deste modal: apaga
 * automaticamente ao fechar (best-effort — o reaper do backend é a rede de segurança real se essa
 * chamada falhar) e também via botão explícito "Encerrar e Remover".
 *
 * "Salvar como Secret" (edição manual): motivado por um caso real — às vezes o AKV não reflete o
 * segredo (falha na construção da própria secret na origem, ou algum impedimento técnico impede o
 * sync), mas pra testar a aplicação em HLG o usuário precisa do Secret existindo mesmo assim, com
 * valores digitados manualmente. Reaproveita EXATAMENTE o mesmo endpoint (`apiClient.applySecret`,
 * server-side apply com --force-conflicts, ver internal/kubernetes/client.go) já usado pela aba
 * Secrets — cria o Secret se ele não existir, ou sobrescreve se existir (mesmo quando gerenciado
 * por um external-secrets real: --force-conflicts toma ownership dos campos editados). Nunca
 * escreve no Azure Key Vault (fonte real) — só no Secret K8s, e fica explícito na UI que isso é
 * perdido no próximo sync bem-sucedido do external-secrets.
 */
export function AKVDiscoveryModal({ cluster, namespace, name, targetName, onClose, defaultTargetSecretName }: AKVDiscoveryModalProps) {
  // height=820 garante espaço pro editor mostrar 30 linhas por padrão em vez das ~15 anteriores
  // (MonacoYamlEditor usa lineHeight=20px → 30 linhas = 600px, ver minHeight no container do
  // editor abaixo; o resto é chrome do modal — header/toolbar/status).
  const [modalSize, setModalSize] = useState({ width: 900, height: 820 });
  const resizing = useRef(false);
  const resizeDir = useRef<"se" | "e" | "s">("se");
  const lastResizePos = useRef({ x: 0, y: 0 });

  const [ready, setReady] = useState(false);
  const [statusMessage, setStatusMessage] = useState<string | undefined>();
  const [statusReason, setStatusReason] = useState<string | undefined>();
  const [attempts, setAttempts] = useState(0);
  const [polling, setPolling] = useState(true);
  const [keys, setKeys] = useState<AKVDiscoveredKey[] | null>(null);
  const [loadingData, setLoadingData] = useState(false);
  // Base64 é o padrão — mesma representação original de um Secret K8s de verdade, e a mesma
  // convenção de "mascarado até ação explícita" já usada no resto da app (Secrets/Dependencies).
  // Importa aqui em especial: um único ExternalSecret pode reunir chaves usadas por aplicações
  // diferentes (find/regex amplo) — decodificar tudo por padrão exporia valor em texto puro de
  // algo que pode nem pertencer à aplicação que o usuário está de fato investigando.
  const [showDecoded, setShowDecoded] = useState(false);
  const [stopping, setStopping] = useState(false);
  const stoppedRef = useRef(false);

  // Edição manual + "Salvar como Secret" — ver comentário do componente acima.
  const [editorValue, setEditorValue] = useState("");
  const [targetSecretName, setTargetSecretName] = useState(defaultTargetSecretName ?? "");
  const [saveConfirmOpen, setSaveConfirmOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return;
      const dx = e.clientX - lastResizePos.current.x;
      const dy = e.clientY - lastResizePos.current.y;
      lastResizePos.current = { x: e.clientX, y: e.clientY };
      setModalSize((prev) => ({
        width: resizeDir.current !== "s" ? Math.max(560, prev.width + dx) : prev.width,
        // 700 — piso que ainda comporta as 30 linhas mínimas garantidas do editor (ver minHeight
        // no container dele) sem cortar o chrome do modal (header/toolbar/status).
        height: resizeDir.current !== "e" ? Math.max(700, prev.height + dy) : prev.height,
      }));
    };
    const onUp = () => {
      if (!resizing.current) return;
      resizing.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, []);

  const fetchData = useCallback(async () => {
    setLoadingData(true);
    try {
      const res = await apiClient.getAKVDiscoveryData(cluster, namespace, name);
      setKeys(res.keys);
    } catch (e) {
      toast.error("Falha ao buscar os dados sincronizados", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    } finally {
      setLoadingData(false);
    }
  }, [cluster, namespace, name]);

  const pollOnce = useCallback(async () => {
    try {
      const status = await apiClient.getAKVDiscoveryStatus(cluster, namespace, name);
      setStatusMessage(status.status_message);
      setStatusReason(status.status_reason);
      if (status.ready) {
        setReady(true);
        setPolling(false);
        fetchData();
      }
    } catch (e) {
      setStatusMessage(e instanceof Error ? e.message : "Erro desconhecido ao consultar status");
    }
  }, [cluster, namespace, name, fetchData]);

  useEffect(() => {
    if (!polling) return;
    if (attempts >= POLL_MAX_ATTEMPTS) {
      setPolling(false);
      return;
    }
    const t = setTimeout(() => {
      pollOnce();
      setAttempts((a) => a + 1);
    }, POLL_INTERVAL_MS);
    return () => clearTimeout(t);
  }, [polling, attempts, pollOnce]);

  // 1ª tentativa imediata, sem esperar o primeiro intervalo
  useEffect(() => {
    pollOnce();
    setAttempts(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const stopSession = useCallback(async () => {
    if (stoppedRef.current) return;
    stoppedRef.current = true;
    try {
      await apiClient.stopAKVDiscovery(cluster, namespace, name);
    } catch {
      // best-effort — o reaper do backend (30min) é a rede de segurança real
    }
  }, [cluster, namespace, name]);

  const handleClose = async () => {
    setStopping(true);
    await stopSession();
    setStopping(false);
    onClose();
  };

  // Cleanup best-effort se o componente desmontar sem passar por handleClose (ex: troca de aba)
  useEffect(() => {
    return () => {
      void stopSession();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // baselineYaml — reconstruído sempre que a sincronização traz dado novo ou o modo de exibição
  // muda; keys===null (sync ainda não trouxe nada, ou falhou) vira um esqueleto de Secret VÁLIDO E
  // EDITÁVEL (data: {}) em vez de um comentário — pra permitir digitar chaves manualmente mesmo
  // quando o AKV nunca sincronizou nada de verdade (o motivo desta feature existir).
  const baselineYaml = (() => {
    const data: Record<string, string> = {};
    if (keys) {
      for (const k of keys) {
        if (k.is_binary) {
          data[k.key] = showDecoded ? `<binário — ${k.value_base64.length} chars em base64, veja abaixo>` : k.value_base64;
        } else {
          data[k.key] = showDecoded ? (k.value_decoded ?? "") : k.value_base64;
        }
      }
    }
    const doc = {
      apiVersion: "v1",
      kind: "Secret",
      metadata: { name: targetName, namespace },
      [showDecoded ? "dataDecoded" : "data"]: data,
    };
    return yaml.dump(doc, { lineWidth: -1 });
  })();

  // Ressincroniza o buffer editável com o baseline sempre que ele muda de verdade (nova resposta
  // do Vault, ou toggle Base64/Decodificado) — edições em andamento só sobrevivem enquanto keys e
  // showDecoded não mudarem, o que é esperado aqui (ver comentário do componente: esta feature é
  // uma sobreposição manual explícita, não precisa persistir através de re-sincronizações).
  useEffect(() => {
    setEditorValue(baselineYaml);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [baselineYaml]);

  const handleCopyAll = () => {
    navigator.clipboard.writeText(editorValue).then(() => toast.success("Copiado para a área de transferência"));
  };

  // "Salvar como Secret": server-side apply direto no cluster (cria se não existir, sobrescreve se
  // existir) — nunca toca o Azure Key Vault. metadata.name/namespace do YAML editado são sempre
  // forçados pro nome de destino escolhido (targetSecretName) e pro namespace desta sessão, mesmo
  // que o usuário tenha editado esses campos no texto — evita o "secret name mismatch" que o
  // backend rejeitaria se o YAML e o parâmetro de URL divergissem (ver prepareSecretApplyPayload
  // em internal/kubernetes/client.go).
  const handleSaveClick = () => {
    if (showDecoded) {
      toast.error("Não é possível salvar com valores decodificados", {
        description: "Clique em \"Ver em Base64 (tudo)\" antes de salvar.",
      });
      return;
    }
    if (!targetSecretName.trim()) {
      toast.error("Informe o nome do Secret de destino");
      return;
    }
    setSaveConfirmOpen(true);
  };

  const confirmSave = async () => {
    const finalName = targetSecretName.trim();
    setSaving(true);
    try {
      const parsed = yaml.load(editorValue);
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        throw new Error("YAML inválido — esperado um objeto Secret (apiVersion/kind/metadata/data)");
      }
      const doc = parsed as Record<string, unknown>;
      const metadata = (doc.metadata as Record<string, unknown>) ?? {};
      metadata.name = finalName;
      metadata.namespace = namespace;
      doc.metadata = metadata;
      const finalYaml = yaml.dump(doc, { lineWidth: -1 });

      await apiClient.applySecret(cluster, namespace, finalName, {
        yaml: finalYaml,
        fieldManager: "akv-discovery-manual-override",
        dryRun: false,
        force: true,
      });
      toast.success("Secret salvo", {
        description: `${namespace}/${finalName} — será sobrescrito no próximo sync bem-sucedido do external-secrets, se houver`,
      });
      setSaveConfirmOpen(false);
    } catch (e) {
      toast.error("Falha ao salvar o Secret", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open) handleClose(); }}>
      <DialogContent
        className="flex flex-col p-0 gap-0 overflow-hidden"
        style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
      >
        <DialogHeader className="px-4 pt-4 pb-3 border-b border-border flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <ScanEye className="h-4 w-4" />
            Descoberta AKV — <span className="font-mono text-xs text-muted-foreground">{namespace}/{name}</span>
          </DialogTitle>
        </DialogHeader>

        <div className="flex items-center gap-2 px-4 py-2 border-b border-border flex-shrink-0 flex-wrap">
          {!ready ? (
            <Badge variant="outline" className="gap-1 bg-blue-500/10 text-blue-400 border-blue-500/30">
              <Loader2 className="h-3 w-3 animate-spin" /> Sincronizando com o Vault...
            </Badge>
          ) : (
            <Badge variant="outline" className="gap-1 bg-green-500/10 text-green-500 border-green-500/30">
              Sincronizado
            </Badge>
          )}
          {!polling && !ready && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              onClick={() => { setPolling(true); setAttempts(0); }}
            >
              <RefreshCw className="h-3 w-3 mr-1" /> Tentar de novo
            </Button>
          )}
          <div className="flex-1" />
          {keys && keys.length > 0 && (
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setShowDecoded((v) => !v)}>
              <Binary className="h-3 w-3 mr-1" />
              {showDecoded ? "Ver em Base64 (tudo)" : "Decodificar tudo (Base64 → texto)"}
            </Button>
          )}
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={handleCopyAll} disabled={!keys}>
            <Copy className="h-3 w-3 mr-1" /> Copiar
          </Button>
          <ProtectedAction>
            <Button
              size="sm"
              variant="destructive"
              className="h-7 text-xs"
              disabled={stopping}
              onClick={handleClose}
            >
              {stopping ? <Loader2 className="h-3 w-3 mr-1 animate-spin" /> : <Trash2 className="h-3 w-3 mr-1" />}
              Encerrar e Remover
            </Button>
          </ProtectedAction>
        </div>

        {/* "Salvar como Secret" — edição manual pra testes em HLG quando o AKV não sincroniza
            (falha na origem ou impedimento técnico). Nunca escreve no Vault, só no Secret K8s. */}
        <div className="flex items-center gap-2 px-4 py-2 border-b border-border flex-shrink-0 flex-wrap">
          <Label htmlFor="akv-target-secret-name" className="text-xs text-muted-foreground flex-shrink-0">
            Salvar como Secret:
          </Label>
          <Input
            id="akv-target-secret-name"
            className="h-7 text-xs font-mono max-w-xs"
            placeholder="nome do Secret de destino"
            value={targetSecretName}
            onChange={(e) => setTargetSecretName(e.target.value)}
          />
          <ProtectedAction>
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs border-amber-500/40 text-amber-500 hover:bg-amber-500/10 hover:text-amber-400"
              disabled={showDecoded || !targetSecretName.trim()}
              onClick={handleSaveClick}
              title={showDecoded ? "Volte pra Base64 antes de salvar" : "Cria ou sobrescreve o Secret no cluster (não escreve no Vault)"}
            >
              <Save className="h-3 w-3 mr-1" /> Salvar
            </Button>
          </ProtectedAction>
          <span className="text-[11px] text-muted-foreground">
            Sobrescreve o Secret real — perdido no próximo sync bem-sucedido do external-secrets.
          </span>
        </div>

        {statusMessage && (
          <div className="px-4 py-2 border-b border-border flex-shrink-0 text-xs flex items-start gap-1.5 text-muted-foreground">
            {!ready && <AlertTriangle className="h-3.5 w-3.5 flex-shrink-0 mt-0.5 text-amber-500" />}
            <span className="break-all">
              {statusReason ? <span className="font-medium">{statusReason}: </span> : null}
              {statusMessage}
            </span>
          </div>
        )}

        {/* minHeight: 30 linhas * lineHeight 20px (MonacoYamlEditor) — garante o mínimo pedido
            mesmo se o modal for redimensionado menor que o ideal pro chrome ao redor. */}
        <div className="flex-1 min-h-0" style={{ minHeight: 600 }}>
          {loadingData && !keys ? (
            <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 mr-2 animate-spin" /> Carregando dados sincronizados...
            </div>
          ) : (
            <MonacoYamlEditor
              value={editorValue}
              onChange={(v) => setEditorValue(v)}
              mode="editor"
              height="100%"
              readOnly={false}
              autoCheckSecretWhitespace={!showDecoded}
              documentKey={`${cluster}/${namespace}/${name}`}
            />
          )}
        </div>

        {/* Handles de resize — mesmo padrão de PodQuickViewModal.tsx */}
        <div
          className="absolute top-0 right-0 w-1.5 h-full cursor-e-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "e";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "e-resize";
            document.body.style.userSelect = "none";
          }}
        />
        <div
          className="absolute bottom-0 left-0 w-full h-1.5 cursor-s-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "s";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "s-resize";
            document.body.style.userSelect = "none";
          }}
        />
        <div
          className="absolute bottom-0 right-0 w-4 h-4 cursor-se-resize z-50 flex items-end justify-end pr-0.5 pb-0.5"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "se";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "se-resize";
            document.body.style.userSelect = "none";
          }}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" className="text-muted-foreground/40 hover:text-primary/60">
            <path d="M9 1 L9 9 L1 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
            <path d="M9 5 L9 9 L5 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
          </svg>
        </div>
      </DialogContent>

      <Dialog open={saveConfirmOpen} onOpenChange={(o) => { if (!saving) setSaveConfirmOpen(o); }}>
        <DialogContent className="max-w-lg bg-background border-border">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-base">
              <AlertTriangle className="h-4 w-4 text-amber-500" /> Confirmar salvamento manual
            </DialogTitle>
            <DialogDescription>
              Isso cria ou sobrescreve o Secret abaixo diretamente no cluster, agora — não é uma
              simulação. Se o Secret já for gerenciado por um external-secrets de verdade, esta
              operação toma ownership dos campos editados; o próximo sync bem-sucedido do AKV
              sobrescreve os valores manuais de novo.
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs space-y-1">
            <p><span className="text-muted-foreground">Cluster:</span> {cluster}</p>
            <p><span className="text-muted-foreground">Namespace:</span> {namespace}</p>
            <p><span className="text-muted-foreground">Secret:</span> {targetSecretName.trim()}</p>
          </div>
          <DialogFooter className="gap-2">
            <Button variant="outline" size="sm" disabled={saving} onClick={() => setSaveConfirmOpen(false)}>
              Cancelar
            </Button>
            <Button size="sm" disabled={saving} onClick={confirmSave}>
              {saving ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <Save className="h-3.5 w-3.5 mr-1.5" />}
              Confirmar e salvar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Dialog>
  );
}
