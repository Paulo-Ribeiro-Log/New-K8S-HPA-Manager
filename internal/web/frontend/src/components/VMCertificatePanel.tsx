import { useEffect, useRef, useState } from "react";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Loader2,
  RefreshCcw,
  ShieldCheck,
  ShieldAlert,
  FolderOpen,
  Upload,
  Terminal as TerminalIcon,
  CheckCircle2,
  XCircle,
  FileSearch,
  Folder,
  File as FileIcon,
  Home,
  ChevronRight,
  ArrowRight,
  Check,
  ChevronsUpDown,
  Server,
  Pencil,
} from "lucide-react";
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import { useVMAwsProfiles, useVMInstances, useVMCredentialProfiles } from "@/hooks/useVMs";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { CertificateSourcePickerModal } from "@/components/CertificateSourcePickerModal";
import VMConnectModal from "@/components/VMConnectModal";
import { countPemCertificates } from "@/lib/pemUtils";
import { apiClient } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import type {
  VMInstance,
  VMCertValidateResult,
  VMCertTransferResult,
  VMCertTransferSSMResult,
  VMCertReadResult,
  VMSFTPBrowseEntry,
} from "@/lib/api/types";
import type { CertificateInfo } from "@/types/certificates";

const REGION_SHORTCUTS = ["us-east-1", "us-east-2", "us-west-2", "sa-east-1"];

function authToken(): string {
  return localStorage.getItem("auth_token") ?? "";
}

// ApiFetchError — mesmo padrão de VMSFTPModal.tsx: carrega code/fingerprint estruturados do
// corpo de erro além da mensagem (apiClient.request() descartaria os dois, só propaga a
// mensagem) — necessário aqui pro caso SSH_HOSTKEY_UNKNOWN durante a transferência.
class ApiFetchError extends Error {
  code?: string;
  fingerprint?: string;
}

async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { "Content-Type": "application/json", ...(init.headers as Record<string, string> | undefined) };
  const token = authToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const resp = await fetch(url, { ...init, headers });
  return resp;
}

async function parseJSON<T>(resp: Response): Promise<T> {
  const body = await resp.json().catch(() => ({}));
  return body as T;
}

