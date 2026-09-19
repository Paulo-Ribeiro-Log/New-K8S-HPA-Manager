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
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
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
  VMServiceRestartResult,
} from "@/lib/api/types";
import type { CertificateInfo } from "@/types/certificates";

const REGION_SHORTCUTS = ["us-east-1", "us-east-2", "us-west-2", "sa-east-1"];

// COMMON_SERVICES — pedido explícito do usuário: seleção entre os gerenciadores de serviço mais
// comuns de mercado, em vez de exigir digitar o nome de cor toda vez. Value = nome real da unit
// systemd/init (o que de fato vai pro comando `systemctl restart <value>`/`service <value>
// restart`), Label = nome popular de exibição quando diverge do value técnico.
const COMMON_SERVICES: { value: string; label: string }[] = [
  { value: "nginx", label: "nginx" },
  { value: "apache2", label: "Apache (apache2 — Debian/Ubuntu)" },
  { value: "httpd", label: "Apache (httpd — RHEL/Amazon Linux)" },
  { value: "haproxy", label: "HAProxy" },
  { value: "envoy", label: "Envoy" },
  { value: "caddy", label: "Caddy" },
  { value: "squid", label: "Squid" },
  { value: "tomcat", label: "Tomcat" },
  { value: "mysqld", label: "MySQL (mysqld)" },
  { value: "mariadb", label: "MariaDB" },
  { value: "postgresql", label: "PostgreSQL" },
  { value: "docker", label: "Docker" },
];
const CUSTOM_SERVICE_VALUE = "__custom__";

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

  // confirmPathInput — BUG REAL CORRIGIDO, relatado ao vivo pelo usuário ("nada pode ser
  // selecionado lá"): clicar numa linha só funciona pra escolher um arquivo que JÁ EXISTE na VM —
  // certo pro passo 3 (ler o que já está instalado, que por definição precisa existir), mas o
  // passo 5 (destino de um certificado NOVO) quase sempre aponta pra um caminho que ainda não
  // existe (pasta vazia ou com outro nome de arquivo) — a listagem nunca tem nada clicável que
  // corresponda ao destino desejado, travando a escolha por completo. Barra de rodapé sempre
  // visível: mostra/edita o caminho completo (pasta atual + nome de arquivo digitado), com um
  // botão "Usar este caminho" que devolve esse valor via onPick mesmo sem o arquivo existir —
  // clicar numa linha da listagem continua funcionando igual (atalho pro caso comum de já existir).
  const [confirmPathInput, setConfirmPathInput] = useState("/");

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

  // Sincroniza a barra "Usar este caminho" com a pasta atual a cada navegação (clique no
  // breadcrumb ou numa subpasta) — o usuário só precisa completar com o nome do arquivo.
  useEffect(() => {
    setConfirmPathInput(path);
  }, [path]);

  const handleConfirmPath = () => {
    const trimmed = confirmPathInput.trim();
    if (!trimmed) return;
    onPick(trimmed.startsWith("/") ? trimmed : `/${trimmed}`);
    onOpenChange(false);
  };

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
                <div className="flex flex-col items-center justify-center h-full gap-1 text-center px-4">
                  <p className="text-sm text-muted-foreground">Pasta vazia.</p>
                  <p className="text-xs text-muted-foreground">
                    Se o arquivo ainda não existe (destino de uma instalação nova), digite o caminho completo no campo
                    abaixo e clique em "Usar este caminho".
                  </p>
                </div>
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

            <div className="flex items-center gap-2 flex-shrink-0 pt-1 border-t">
              <Input
                className="h-8 text-xs font-mono flex-1"
                value={confirmPathInput}
                onChange={(e) => setConfirmPathInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") handleConfirmPath(); }}
                placeholder="/caminho/completo/do/arquivo"
              />
              <Button size="sm" disabled={!confirmPathInput.trim()} onClick={handleConfirmPath}>
                Usar este caminho
              </Button>
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

  // Restart de serviço (nginx/apache2/httpd/haproxy/etc.) — pedido explícito do usuário: um botão
  // dedicado em vez de exigir abrir o terminal (que continua existindo como alternativa manual,
  // mais abaixo). serviceSelectValue guarda o valor do <Select> (um dos COMMON_SERVICES OU
  // CUSTOM_SERVICE_VALUE); serviceNameCustom só é usado/mostrado quando o valor selecionado é
  // "Personalizado...".
  const [serviceSelectValue, setServiceSelectValue] = useState(COMMON_SERVICES[0].value);
  const [serviceNameCustom, setServiceNameCustom] = useState("");
  const [initSystem, setInitSystem] = useState<"systemd" | "sysv">("systemd");
  const [restarting, setRestarting] = useState(false);
  const [restartResult, setRestartResult] = useState<VMServiceRestartResult | null>(null);
  const effectiveServiceName = (serviceSelectValue === CUSTOM_SERVICE_VALUE ? serviceNameCustom : serviceSelectValue).trim();

  // confirmRestartOpen — pedido explícito do usuário: "implemente a verificação e aprovação do uso
  // do botão de reinício do serviço". Reiniciar um serviço real pode derrubar tráfego em produção
  // por alguns segundos — clicar em "Reiniciar serviço" não dispara mais o restart direto, só abre
  // um AlertDialog mostrando exatamente o que vai rodar (instância + serviço + comando exato) pra
  // o usuário confirmar antes. Mesmo padrão já usado pras ações de energia da instância em
  // VMsTab.tsx (pendingAction/AlertDialog). Um `false` reaproveitado — não é um "retry" de host key
  // desconhecida (handleTrustHostKeyMain chama handleRestartService direto, sem reabrir este
  // diálogo — é a MESMA ação já aprovada uma vez, só reenviada depois de confiar na fingerprint).
  const [confirmRestartOpen, setConfirmRestartOpen] = useState(false);

  // "Certificado atual na VM" — leitura via SFTP de um arquivo JÁ instalado (ReadRemoteCertificate),
  // pra responder "como vejo o que já está lá" sem precisar já saber o caminho de cor. browseTarget
  // decide pra qual campo (readPath/remoteCertPath/remoteKeyPath) o caminho escolhido no navegador
  // embutido (RemoteFileBrowserDialog) deve ir.
  const [readPath, setReadPath] = useState("");
  const [reading, setReading] = useState(false);
  const [readResult, setReadResult] = useState<VMCertReadResult | null>(null);
  const [browseTarget, setBrowseTarget] = useState<"readPath" | "remoteCertPath" | "remoteKeyPath" | null>(null);

  // Confirmação de host key SSH desconhecida (TOFU), agora TAMBÉM nas ações "Ler certificado" e
  // "Transferir" — BUG REAL CORRIGIDO, relatado ao vivo pelo usuário ("o envio de arquivos ainda
  // não está funcionando, assim como os botões de procurar na vm"): só RemoteFileBrowserDialog
  // tinha essa confirmação inline; handleReadCertificate/handleTransfer só mostravam um toast
  // pedindo pra abrir o terminal SSH (aba VMs/EC2) e aceitar a fingerprint lá — tecnicamente
  // funciona (mesmo vm_known_hosts, ver internal/vmssh/knownhosts.go), mas exige sair do painel
  // sem nenhum contexto do que se estava tentando fazer, indistinguível de "não funciona" pra
  // quem nunca leu o toast até o fim. Mesmo mecanismo agora aqui: aceitar grava no known_hosts
  // (persistido no servidor) e relança a MESMA ação que falhou.
  const [pendingHostKeyFingerprint, setPendingHostKeyFingerprint] = useState<string | null>(null);
  const [pendingHostKeyAction, setPendingHostKeyAction] = useState<"read" | "transfer" | "restart" | null>(null);
  const [acceptedHostKeyFingerprint, setAcceptedHostKeyFingerprint] = useState<string | null>(null);

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

  // fingerprintOverride — mesmo motivo de RemoteFileBrowserDialog: setState é assíncrono, então o
  // acceptedHostKeyFingerprint recém-aceito ainda não estaria visível na mesma chamada que o
  // relança (handleTrustHostKeyMain) via closure de estado, sem passar explicitamente.
  const targetParams = (fingerprintOverride?: string) => {
    const base =
      connectionMode === "ssm"
        ? { tunnelSessionId: tunnelSessionId ?? "" }
        : { host: host.trim(), port: Number(port) || 22 };
    const fp = fingerprintOverride ?? acceptedHostKeyFingerprint;
    return fp ? { ...base, acceptHostKeyFingerprint: fp } : base;
  };

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

  // browseSSMUrl — BUG REAL CORRIGIDO, relatado ao vivo pelo usuário: no modo "SSM (sem SSH)", o
  // botão "Procurar na VM" ficava SEMPRE desabilitado (não existe SFTP sem sshd) — sem nenhuma
  // pista clara na tela, dava a impressão de que a ferramenta inteira "não conecta", mesmo o
  // caminho já existindo de verdade (caso mais comum: ATUALIZAR um certificado, não instalar um
  // novo). Endpoint novo (ListDirectoryViaSSM, certificates_vm.go) lista via `find` sobre SSM Run
  // Command, mesmo shape de resposta do /sftp/list — RemoteFileBrowserDialog funciona sem nenhuma
  // mudança interna, só aponta pra uma URL diferente.
  const browseSSMUrl = () => {
    if (!instance) return "";
    const params = new URLSearchParams({ profile: awsProfile, region });
    return `/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/browse-ssm?${params.toString()}`;
  };

  const handlePickedPath = (path: string) => {
    if (browseTarget === "remoteCertPath") setRemoteCertPath(path);
    else if (browseTarget === "remoteKeyPath") setRemoteKeyPath(path);
    else setReadPath(path);
    setBrowseTarget(null);
  };

  const handleReadCertificate = async (fingerprintOverride?: string) => {
    if (!instance || !readPath.trim()) return;
    setReading(true);
    setReadResult(null);
    try {
      let resp: Response;
      if (connectionMode === "ssm-command") {
        const params = new URLSearchParams({ profile: awsProfile, region, path: readPath.trim() });
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/read-ssm?${params.toString()}`);
      } else {
        const params = new URLSearchParams({ ...targetParams(fingerprintOverride), credentialProfileId, path: readPath.trim() } as Record<string, string>);
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/read?${params.toString()}`);
      }
      const result = await parseJSON<VMCertReadResult>(resp);
      if (!result.success && result.error?.code === "SSH_HOSTKEY_UNKNOWN" && result.error.fingerprint) {
        setPendingHostKeyFingerprint(result.error.fingerprint);
        setPendingHostKeyAction("read");
        return;
      }
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

  const handleTransfer = async (fingerprintOverride?: string) => {
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
            ...targetParams(fingerprintOverride),
            credentialProfileId,
            certPem: tlsCrt,
            keyPem: tlsKey,
            remoteCertPath: remoteCertPath.trim(),
            remoteKeyPath: remoteKeyPath.trim(),
          }),
        });
      }
      const result = await parseJSON<VMCertTransferResult | VMCertTransferSSMResult>(resp);
      // "fingerprint" in result.error — SSH_HOSTKEY_UNKNOWN só existe no caminho SFTP
      // (VMCertTransferResult); o caminho SSM (VMCertTransferSSMResult) nunca tem esse campo, já
      // que não envolve SSH algum — narrowing explícito em vez de um cast, TS não infere sozinho.
      if (!result.success && result.error?.code === "SSH_HOSTKEY_UNKNOWN" && "fingerprint" in result.error && result.error.fingerprint) {
        setPendingHostKeyFingerprint(result.error.fingerprint);
        setPendingHostKeyAction("transfer");
        return;
      }
      setTransferResult(result);
      if (result.success) {
        toast.success("Certificado transferido para a VM", {
          description: `${remoteCertPath.trim()} + ${remoteKeyPath.trim()}`,
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

  // handleRestartService — pedido explícito do usuário: botão dedicado pra reiniciar o serviço
  // (nginx/apache2/httpd/haproxy/etc.) sem precisar abrir o terminal e digitar o comando na mão
  // (que continua disponível logo abaixo, como alternativa 100% manual). Mesmos 3 transportes já
  // usados por handleReadCertificate/handleTransfer — SSM Run Command no modo sem SSH, SSH/SFTP
  // (RunCommand sobre a mesma sessão já resolvida) nos outros dois.
  const handleRestartService = async (fingerprintOverride?: string) => {
    if (!instance) return;
    const svc = effectiveServiceName;
    if (!svc) {
      toast.error("Selecione ou digite o nome do serviço");
      return;
    }
    if (isSSHBased && !credentialProfileId) {
      toast.error("Selecione um perfil de credencial SSH");
      return;
    }
    setRestarting(true);
    setRestartResult(null);
    try {
      let resp: Response;
      if (connectionMode === "ssm-command") {
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/restart-service-ssm`, {
          method: "POST",
          body: JSON.stringify({ profile: awsProfile, region, serviceName: svc, initSystem }),
        });
      } else {
        resp = await apiFetch(`/api/v1/vms/${encodeURIComponent(instance.id)}/certificates/restart-service`, {
          method: "POST",
          body: JSON.stringify({ ...targetParams(fingerprintOverride), credentialProfileId, serviceName: svc, initSystem }),
        });
      }
      const result = await parseJSON<VMServiceRestartResult>(resp);
      if (!result.success && result.error?.code === "SSH_HOSTKEY_UNKNOWN" && result.error.fingerprint) {
        setPendingHostKeyFingerprint(result.error.fingerprint);
        setPendingHostKeyAction("restart");
        return;
      }
      setRestartResult(result);
      if (result.success) {
        toast.success(`Serviço "${svc}" reiniciado`, { description: result.status ? `status: ${result.status}` : undefined });
      } else {
        toast.error("Erro ao reiniciar serviço", { description: result.error?.message });
      }
    } catch (e) {
      toast.error("Erro ao reiniciar serviço", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setRestarting(false);
    }
  };

  // restartPreviewCommand — só pra EXIBIÇÃO no AlertDialog de confirmação (nunca enviado ao
  // backend, que sempre monta e escapa o comando de verdade server-side via ShellQuote,
  // restartServiceCommand em certificates_vm.go) — deixa claro pro usuário exatamente o que vai
  // rodar antes de aprovar.
  const restartPreviewCommand = () => {
    const svc = effectiveServiceName || "<serviço>";
    return initSystem === "sysv" ? `sudo -n service '${svc}' restart` : `sudo -n systemctl restart '${svc}'`;
  };

  const handleConfirmRestart = () => {
    setConfirmRestartOpen(false);
    void handleRestartService();
  };

  const handleTrustHostKeyMain = () => {
    if (!pendingHostKeyFingerprint || !pendingHostKeyAction) return;
    const fingerprint = pendingHostKeyFingerprint;
    const action = pendingHostKeyAction;
    setAcceptedHostKeyFingerprint(fingerprint);
    setPendingHostKeyFingerprint(null);
    setPendingHostKeyAction(null);
    if (action === "read") void handleReadCertificate(fingerprint);
    else if (action === "transfer") void handleTransfer(fingerprint);
    else void handleRestartService(fingerprint);
  };

  const handleRejectHostKeyMain = () => {
    setPendingHostKeyFingerprint(null);
    setPendingHostKeyAction(null);
    toast.error("Host key rejeitada — a operação não pode continuar sem confiar nela.");
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

  // Carregar do PC do usuário — BUG REAL CORRIGIDO, relatado ao vivo pelo usuário: até aqui o
  // único jeito de preencher o certificado NOVO era colar manualmente ou escolher de uma fonte já
  // salva no SERVIDOR (CertificateSourcePickerModal — rollback/backup apartado/PFX extraído, nada
  // disso é o arquivo do PC do usuário). "Procurar na VM" (RemoteFileBrowserDialog) também não
  // serve pra isso — navega arquivos JÁ na VM, é o caminho contrário (usado pra "ler o que já está
  // lá" ou escolher o CAMINHO de destino remoto, nunca a origem do conteúdo local). Lê o arquivo
  // inteiramente no browser (FileReader, nunca sobe pro servidor antes de "Transferir" — mesma
  // origem de dado que colar manualmente, só que sem exigir copiar/colar) e preenche a textarea
  // correspondente, exatamente como se o usuário tivesse colado o texto.
  const certFileInputRef = useRef<HTMLInputElement>(null);
  const keyFileInputRef = useRef<HTMLInputElement>(null);

  const readLocalFileInto = (file: File, setter: (v: string) => void) => {
    const reader = new FileReader();
    reader.onload = () => {
      setter(String(reader.result ?? ""));
      setValidateResult(null);
      setTransferResult(null);
    };
    reader.onerror = () => toast.error("Erro ao ler o arquivo", { description: file.name });
    reader.readAsText(file);
  };

  const handleLocalCertFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (file) readLocalFileInto(file, setTlsCrt);
  };

  const handleLocalKeyFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (file) readLocalFileInto(file, setTlsKey);
  };

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
                    terminal). "Procurar na VM" também funciona aqui — lista via um comando remoto (find), não SFTP.
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

              {/* Confirmação de host key SSH desconhecida (TOFU) — dispara quando "Ler
                  certificado" (passo 3), "Transferir" (passo 5) ou "Reiniciar serviço" (passo 6)
                  batem em SSH_HOSTKEY_UNKNOWN numa instância nunca antes acessada por este app. Ver
                  comentário no state pendingHostKeyFingerprint acima. */}
              {pendingHostKeyFingerprint && (
                <div className="space-y-3 border border-amber-500/40 rounded-md p-4 bg-amber-500/5">
                  <div className="flex items-center gap-2 text-amber-500">
                    <ShieldAlert className="h-4 w-4" />
                    <span className="text-sm font-medium">Host key SSH desconhecida</span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Esta é a primeira conexão com esta instância. Confirme a fingerprint com o administrador da VM
                    antes de aceitar — aceitar uma fingerprint errada pode expor suas credenciais a um ataque MITM.
                  </p>
                  <code className="block px-2 py-1 rounded bg-muted text-xs font-mono break-all">{pendingHostKeyFingerprint}</code>
                  <div className="flex justify-end gap-2">
                    <Button variant="outline" size="sm" onClick={handleRejectHostKeyMain}>Rejeitar</Button>
                    <Button size="sm" onClick={handleTrustHostKeyMain}>Confiar e continuar</Button>
                  </div>
                </div>
              )}

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
                    disabled={!certActionsReady}
                    title={certActionsReady ? "Navegar pelos arquivos da VM" : "Preencha a conexão (passo 2) primeiro"}
                    onClick={() => setBrowseTarget("readPath")}
                  >
                    <FolderOpen className="h-3.5 w-3.5 mr-1.5" />
                    Procurar na VM...
                  </Button>
                  <Button size="sm" disabled={!certActionsReady || !readPath.trim() || reading} onClick={() => handleReadCertificate()}>
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
                    <div className="flex items-center gap-2">
                      {tlsCrt.trim() && (
                        <span className="text-[11px] text-muted-foreground">
                          {countPemCertificates(tlsCrt)} certificado(s){countPemCertificates(tlsCrt) > 1 && " (chain incluída)"}
                        </span>
                      )}
                      <Button type="button" variant="ghost" size="sm" className="h-6 text-[11px] px-1.5" onClick={() => certFileInputRef.current?.click()}>
                        <Upload className="h-3 w-3 mr-1" /> Carregar do PC
                      </Button>
                      <input ref={certFileInputRef} type="file" className="hidden" onChange={handleLocalCertFile} />
                    </div>
                  </div>
                  <textarea
                    value={tlsCrt}
                    onChange={(e) => setTlsCrt(e.target.value)}
                    placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                    className="w-full mt-1 h-28 p-2 text-xs font-mono bg-background border rounded resize-none"
                  />
                </div>
                <div>
                  <div className="flex items-center justify-between">
                    <Label className="text-xs">Chave Privada (tls.key — PEM)</Label>
                    <div className="flex items-center gap-2">
                      <Button type="button" variant="ghost" size="sm" className="h-6 text-[11px] px-1.5" onClick={() => keyFileInputRef.current?.click()}>
                        <Upload className="h-3 w-3 mr-1" /> Carregar do PC
                      </Button>
                      <input ref={keyFileInputRef} type="file" className="hidden" onChange={handleLocalKeyFile} />
                    </div>
                  </div>
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
                        size="sm" variant="outline" className="h-8 text-xs" disabled={!certActionsReady}
                        title={certActionsReady ? "Navegar pelos arquivos da VM" : "Preencha a conexão (passo 2) primeiro"}
                        onClick={() => setBrowseTarget("remoteCertPath")}
                      >
                        <FolderOpen className="h-3.5 w-3.5" />
                      </Button>
                      <div className="space-y-1 flex-1 min-w-[220px]">
                        <Label className="text-xs text-muted-foreground">Caminho remoto da chave</Label>
                        <Input className="h-8 text-xs font-mono" value={remoteKeyPath} onChange={(e) => setRemoteKeyPath(e.target.value)} placeholder="/etc/nginx/ssl/tls.key" />
                      </div>
                      <Button
                        size="sm" variant="outline" className="h-8 text-xs" disabled={!certActionsReady}
                        title={certActionsReady ? "Navegar pelos arquivos da VM" : "Preencha a conexão (passo 2) primeiro"}
                        onClick={() => setBrowseTarget("remoteKeyPath")}
                      >
                        <FolderOpen className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                    <ProtectedAction>
                      <Button size="sm" disabled={!canTransfer || transferring} onClick={() => handleTransfer()}>
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

              {/* 6. Reload — BUG REAL CORRIGIDO, relatado ao vivo pelo usuário ("o botão de
                  executar o restart do serviço não está exibido"): esta seção inteira estava
                  presa atrás de `transferResult?.success` — só aparecia DEPOIS de uma transferência
                  bem-sucedida NESTA MESMA sessão do painel, mesmo restart de serviço sendo uma ação
                  independente na prática (confirmar/testar o que já está instalado, sem
                  necessariamente ter acabado de transferir nada novo agora). Passou a acompanhar só
                  o `instance && (...)` de fora (mesmo nível de steps 2/3) — sempre visível assim que
                  a instância está selecionada; o próprio botão já fica desabilitado (com o motivo
                  no title) enquanto a conexão do passo 2 não estiver pronta. */}
              <>
                <Separator />

                <div className="space-y-2">
                    <Label className="text-xs font-semibold">6. Reiniciar serviço e confirmar</Label>
                    <p className="text-xs text-muted-foreground">
                      Ação independente das anteriores — funciona mesmo sem ter transferido um certificado novo nesta
                      sessão (ex: só confirmar/testar o que já está instalado). Escolha o serviço e clique em
                      "Reiniciar serviço" — roda <code className="font-mono mx-1">systemctl restart</code> (ou
                      <code className="font-mono">service ... restart</code>, pra init clássico) direto na VM, sem
                      precisar abrir terminal nenhum.
                    </p>
                    <div className="flex gap-2 flex-wrap items-end">
                      <div className="space-y-1 min-w-[220px]">
                        <Label className="text-xs text-muted-foreground">Serviço</Label>
                        <Select value={serviceSelectValue} onValueChange={setServiceSelectValue}>
                          <SelectTrigger className="h-8 text-xs">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {COMMON_SERVICES.map((s) => (
                              <SelectItem key={s.value} value={s.value} className="text-xs">{s.label}</SelectItem>
                            ))}
                            <SelectItem value={CUSTOM_SERVICE_VALUE} className="text-xs">Personalizado (digitar)...</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                      {serviceSelectValue === CUSTOM_SERVICE_VALUE && (
                        <div className="space-y-1 min-w-[180px]">
                          <Label className="text-xs text-muted-foreground">Nome exato do serviço</Label>
                          <Input
                            className="h-8 text-xs font-mono"
                            value={serviceNameCustom}
                            onChange={(e) => setServiceNameCustom(e.target.value)}
                            placeholder="ex: my-app.service"
                          />
                        </div>
                      )}
                      <div className="space-y-1">
                        <Label className="text-xs text-muted-foreground">Gerenciador</Label>
                        <div className="flex gap-1">
                          <Button type="button" size="sm" variant={initSystem === "systemd" ? "default" : "outline"} className="h-8 text-xs" onClick={() => setInitSystem("systemd")}>
                            systemd
                          </Button>
                          <Button type="button" size="sm" variant={initSystem === "sysv" ? "default" : "outline"} className="h-8 text-xs" onClick={() => setInitSystem("sysv")}>
                            SysV/init
                          </Button>
                        </div>
                      </div>
                      <ProtectedAction>
                        <Button
                          size="sm"
                          disabled={!certActionsReady || !effectiveServiceName || restarting}
                          title={!certActionsReady ? "Preencha a conexão (passo 2) primeiro" : !effectiveServiceName ? "Escolha ou digite o nome do serviço" : undefined}
                          onClick={() => setConfirmRestartOpen(true)}
                        >
                          {restarting ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5 mr-1.5" />}
                          Reiniciar serviço
                        </Button>
                      </ProtectedAction>
                    </div>

                    {restartResult && (
                      <div className={`text-xs flex items-center gap-2 ${restartResult.success ? "text-green-500" : "text-red-500"}`}>
                        {restartResult.success ? <CheckCircle2 className="h-3.5 w-3.5 flex-shrink-0" /> : <XCircle className="h-3.5 w-3.5 flex-shrink-0" />}
                        {restartResult.success
                          ? `Reiniciado com sucesso${restartResult.status ? ` — status: ${restartResult.status}` : ""}`
                          : restartResult.error?.message || "Falha ao reiniciar o serviço"}
                      </div>
                    )}

                    <p className="text-xs text-muted-foreground pt-1">
                      Alternativa 100% manual: abra o terminal e rode o comando você mesmo (útil quando o serviço não
                      está na lista, ou quando o reload correto não é um restart completo).
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
          listUrl={connectionMode === "ssm-command" ? browseSSMUrl() : sftpListUrl()}
          onPick={handlePickedPath}
        />
      )}

      <AlertDialog open={confirmRestartOpen} onOpenChange={setConfirmRestartOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmar reinício do serviço?</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-2 text-sm text-muted-foreground">
                <p>
                  Isso vai reiniciar o serviço <strong className="text-foreground">{effectiveServiceName}</strong> na
                  instância <strong className="text-foreground">{instance?.name}</strong> ({instance?.id}) — pode causar
                  interrupção breve no tráfego servido por ele.
                </p>
                <code className="block px-2 py-1 rounded bg-muted text-xs font-mono break-all text-foreground">
                  {restartPreviewCommand()}
                </code>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmRestart}>Confirmar e reiniciar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