// parseHostInput — aceita tanto "exemplo.com"/"exemplo.com:8443" quanto uma URL completa
// ("https://exemplo.com/algum/caminho?x=1") e extrai host+porta, ignorando path/query. Motivado
// por um relato real do usuário: a checagem ao vivo "sempre falhava" — causa provável era colar a
// URL do site inteira (com "https://" e possivelmente um path) no campo que antes só aceitava um
// host puro, fazendo o dial TCP tentar resolver ISSO como hostname literal. Normaliza SEMPRE
// prefixando um esquema (quando ausente) antes de delegar pro parser nativo `URL` do browser — um
// único caminho de código cobre os dois formatos, sem regex frágil própria.
function parseHostInput(raw: string): { host: string; port: number } | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`;
  try {
    const url = new URL(withScheme);
    if (!url.hostname) return null;
    const port = url.port ? parseInt(url.port, 10) : url.protocol === "http:" ? 80 : 443;
    return { host: url.hostname, port };
  } catch {
    return null;
  }
}

// CertInfoCard — mesmo bloco de informações (Subject/Issuer/expiração/SANs) usado tanto pra "o
// que já está na VM" (ReadRemoteCertificate) quanto pra "o par que estou prestes a instalar"
// (ValidateCertKeyPair) — um único ponto de renderização, sem duplicar markup.
function CertInfoCard({ info }: { info: CertificateInfo }) {
  return (
    <div className="text-muted-foreground space-y-0.5">
      <div className="flex items-center gap-2 mb-1">{getStatusBadge(info.status)}</div>
      <div>Subject: <span className="font-mono">{info.subject}</span></div>
      <div>Issuer: <span className="font-mono">{info.issuer}</span></div>
      <div>Expira em: {new Date(info.notAfter).toLocaleString("pt-BR")} ({info.daysRemaining}d)</div>
      {info.dnsNames?.length > 0 && <div>SANs: {info.dnsNames.join(", ")}</div>}
    </div>
  );
}

// InstanceSelect — combobox Popover+Command (não Radix <Select>), mesmo componente/motivo já
// documentado em VMTerminalModal.tsx/VMSFTPModal.tsx (ProfileSelect): <Select> nativo não escala
// pra listas longas (sem busca nenhuma) e o usuário pediu explicitamente pesquisa na seleção de
// instância — uma conta AWS real facilmente tem dezenas/centenas de EC2. Busca por nome, ID ou
// qualquer um dos dois IPs (mesmos campos já usados no filtro de texto de VMsTab.tsx).
function InstanceSelect({
  value,
  onChange,
  instances,
  loading,
}: {
  value: string;
  onChange: (id: string) => void;
  instances: VMInstance[];
  loading?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const selected = instances.find((i) => i.id === value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={loading || instances.length === 0}
          className="h-8 text-xs w-full max-w-md justify-between font-normal"
        >
          <span className="flex items-center gap-1.5 truncate">
            <Server className="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
            <span className="truncate">
              {selected
                ? `${selected.name}${selected.name !== selected.id ? ` (${selected.id})` : ""} — ${selected.state}`
                : loading
                ? "Carregando instâncias..."
                : instances.length === 0
                ? "Nenhuma instância encontrada"
                : "Selecione a instância..."}
            </span>
          </span>
          <ChevronsUpDown className="ml-2 h-3.5 w-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder="Buscar por nome, ID ou IP..." className="text-xs" />
          <CommandList>
            <CommandEmpty>Nenhuma instância encontrada.</CommandEmpty>
            <CommandGroup>
              {instances.map((inst) => (
                <CommandItem
                  key={inst.id}
                  value={`${inst.name} ${inst.id} ${inst.privateIp ?? ""} ${inst.publicIp ?? ""} ${inst.instanceType ?? ""}`}
                  onSelect={() => {
                    onChange(inst.id);
                    setOpen(false);
                  }}
                  className="text-xs"
                >
                  <Check className={cn("mr-2 h-3.5 w-3.5 flex-shrink-0", value === inst.id ? "opacity-100" : "opacity-0")} />
                  <div className="flex flex-col min-w-0">
                    <span className="truncate">
                      {inst.name}
                      {inst.name !== inst.id && <span className="text-muted-foreground"> ({inst.id})</span>}
                      {" · "}
                      {inst.state}
                    </span>
                    {(inst.privateIp || inst.publicIp) && (
                      <span className="text-muted-foreground text-[10px] truncate">
                        {inst.privateIp && `priv: ${inst.privateIp}`}
                        {inst.privateIp && inst.publicIp && " · "}
                        {inst.publicIp && `pub: ${inst.publicIp}`}
                      </span>
                    )}
                  </div>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// RemoteFileBrowserDialog — navegador de diretórios read-only embutido, reaproveitando o MESMO
// endpoint de listagem já usado pelo Arquivos (SFTP) da Fase 4 (GET .../sftp/list) — sem endpoint
// novo. Existe pra resolver "como vejo o que já está na VM se não tenho nada que me leve até lá":
// sem isso, o usuário só conseguia inspecionar/instalar um certificado se já soubesse o caminho
// exato de cor. Ao clicar num arquivo, devolve o caminho via onPick e fecha.
function RemoteFileBrowserDialog({
  open,
  onOpenChange,
  listUrl,
  onPick,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  listUrl: string; // já inclui host/porta/túnel/credencial — só falta &path=
  onPick: (path: string) => void;
}) {
  const [path, setPath] = useState("/");
  const [entries, setEntries] = useState<VMSFTPBrowseEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Edição manual do caminho — mesmo padrão de VMSFTPModal.tsx (pedido explícito do usuário): o
  // breadcrumb clique-a-clique não basta quando já se sabe o caminho exato de cor.
  const [editingPath, setEditingPath] = useState(false);
  const [pathInput, setPathInput] = useState("/");
  const pathInputRef = useRef<HTMLInputElement>(null);

  // Confirmação de host key SSH desconhecida (TOFU) — BUG REAL CORRIGIDO, relatado ao vivo pelo
  // usuário ("o botão procurar na VM não funciona"): esta tela nunca tratava SSH_HOSTKEY_UNKNOWN
  // de verdade, só mostrava o texto cru do erro ("...confirme e tente de novo") sem nenhum jeito
  // de CONFIRMAR — pra qualquer instância na primeira conexão, a listagem falhava sempre, sem
  // saída nenhuma (indistinguível de "não funciona"). VMSFTPModal.tsx já tinha esse tratamento;
  // faltava replicar aqui. Mesmo mecanismo: aceitar reenvia a MESMA chamada com
  // acceptHostKeyFingerprint=<fp>, que grava em vm_known_hosts no backend — dali em diante (até
  // outra sessão do modal, sem precisar reconfirmar de novo, já que o known_hosts é persistido no
  // servidor, não neste estado local).
  const [hostKeyToConfirm, setHostKeyToConfirm] = useState<string | null>(null);
  const [acceptedFingerprint, setAcceptedFingerprint] = useState<string | null>(null);

  // fingerprintOverride — só usado por handleTrustHostKey: setState é assíncrono, então o
  // acceptedFingerprint recém-aceito ainda não estaria visível pra esta chamada via closure de
  // estado; passar explicitamente evita depender de um 2º render pra re-tentar.
  const load = async (targetPath: string, fingerprintOverride?: string) => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ path: targetPath });
      const fp = fingerprintOverride ?? acceptedFingerprint;
      if (fp) params.set("acceptHostKeyFingerprint", fp);
      const resp = await apiFetch(`${listUrl}&${params.toString()}`);
      const body = await parseJSON<{
        success?: boolean;
        entries?: VMSFTPBrowseEntry[];
        error?: { message: string; code?: string; fingerprint?: string };
      }>(resp);
      if (!resp.ok || body.error) {
        if (body.error?.code === "SSH_HOSTKEY_UNKNOWN" && body.error.fingerprint) {
          setHostKeyToConfirm(body.error.fingerprint);
          return;
        }
        setError(body.error?.message || `HTTP ${resp.status}`);
        setEntries([]);
        return;
      }
      const sorted = (body.entries ?? []).sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
        return a.name.localeCompare(b.name);
      });
      setEntries(sorted);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Erro ao listar diretório");
      setEntries([]);
    } finally {
      setLoading(false);
    }
  };

  const handleTrustHostKey = () => {
    if (!hostKeyToConfirm) return;
    const fingerprint = hostKeyToConfirm;
    setAcceptedFingerprint(fingerprint);
    setHostKeyToConfirm(null);
    void load(path, fingerprint);
  };

  const handleRejectHostKey = () => {
    setHostKeyToConfirm(null);
    setError("Host key rejeitada — a conexão não pode continuar sem confiar nela.");
  };

  useEffect(() => {
    if (open) {
      setPath("/");
      setHostKeyToConfirm(null);
      setAcceptedFingerprint(null);
      load("/");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const navigateTo = (p: string) => {
    setPath(p || "/");
    load(p || "/");
  };
  const breadcrumbSegments = path === "/" ? [] : path.split("/").filter(Boolean);

  const startEditingPath = () => {
    setPathInput(path);
    setEditingPath(true);
  };

  const commitPathInput = () => {
    const trimmed = pathInput.trim();
    navigateTo(trimmed.startsWith("/") ? trimmed : `/${trimmed}`);
    setEditingPath(false);
  };

  useEffect(() => {
    if (editingPath) {
      pathInputRef.current?.focus();
      pathInputRef.current?.select();
    }
  }, [editingPath]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg h-[70vh] flex flex-col overflow-hidden">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><FolderOpen className="h-4 w-4" /> Procurar na VM</DialogTitle>
          <DialogDescription>Clique num arquivo para escolher o caminho.</DialogDescription>
        </DialogHeader>

        {hostKeyToConfirm ? (
          <div className="flex-1 min-h-0 flex flex-col items-center justify-center gap-3 text-center px-6 border rounded-md">
            <ShieldAlert className="h-8 w-8 text-amber-500" />
            <div className="space-y-1">
              <p className="text-sm font-medium">Host key SSH desconhecida</p>
              <p className="text-xs text-muted-foreground">
                Esta é a primeira conexão com esta instância. Confira a fingerprint abaixo antes de confiar:
              </p>
              <code className="block mt-1 px-2 py-1 rounded bg-muted text-xs font-mono break-all">{hostKeyToConfirm}</code>
            </div>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" onClick={handleRejectHostKey}>Rejeitar</Button>
              <Button size="sm" onClick={handleTrustHostKey}>Confiar e continuar</Button>
            </div>
          </div>
        ) : (
          <>
            {editingPath ? (
              <Input
                ref={pathInputRef}
                value={pathInput}
                onChange={(e) => setPathInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitPathInput();
                  else if (e.key === "Escape") setEditingPath(false);
                }}
                onBlur={() => setEditingPath(false)}
                placeholder="/etc/nginx/"
                className="h-8 text-xs font-mono flex-shrink-0"
              />
            ) : (
              <div className="flex items-center gap-1 flex-shrink-0">
                <div className="flex items-center gap-1 text-xs flex-1 min-w-0 overflow-x-auto whitespace-nowrap">
                  <button className="p-1 rounded hover:bg-accent flex-shrink-0" onClick={() => navigateTo("/")} title="Raiz">
                    <Home className="h-3.5 w-3.5" />
                  </button>
                  {breadcrumbSegments.map((seg, i) => {
                    const segPath = "/" + breadcrumbSegments.slice(0, i + 1).join("/");
                    return (
                      <span key={segPath} className="flex items-center gap-1 flex-shrink-0">
                        <ChevronRight className="h-3 w-3 text-muted-foreground" />
                        <button className="hover:underline" onClick={() => navigateTo(segPath)}>{seg}</button>
                      </span>
                    );
                  })}
                </div>
                <Button variant="ghost" size="icon" className="h-7 w-7 flex-shrink-0" onClick={startEditingPath} title="Digitar o caminho diretamente">
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
              </div>
            )}

            <div className="flex-1 min-h-0 overflow-hidden border rounded-md">
              {error ? (
                <div className="flex flex-col items-center justify-center h-full gap-2 text-center px-4">
                  <p className="text-sm text-destructive">{error}</p>
                  <Button variant="outline" size="sm" onClick={() => load(path)}>Tentar de novo</Button>
                </div>
              ) : loading ? (
                <div className="flex items-center justify-center h-full gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
                </div>
              ) : entries.length === 0 ? (
                <div className="flex items-center justify-center h-full text-sm text-muted-foreground">Pasta vazia.</div>
              ) : (
                <ScrollArea className="h-full">
                  <div className="divide-y divide-border/50">
                    {entries.map((entry) => (
                      <button
                        key={entry.path}
                        className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-accent/50 transition-colors text-left"
                        onClick={() => {
                          if (entry.is_dir) {
                            navigateTo(entry.path);
                          } else {
                            onPick(entry.path);
                            onOpenChange(false);
                          }
                        }}
                      >
                        {entry.is_dir ? (
                          <Folder className="h-4 w-4 text-blue-400 flex-shrink-0" />
                        ) : (
                          <FileIcon className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                        )}
                        <span className="truncate flex-1">{entry.name}</span>
                        {entry.is_dir && <ChevronRight className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />}
                      </button>
                    ))}
                  </div>
                </ScrollArea>
              )}
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// VMCertificatePanel — sub-aba "Certificados em VM" (Fase 6 do plano em
// /home/paulo/.claude/plans/scalable-greeting-kazoo.md), dentro de Certificados TLS. Fluxo:
// escolher instância (mesmas listagens já usadas em VMsTab.tsx) → conectar → ver o certificado
// JÁ instalado (navegando pelos arquivos da VM via SFTP — RemoteFileBrowserDialog — ou digitando
// o caminho direto; sem isso não havia NENHUM jeito de inspecionar o que já está lá, só de colar
// um novo às cegas) → colar/escolher par cert+chave novo → validar (par local + checagem TLS ao
// vivo opcional) → transferir pros caminhos remotos exatos → reload SEMPRE manual (botão abre o
// mesmo terminal SSH/SSM já usado na aba VMs/EC2, sem tentar automatizar) → reconfirmar (roda a
// mesma checagem ao vivo de novo, comparando o número de série servido contra o instalado).
//
// Transporte é escolhido no passo 2, 3 modos: "SSH direto"/"SSM (túnel até sshd)" (os dois via
// SFTP de verdade, ver Fase 4) e "SSM (sem SSH)" — motivado por um caso real relatado pelo
// usuário: instância gerida só via SSM, sem sshd instalado/rodando. Nesse cenário nem o túnel
// resolve (só encaminha TCP, ainda precisa de sshd do outro lado) — o modo "sem SSH" executa via
// AWS SSM Run Command (RunShellCommand, internal/cloudprovider/aws/ssm_command.go), que só
// depende do agente SSM já exigido pelo terminal. Sem navegação visual de arquivos nesse modo
// (fora de escopo desta rodada — o usuário digita o caminho direto).
export function VMCertificatePanel() {
  const { profiles: awsProfiles, loading: loadingAwsProfiles } = useVMAwsProfiles();
  const [awsProfile, setAwsProfile] = useState("");
  const [region, setRegion] = useState("us-east-1");
  const { instances, loading: loadingInstances, refetch: refetchInstances } = useVMInstances(awsProfile, region);
  const [instanceId, setInstanceId] = useState("");
  const instance = instances.find((i) => i.id === instanceId) || null;

  const { profiles: credProfiles, loading: loadingCredProfiles } = useVMCredentialProfiles();

  // "ssm-command" — modo sem SSH algum (nem direto nem via túnel), pra instâncias geridas só via
  // SSM sem sshd instalado/rodando: o túnel SSM só encaminha TCP, ainda precisa de sshd do outro
  // lado, então nem ele resolve nesse caso. Executa via AWS SSM Run Command (RunShellCommand,
  // internal/cloudprovider/aws/ssm_command.go), que só depende do agente SSM já exigido pelo
  // terminal — reaproveita profile/região do passo 1, sem credencial SSH nenhuma.
  const [connectionMode, setConnectionMode] = useState<"ssh" | "ssm" | "ssm-command">("ssh");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [remotePort, setRemotePort] = useState("22");
  const [credentialProfileId, setCredentialProfileId] = useState("");
  const [tunnelSessionId, setTunnelSessionId] = useState<string | null>(null);
  const [tunnelStarting, setTunnelStarting] = useState(false);

  const [tlsCrt, setTlsCrt] = useState("");
  const [tlsKey, setTlsKey] = useState("");
  const [sourcePickerOpen, setSourcePickerOpen] = useState(false);

  // checkTarget — host puro ("exemplo.com"), "host:porta" OU uma URL completa
  // ("https://exemplo.com/caminho") — parseado via parseHostInput na hora de validar. Um único
  // campo em vez de host+porta separados, pra aceitar colar a URL do site direto (pedido
  // explícito do usuário — colar a URL inteira num campo que só aceitava host puro era a causa
  // real de "a validação ao vivo sempre falha").
  const [checkTarget, setCheckTarget] = useState("");
  const [checkSni, setCheckSni] = useState("");

  const [validating, setValidating] = useState(false);
  const [validateResult, setValidateResult] = useState<VMCertValidateResult | null>(null);

  const [remoteCertPath, setRemoteCertPath] = useState("");
  const [remoteKeyPath, setRemoteKeyPath] = useState("");
  const [transferring, setTransferring] = useState(false);
  const [transferResult, setTransferResult] = useState<VMCertTransferResult | VMCertTransferSSMResult | null>(null);

  const [connectOpen, setConnectOpen] = useState(false);

  // "Certificado atual na VM" — leitura via SFTP de um arquivo JÁ instalado (ReadRemoteCertificate),
  // pra responder "como vejo o que já está lá" sem precisar já saber o caminho de cor. browseTarget
  // decide pra qual campo (readPath/remoteCertPath/remoteKeyPath) o caminho escolhido no navegador
  // embutido (RemoteFileBrowserDialog) deve ir.
  const [readPath, setReadPath] = useState("");
  const [reading, setReading] = useState(false);
  const [readResult, setReadResult] = useState<VMCertReadResult | null>(null);
  const [browseTarget, setBrowseTarget] = useState<"readPath" | "remoteCertPath" | "remoteKeyPath" | null>(null);

  // Reset da conexão/validação ao trocar de instância — nunca reaproveita host/resultado de uma
  // instância diferente sem o usuário perceber. Encerra também um túnel SSM porventura aberto pra
  // instância anterior (mesmo princípio de stopTunnelIfAny em VMSFTPModal.tsx) — best-effort, o
  // reaper de ociosidade do backend derruba de qualquer forma se isso falhar.
  useEffect(() => {
    return () => {
      setTunnelSessionId((prevId) => {
        if (prevId) apiClient.stopVMSSMTunnel(instance?.id || "", prevId).catch(() => {});
        return null;
      });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId]);

  useEffect(() => {
    setHost(instance?.publicIp || instance?.privateIp || "");
    setCheckTarget(instance?.publicIp || instance?.privateIp || "");
    setValidateResult(null);
    setTransferResult(null);
    setReadPath("");
    setReadResult(null);
  }, [instanceId]); // eslint-disable-line react-hooks/exhaustive-deps

  // isSSHBased — os dois modos que passam por SFTP de verdade (SSH direto ou túnel SSM até um
  // sshd) — distinto de "ssm-command", que não tem SFTP nenhum (SSM Run Command não fala esse
  // protocolo, só executa comandos de shell).
  const isSSHBased = connectionMode === "ssh" || connectionMode === "ssm";
  // terminalMode — VMConnectModal só conhece "ssh"|"ssm" (não sabe nada sobre "ssm-command", um
  // conceito só deste painel); a conexão de TERMINAL por trás de "ssm-command" já é, de fato, SSM
  // Session Manager — mapeia pro mesmo "ssm" que o modo de túnel já usa pro terminal.
  const terminalMode: "ssh" | "ssm" = connectionMode === "ssh" ? "ssh" : "ssm";

  const targetParams = () =>
    connectionMode === "ssm"
      ? { tunnelSessionId: tunnelSessionId ?? "" }
      : { host: host.trim(), port: Number(port) || 22 };

  const handleStartSSMTunnel = async () => {
    if (!awsProfile || !region || !instance) return;
    setTunnelStarting(true);
    try {
      const { sessionId } = await apiClient.startVMSSMTunnel(instance.id, awsProfile, region, Number(remotePort) || 22);
      setTunnelSessionId(sessionId);
      toast.success("Túnel SSM aberto");
    } catch (e) {
      toast.error("Erro ao abrir túnel SSM", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setTunnelStarting(false);
    }
  };

  // sftpConnectionReady — só se aplica aos modos SSH-based (SFTP de verdade); gateia "Procurar na
  // VM" (browse via SFTP nunca existe no modo ssm-command — ver comentário de isSSHBased).
  const sftpConnectionReady =
    isSSHBased && !!instance && !!credentialProfileId && (connectionMode === "ssh" ? host.trim() !== "" : !!tunnelSessionId);
  // ssmCommandReady — modo sem SSH: só precisa do profile/região já escolhidos no passo 1.
  const ssmCommandReady = connectionMode === "ssm-command" && !!instance && !!awsProfile && !!region;
  // certActionsReady — condição unificada pra "Ler certificado"/"Transferir", que funcionam nos 3
  // modos (cada um por um transporte diferente).
  const certActionsReady = sftpConnectionReady || ssmCommandReady;

  const sftpListUrl = () => {
    if (!instance) return "";
    const params = new URLSearchParams({ ...targetParams(), credentialProfileId } as Record<string, string>);
    return `/api/v1/vms/${encodeURIComponent(instance.id)}/sftp/list?${params.toString()}`;
  };

  const handlePickedPath = (path: string) => {
    if (browseTarget === "remoteCertPath") setRemoteCertPath(path);
    else if (browseTarget === "remoteKeyPath") setRemoteKeyPath(path);
    else setReadPath(path);
    setBrowseTarget(null);
  };

  const handleReadCertificate = async () => {
    if (!instance || !readPath.trim()) return;
    setReading(true);
    setReadResult(null);
    try {
      let resp: Response;
      if (connectionMode === "ssm-command") {
        const params = new URLSearchParams({ profile: awsProfile, region, path: readPath.trim() });
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/read-ssm?${params.toString()}`);
      } else {
        const params = new URLSearchParams({ ...targetParams(), credentialProfileId, path: readPath.trim() } as Record<string, string>);
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/read?${params.toString()}`);
      }
      const result = await parseJSON<VMCertReadResult>(resp);
      setReadResult(result);
      if (!result.success) {
        toast.error("Não foi possível ler o certificado", { description: result.error?.message });
      }
    } catch (e) {
      toast.error("Erro ao ler certificado", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setReading(false);
    }
  };

  const handleValidate = async (withLiveCheck: boolean) => {
    if (!tlsCrt.trim() || !tlsKey.trim()) {
      toast.error("Cole o certificado e a chave privada antes de validar");
      return;
    }

    let liveCheckFields: { checkHost: string; checkPort: number; checkSni?: string } | undefined;
    if (withLiveCheck && checkTarget.trim()) {
      const parsed = parseHostInput(checkTarget);
      if (!parsed) {
        toast.error("URL/host inválido no campo de checagem ao vivo", {
          description: "Aceita um host puro (exemplo.com), host:porta (exemplo.com:8443) ou uma URL completa (https://exemplo.com).",
        });
        return;
      }
      liveCheckFields = { checkHost: parsed.host, checkPort: parsed.port, checkSni: checkSni.trim() || undefined };
    }

    setValidating(true);
    setValidateResult(null);
    try {
      const resp = await apiFetch("/api/v1/vms/certificates/validate", {
        method: "POST",
        body: JSON.stringify({
          certPem: tlsCrt,
          keyPem: tlsKey,
          ...(liveCheckFields ?? {}),
        }),
      });
      const result = await parseJSON<VMCertValidateResult>(resp);
      setValidateResult(result);
      if (!result.success) {
        toast.error("Par cert+chave inválido", { description: result.error?.message });
      }
    } catch (e) {
      toast.error("Erro ao validar", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setValidating(false);
    }
  };

  const handleTransfer = async () => {
    if (!instance) return;
    if (isSSHBased && !credentialProfileId) {
      toast.error("Selecione um perfil de credencial SSH");
      return;
    }
    if (!remoteCertPath.trim() || !remoteKeyPath.trim()) {
      toast.error("Informe os caminhos remotos de destino (certificado e chave)");
      return;
    }
    setTransferring(true);
    setTransferResult(null);
    try {
      let resp: Response;
      if (connectionMode === "ssm-command") {
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/transfer-ssm`, {
          method: "POST",
          body: JSON.stringify({
            profile: awsProfile,
            region,
            certPem: tlsCrt,
            keyPem: tlsKey,
            remoteCertPath: remoteCertPath.trim(),
            remoteKeyPath: remoteKeyPath.trim(),
          }),
        });
      } else {
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/transfer`, {
          method: "POST",
          body: JSON.stringify({
            ...targetParams(),
            credentialProfileId,
            certPem: tlsCrt,
            keyPem: tlsKey,
            remoteCertPath: remoteCertPath.trim(),
            remoteKeyPath: remoteKeyPath.trim(),
          }),
        });
      }
      const result = await parseJSON<VMCertTransferResult | VMCertTransferSSMResult>(resp);
      setTransferResult(result);
      if (result.success) {
        toast.success("Certificado transferido para a VM", {
          description: `${remoteCertPath.trim()} + ${remoteKeyPath.trim()}`,
        });
      } else if (result.error?.code === "SSH_HOSTKEY_UNKNOWN") {
        toast.error("Host key SSH desconhecida", {
          description: "Abra um terminal SSH pra esta instância primeiro (aba VMs/EC2) e aceite a fingerprint — a transferência de arquivo nunca pergunta, só reaproveita o known_hosts já gravado.",
        });
      } else {
        toast.error("Erro ao transferir", { description: result.error?.message });
      }
    } catch (e) {
      toast.error("Erro ao transferir", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setTransferring(false);
    }
  };

  const canValidate = tlsCrt.trim() !== "" && tlsKey.trim() !== "" && !validating;
  // canTransfer — NÃO exige validateResult?.success: o backend já revalida o par cert+chave
  // antes de gravar (defesa em profundidade, ver TransferCertificate/TransferCertificateViaSSM),
  // então "Validar" (passo 4) é só uma conferência opcional pro usuário, nunca um pré-requisito
  // pra o botão "Transferir" sequer aparecer. Bug real corrigido, relatado pelo usuário: o botão
  // de transferir ficava 100% escondido até validar com sucesso — quem só colava/escolhia um
  // certificado (sem clicar em "Validar" antes) não via NENHUM botão de ação, parecendo que
  // faltava um jeito de enviar o certificado de verdade.
  const canTransfer =
    !!instance && tlsCrt.trim() !== "" && tlsKey.trim() !== "" && remoteCertPath.trim() !== "" && remoteKeyPath.trim() !== "" &&
    (connectionMode === "ssm-command" ? ssmCommandReady : !!credentialProfileId && (connectionMode === "ssh" ? host.trim() !== "" : !!tunnelSessionId));

  return (
    <div className="h-full flex flex-col min-h-0">
      <ScrollArea className="flex-1 min-h-0">
        <div className="p-1 space-y-5 max-w-3xl">
          <p className="text-xs text-muted-foreground">
            Valida um par certificado+chave, transfere pra uma VM real via SFTP e ajuda a confirmar que o reload
            (sempre manual, feito por você no terminal) surtiu efeito. Nunca reinicia nenhum serviço sozinho.
          </p>

          {/* 1. Instância */}
          <div className="space-y-2">
            <Label className="text-xs font-semibold">1. Instância</Label>
            <div className="flex flex-wrap items-end gap-2">
              <div className="space-y-1 min-w-[180px]">
                <Label className="text-xs text-muted-foreground">Profile AWS</Label>
                <Select value={awsProfile} onValueChange={(v) => { setAwsProfile(v); setInstanceId(""); }} disabled={loadingAwsProfiles}>
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue placeholder={loadingAwsProfiles ? "Carregando..." : "Selecione..."} />
                  </SelectTrigger>
                  <SelectContent>
                    {awsProfiles.map((p) => (
                      <SelectItem key={p} value={p} className="text-xs">{p}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1 min-w-[140px]">
                <Label className="text-xs text-muted-foreground">Região</Label>
                <Input className="h-8 text-xs" value={region} onChange={(e) => { setRegion(e.target.value); setInstanceId(""); }} />
              </div>
              <div className="flex gap-1">
                {REGION_SHORTCUTS.map((r) => (
                  <Button key={r} size="sm" variant={region === r ? "default" : "outline"} className="h-8 text-xs" onClick={() => { setRegion(r); setInstanceId(""); }}>
                    {r}
                  </Button>
                ))}
              </div>
              <Button size="sm" variant="outline" className="h-8 text-xs" disabled={!awsProfile || loadingInstances} onClick={() => refetchInstances({ refresh: true })}>
                {loadingInstances ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5 mr-1" />}
                Atualizar
              </Button>
            </div>

            {awsProfile && (
              <InstanceSelect value={instanceId} onChange={setInstanceId} instances={instances} loading={loadingInstances} />
            )}
          </div>

          {instance && (
            <>
              <Separator />

              {/* 2. Conexão (SSH direto / SSM túnel de VMSFTPModal.tsx, + SSM sem SSH — motivado
                  por um caso real: instância gerida só via SSM, sem sshd instalado — nem o túnel
                  resolve nesse caso, só encaminha TCP, ainda precisa de sshd do outro lado). */}
              <div className="space-y-2">
                <Label className="text-xs font-semibold">2. Conexão</Label>
                <div className="flex gap-1 flex-wrap">
                  <Button type="button" size="sm" variant={connectionMode === "ssh" ? "default" : "outline"} className="h-7 text-xs" onClick={() => setConnectionMode("ssh")}>
                    SSH direto
                  </Button>
                  <Button type="button" size="sm" variant={connectionMode === "ssm" ? "default" : "outline"} className="h-7 text-xs" onClick={() => setConnectionMode("ssm")}>
                    SSM (túnel até sshd)
                  </Button>
                  <Button
                    type="button" size="sm" variant={connectionMode === "ssm-command" ? "default" : "outline"} className="h-7 text-xs"
                    onClick={() => setConnectionMode("ssm-command")}
                  >
                    SSM (sem SSH)
                  </Button>
                </div>

                {connectionMode === "ssh" && (
                  <div className="flex gap-2 flex-wrap">
                    <div className="space-y-1">
                      <Label className="text-xs text-muted-foreground">Host</Label>
                      <Input className="h-8 text-xs w-48" value={host} onChange={(e) => setHost(e.target.value)} placeholder="IP ou hostname" />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs text-muted-foreground">Porta</Label>
                      <Input className="h-8 text-xs w-20" value={port} onChange={(e) => setPort(e.target.value)} />
                    </div>
                  </div>
                )}

                {connectionMode === "ssm" && (
                  <div className="flex items-end gap-2 flex-wrap">
                    <div className="space-y-1">
                      <Label className="text-xs text-muted-foreground">Porta remota (sshd)</Label>
                      <Input className="h-8 text-xs w-28" value={remotePort} onChange={(e) => setRemotePort(e.target.value)} />
                    </div>
                    {tunnelSessionId ? (
                      <span className="text-xs text-green-500 flex items-center gap-1 pb-1.5"><CheckCircle2 className="h-3.5 w-3.5" /> Túnel aberto</span>
                    ) : (
                      <Button size="sm" variant="outline" className="h-8 text-xs" disabled={tunnelStarting} onClick={handleStartSSMTunnel}>
                        {tunnelStarting && <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />}
                        Abrir túnel SSM
                      </Button>
                    )}
                  </div>
                )}

                {connectionMode === "ssm-command" && (
                  <p className="text-xs text-muted-foreground">
                    Sem SSH/sshd nenhum — os comandos rodam via AWS SSM Run Command, usando o profile "{awsProfile || "?"}"
                    e a região "{region}" já escolhidos no passo 1. Só precisa do agente SSM (o mesmo já exigido pelo
                    terminal). Navegação visual de arquivos não está disponível neste modo — digite o caminho direto.
                  </p>
                )}

                {isSSHBased && (
                  <div className="space-y-1 max-w-sm">
                    <Label className="text-xs text-muted-foreground">Perfil de credencial SSH</Label>
                    <Select value={credentialProfileId} onValueChange={setCredentialProfileId} disabled={loadingCredProfiles}>
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder={credProfiles.length === 0 ? "Nenhum perfil — cadastre em VMs/EC2" : "Selecione..."} />
                      </SelectTrigger>
                      <SelectContent>
                        {credProfiles.map((p) => (
                          <SelectItem key={p.id} value={p.id} className="text-xs">{p.name} ({p.username})</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}
              </div>

              <Separator />

              {/* 3. Certificado ATUAL na VM — responde "como vejo o que já está instalado": sem
                  isso, não havia nenhum caminho pra inspecionar um certificado existente, só pra
                  colar um novo às cegas. */}
              <div className="space-y-2">
                <Label className="text-xs font-semibold">3. Certificado atual na VM (opcional)</Label>
                <p className="text-xs text-muted-foreground">
                  Navegue pelos arquivos da VM ou digite o caminho direto pra ver o que já está instalado hoje.
                </p>
                <div className="flex gap-2 flex-wrap items-end">
                  <div className="space-y-1 flex-1 min-w-[240px]">
                    <Label className="text-xs text-muted-foreground">Caminho do certificado na VM</Label>
                    <Input
                      className="h-8 text-xs font-mono"
                      value={readPath}
                      onChange={(e) => setReadPath(e.target.value)}
                      placeholder="/etc/nginx/ssl/tls.crt"
                    />
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-8 text-xs"
                    disabled={!sftpConnectionReady}
                    title={
                      connectionMode === "ssm-command"
                        ? "Navegação visual não disponível no modo SSM sem SSH — digite o caminho direto"
                        : !sftpConnectionReady
                        ? "Preencha a conexão (passo 2) primeiro"
                        : "Navegar pelos arquivos da VM"
                    }
                    onClick={() => setBrowseTarget("readPath")}
                  >
                    <FolderOpen className="h-3.5 w-3.5 mr-1.5" />
                    Procurar na VM...
                  </Button>
                  <Button size="sm" disabled={!certActionsReady || !readPath.trim() || reading} onClick={handleReadCertificate}>
                    {reading ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <FileSearch className="h-3.5 w-3.5 mr-1.5" />}
                    Ler certificado
                  </Button>
                </div>

                {readResult && (
                  <div className="border rounded-md p-3 text-xs">
                    {readResult.success && readResult.certificate ? (
                      <CertInfoCard info={readResult.certificate} />
                    ) : (
                      <div className="flex items-center gap-2 text-red-500">
                        <ShieldAlert className="h-4 w-4" />
                        <span>{readResult.error?.message || "Não foi possível ler/parsear o arquivo"}</span>
                      </div>
                    )}
                  </div>
                )}
              </div>

              <Separator />

              {/* 4. Certificado NOVO a instalar */}
              <div className="space-y-2">
                <Label className="text-xs font-semibold">4. Novo certificado (a instalar)</Label>
                <div className="flex justify-end">
                  <Button type="button" variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setSourcePickerOpen(true)}>
                    <FolderOpen className="h-3.5 w-3.5 mr-1.5" />
                    Escolher de um backup...
                  </Button>
                </div>
                <div>
                  <div className="flex items-center justify-between">
                    <Label className="text-xs">Certificado (tls.crt — PEM)</Label>
                    {tlsCrt.trim() && (
                      <span className="text-[11px] text-muted-foreground">
                        {countPemCertificates(tlsCrt)} certificado(s){countPemCertificates(tlsCrt) > 1 && " (chain incluída)"}
                      </span>
                    )}
                  </div>
                  <textarea
                    value={tlsCrt}
                    onChange={(e) => setTlsCrt(e.target.value)}
                    placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                    className="w-full mt-1 h-28 p-2 text-xs font-mono bg-background border rounded resize-none"
                  />
                </div>
                <div>
                  <Label className="text-xs">Chave Privada (tls.key — PEM)</Label>
                  <textarea
                    value={tlsKey}
                    onChange={(e) => setTlsKey(e.target.value)}
                    placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                    className="w-full mt-1 h-28 p-2 text-xs font-mono bg-background border rounded resize-none"
                  />
                </div>

                <div className="space-y-1.5 pt-1">
                  <Label className="text-xs text-muted-foreground">
                    Verificar o que está sendo servido agora (opcional — dial TLS real, sem precisar de SSH). Aceita
                    host puro, host:porta ou a URL completa do site.
                  </Label>
                  <div className="flex gap-2 flex-wrap">
                    <Input
                      className="h-8 text-xs flex-1 min-w-[220px]"
                      value={checkTarget}
                      onChange={(e) => setCheckTarget(e.target.value)}
                      placeholder="https://exemplo.com ou exemplo.com:8443"
                    />
                    <Input className="h-8 text-xs w-40" value={checkSni} onChange={(e) => setCheckSni(e.target.value)} placeholder="SNI (opcional)" />
                  </div>
                </div>

                <div className="flex gap-2">
                  <Button size="sm" disabled={!canValidate} onClick={() => handleValidate(!!checkTarget.trim())}>
                    {validating && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
                    Validar {checkTarget.trim() ? "+ verificar ao vivo" : ""}
                  </Button>
                </div>

                {validateResult && (
                  <div className="border rounded-md p-3 space-y-2 text-xs">
                    {validateResult.success && validateResult.certificate ? (
                      <>
                        <div className="flex items-center gap-2">
                          <ShieldCheck className="h-4 w-4 text-green-500" />
                          <span className="font-medium">Par cert+chave válido</span>
                        </div>
                        <CertInfoCard info={validateResult.certificate} />
                      </>
                    ) : (
                      <div className="flex items-center gap-2 text-red-500">
                        <ShieldAlert className="h-4 w-4" />
                        <span>{validateResult.error?.message || "Par cert+chave inválido"}</span>
                      </div>
                    )}

                    {validateResult.live_check && (
                      <div className="pt-1.5 border-t space-y-1">
                        {validateResult.live_check.success ? (
                          <div className="flex items-center gap-2">
                            {validateResult.live_check.matches_target ? (
                              <>
                                <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />
                                <span className="text-green-500">Servidor já está servindo ESTE certificado.</span>
                              </>
                            ) : (
                              <>
                                <XCircle className="h-3.5 w-3.5 text-amber-500" />
                                <span className="text-amber-500">
                                  Servidor está servindo um certificado diferente (serial {validateResult.live_check.serial_number || "?"}) — reload ainda não foi feito.
                                </span>
                              </>
                            )}
                          </div>
                        ) : (
                          <span className="text-muted-foreground">Checagem ao vivo falhou: {validateResult.live_check.error_message}</span>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>

              {tlsCrt.trim() !== "" && tlsKey.trim() !== "" && (
                <>
                  <Separator />

                  {/* 5. Transferência — visível assim que há um certificado colado/escolhido,
                      SEM exigir ter clicado em "Validar" antes (o backend já revalida na hora de
                      gravar). Se o par for inválido, o próprio botão "Transferir" revela o erro
                      real via transferResult, sem deixar a ação escondida até então. */}
                  <div className="space-y-2">
                    <Label className="text-xs font-semibold">5. Transferir para a VM</Label>
                    {!validateResult && (
                      <p className="text-[11px] text-muted-foreground">
                        Dica: use "Validar" no passo acima pra conferir o certificado antes de enviar — mas não é
                        obrigatório, o servidor também confere antes de gravar.
                      </p>
                    )}
                    {readResult?.success && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-6 text-[11px] px-1.5"
                        onClick={() => { setRemoteCertPath(readPath); toast.info("Caminho do certificado atual reaproveitado como destino"); }}
                      >
                        <ArrowRight className="h-3 w-3 mr-1" />
                        Usar o mesmo caminho lido no passo 3
                      </Button>
                    )}
                    <div className="flex gap-2 flex-wrap items-end">
                      <div className="space-y-1 flex-1 min-w-[220px]">
                        <Label className="text-xs text-muted-foreground">Caminho remoto do certificado</Label>
                        <Input className="h-8 text-xs font-mono" value={remoteCertPath} onChange={(e) => setRemoteCertPath(e.target.value)} placeholder="/etc/nginx/ssl/tls.crt" />
                      </div>
                      <Button
                        size="sm" variant="outline" className="h-8 text-xs" disabled={!sftpConnectionReady}
                        title={connectionMode === "ssm-command" ? "Navegação visual não disponível no modo SSM sem SSH" : "Navegar pelos arquivos da VM"}
                        onClick={() => setBrowseTarget("remoteCertPath")}
                      >
                        <FolderOpen className="h-3.5 w-3.5" />
                      </Button>
                      <div className="space-y-1 flex-1 min-w-[220px]">
                        <Label className="text-xs text-muted-foreground">Caminho remoto da chave</Label>
                        <Input className="h-8 text-xs font-mono" value={remoteKeyPath} onChange={(e) => setRemoteKeyPath(e.target.value)} placeholder="/etc/nginx/ssl/tls.key" />
                      </div>
                      <Button
                        size="sm" variant="outline" className="h-8 text-xs" disabled={!sftpConnectionReady}
                        title={connectionMode === "ssm-command" ? "Navegação visual não disponível no modo SSM sem SSH" : "Navegar pelos arquivos da VM"}
                        onClick={() => setBrowseTarget("remoteKeyPath")}
                      >
                        <FolderOpen className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                    <ProtectedAction>
                      <Button size="sm" disabled={!canTransfer || transferring} onClick={handleTransfer}>
                        {transferring ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <Upload className="h-3.5 w-3.5 mr-1.5" />}
                        {connectionMode === "ssm-command" ? "Transferir via SSM" : "Transferir via SFTP"}
                      </Button>
                    </ProtectedAction>

                    {transferResult && (
                      <div className={`text-xs flex items-center gap-2 ${transferResult.success ? "text-green-500" : "text-red-500"}`}>
                        {transferResult.success ? <CheckCircle2 className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                        {transferResult.success
                          ? "cert_bytes_written" in transferResult
                            ? `Enviado: ${transferResult.cert_bytes_written} + ${transferResult.key_bytes_written} bytes`
                            : "Enviado com sucesso via SSM Run Command"
                          : transferResult.error?.message}
                      </div>
                    )}
                  </div>
                </>
              )}

              {transferResult?.success && (
                <>
                  <Separator />

                  {/* 6. Reload — sempre manual */}
                  <div className="space-y-2">
                    <Label className="text-xs font-semibold">6. Reload (manual) e confirmação</Label>
                    <p className="text-xs text-muted-foreground">
                      A transferência nunca reinicia nada sozinha. Abra o terminal e rode o comando de reload da sua
                      aplicação (ex: <code className="font-mono">systemctl reload nginx</code>), depois clique em
                      "Reconfirmar" abaixo pra verificar se o certificado servido já mudou.
                    </p>
                    <div className="flex gap-2">
                      <ProtectedAction>
                        <Button size="sm" variant="outline" onClick={() => setConnectOpen(true)}>
                          <TerminalIcon className="h-3.5 w-3.5 mr-1.5" />
                          Abrir terminal ({terminalMode.toUpperCase()})
                        </Button>
                      </ProtectedAction>
                      <Button size="sm" variant="outline" disabled={!checkTarget.trim() || validating} onClick={() => handleValidate(true)}>
                        {validating && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
                        Reconfirmar
                      </Button>
                    </div>
                  </div>
                </>
              )}
            </>
          )}
        </div>
      </ScrollArea>

      <CertificateSourcePickerModal
        open={sourcePickerOpen}
        onOpenChange={setSourcePickerOpen}
        cluster=""
        namespace=""
        secretName=""
        defaultTab="manual"
        onSelect={(crt, key) => {
          setTlsCrt(crt);
          setTlsKey(key);
          setValidateResult(null);
          setTransferResult(null);
        }}
      />

      {instance && (
        <VMConnectModal
          instance={instance}
          mode={terminalMode}
          profile={awsProfile}
          region={region}
          open={connectOpen}
          onClose={() => setConnectOpen(false)}
        />
      )}

      {instance && (
        <RemoteFileBrowserDialog
          open={browseTarget !== null}
          onOpenChange={(open) => { if (!open) setBrowseTarget(null); }}
          listUrl={sftpListUrl()}
          onPick={handlePickedPath}
        />
      )}
    </div>
  );
}
