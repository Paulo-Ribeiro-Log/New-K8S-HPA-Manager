import { useCallback, useEffect, useRef, useState } from "react";
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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import {
  FolderOpen,
  Folder,
  File,
  FolderPlus,
  Upload,
  Download,
  Pencil,
  Trash2,
  Loader2,
  ChevronRight,
  Home,
  RefreshCw,
  Check,
  ChevronsUpDown,
  Plus,
  PlugZap,
  ShieldAlert,
} from "lucide-react";
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import { formatBytes } from "@/lib/monitorUtils";
import { useVMCredentialProfiles } from "@/hooks/useVMs";
import { apiClient } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import VMCredentialsModal from "@/components/VMCredentialsModal";
import type { VMInstance, SSHCredentialProfile } from "@/lib/api/types";

// ProfileSelect — combobox Popover+Command (não Radix <Select>), mesmo componente/motivo já
// documentado em VMTerminalModal.tsx: o usuário relatou que o <Select> nativo não expandia o
// dropdown ao clicar dentro deste modal; Popover+Command é o padrão já validado nesta app pra
// combobox dentro de modal (ver SimpleSearchableSelect em PortForwardModal.tsx).
function ProfileSelect({
  value,
  onChange,
  profiles,
  disabled,
}: {
  value: string;
  onChange: (id: string) => void;
  profiles: SSHCredentialProfile[];
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const selected = profiles.find((p) => p.id === value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal"
        >
          <span className="truncate">
            {selected ? `${selected.name} (${selected.username})` : "Selecione um perfil..."}
          </span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder="Buscar perfil..." />
          <CommandList>
            <CommandEmpty>Nenhum perfil encontrado.</CommandEmpty>
            <CommandGroup>
              {profiles.map((p) => (
                <CommandItem
                  key={p.id}
                  value={`${p.name} ${p.username}`}
                  onSelect={() => {
                    onChange(p.id);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === p.id ? "opacity-100" : "opacity-0")} />
                  {p.name} <span className="text-muted-foreground ml-1">({p.username})</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

interface SFTPFileEntry {
  name: string;
  path: string;
  size: number;
  is_dir: boolean;
  mod_time: string;
  mode: string;
}

interface VMSFTPModalProps {
  instance: VMInstance;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // profile/region — só usados no modo SSM (túnel de port-forwarding); mesmo profile/região AWS
  // já usados pra listar a instância na aba VMs/EC2 (ver VMsTab.tsx).
  profile?: string;
  region?: string;
  // initialConnectionMode — pré-seleciona o modo de conexão no formulário (o usuário continua
  // livre pra trocar); "Arquivos (SFTP)" na aba VMs/EC2 é hoje o único ponto de entrada deste
  // modal, deliberadamente separado do fluxo "SSH"/"SSM" (ver VMConnectModal.tsx) — esta tela
  // continua existindo só pra transferência de arquivo de verdade (upload/download/renomear/
  // excluir). "ssm-command" (pedido explícito do usuário: "na lista das VMs... a opção de SSM
  // (sem sshd) não existe") — via SSM Run Command (vm_sftp_ssm.go), pra instâncias sem sshd
  // algum: sem exigir credencial SSH nenhuma, mas com teto de 40KB por arquivo (documento SSM
  // SendCommand tem limite real de 64KB) — arquivos maiores exigem SSH direto ou SSM (túnel).
  initialConnectionMode?: "ssh" | "ssm" | "ssm-command";
}

function authToken(): string {
  return localStorage.getItem("auth_token") ?? "";
}

// ApiFetchError — carrega code/fingerprint estruturados do corpo de erro (ver
// internal/web/handlers/vm_sftp.go, SSH_HOSTKEY_UNKNOWN) além da mensagem — sem isso, o modal
// nunca teria como distinguir "host key desconhecida, mostre a fingerprint pro usuário confirmar"
// de qualquer outro erro genérico (rede fora do ar, credencial errada, etc.).
class ApiFetchError extends Error {
  code?: string;
  fingerprint?: string;
}

async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string> | undefined) };
  const token = authToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const resp = await fetch(url, { ...init, headers });
  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`;
    let code: string | undefined;
    let fingerprint: string | undefined;
    try {
      const body = await resp.json();
      msg = body?.error?.message || body?.error || msg;
      code = body?.error?.code;
      fingerprint = body?.error?.fingerprint;
    } catch {
      // corpo não era JSON — mantém a mensagem genérica
    }
    const err = new ApiFetchError(msg);
    err.code = code;
    err.fingerprint = fingerprint;
    throw err;
  }
  return resp;
}

// throwIfFailed — BUG REAL, corrigido antes de ir pro ar: os handlers *-ssm (vm_sftp_ssm.go)
// seguem a MESMA convenção já usada em certificates_vm.go pra falha "lógica" (comando SSM rodou,
// mas o resultado é um erro — ex: caminho inexistente na VM) — HTTP 200 com `success:false` no
// corpo, reservando status != 2xx só pra falha de TRANSPORTE (SSM_COMMAND_ERROR). Os handlers SFTP
// (SSH) equivalentes, ao contrário, sempre usam status != 2xx pros dois casos — por isso apiFetch()
// acima nunca lança nada quando resp.ok é true, mesmo se o corpo disser success:false. Sem essa
// checagem extra, qualquer falha lógica no modo "SSM sem SSH" (ex: "Pasta vazia" mostrado por
// engano quando na real o comando falhou) passaria batido como sucesso silencioso.
function throwIfFailed(data: { success?: boolean; error?: { code?: string; message?: string; fingerprint?: string } }): void {
  if (data && data.success === false) {
    const err = new ApiFetchError(data.error?.message || "Operação falhou");
    err.code = data.error?.code;
    err.fingerprint = data.error?.fingerprint;
    throw err;
  }
}

function formatModTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("pt-BR");
  } catch {
    return iso;
  }
}

// VMSFTPModal — navegador de arquivos SFTP contra uma VM/instância real (Fase 4 do plano em
// /home/paulo/.claude/plans/scalable-greeting-kazoo.md). Modelado explicitamente em cima de
// PodSFTPModal.tsx (mesma UX/estrutura de navegação/ações) — diferença real é só a camada de
// transporte por baixo: aqui é SFTP nativo de verdade sobre SSH (internal/vmssh/sftp.go), não o
// servidor-em-memória-sobre-kubectl-exec do internal/podsftp (que só existe porque um Pod K8s não
// tem SFTP nativo).
//
// Modo SSM (pedido explícito do usuário: "como vou fazer sftp usando a conexão ssm?") — SSM
// Session Manager não fala SFTP nativamente; a técnica real é um túnel de port-forwarding
// (aws ssm start-session --document-name AWS-StartPortForwardingSession) encaminhando uma porta
// local pra porta remota (sshd) da instância através do canal criptografado do SSM, com SSH/SFTP
// falando normalmente ATRAVÉS desse túnel (mesma credencial de sempre). Diferente do modo SSH
// direto (host:porta reais, sem estado entre operações), o túnel PRECISA ficar vivo enquanto o
// modal está aberto — start explícito ao conectar, stop explícito ao fechar/desconectar.
export function VMSFTPModal({ instance, open, onOpenChange, profile, region, initialConnectionMode }: VMSFTPModalProps) {
  const { profiles, loading: loadingProfiles, refetch: refetchProfiles } = useVMCredentialProfiles();
  const [credentialsModalOpen, setCredentialsModalOpen] = useState(false);

  const [phase, setPhase] = useState<"form" | "browsing">("form");
  const [connectionMode, setConnectionMode] = useState<"ssh" | "ssm" | "ssm-command">("ssh");
  const [host, setHost] = useState(instance.publicIp || instance.privateIp || "");
  const [port, setPort] = useState("22");
  const [profileId, setProfileId] = useState("");

  // Modo SSM — porta REMOTA (sshd na instância, quase sempre 22; diferente de "port" acima, que
  // no modo SSH é a porta real da VM); tunnelSessionId só existe DEPOIS que o túnel abre com
  // sucesso, e é o que openSFTPSession usa pra resolver o endereço local do túnel.
  const [remotePort, setRemotePort] = useState("22");
  const [tunnelSessionId, setTunnelSessionId] = useState<string | null>(null);
  const [tunnelStarting, setTunnelStarting] = useState(false);

  // Confirmação de host key desconhecida (TOFU) — rota REST pura, sem canal interativo (ver
  // comentário de openSFTPSession em vm_sftp.go). Quando uma operação falha com
  // SSH_HOSTKEY_UNKNOWN, `hostKeyToConfirm` guarda a fingerprint pra mostrar num prompt; ao
  // aceitar, `acceptedHostKeyFingerprint` é setada e a MESMA operação é refeita passando
  // acceptHostKeyFingerprint=<fp> na query — que faz o backend aceitar e gravar em
  // vm_known_hosts, sem perguntar de novo dali em diante (mesma identidade estável, ver
  // hostKeyIdentity em vm_sftp.go).
  const [hostKeyToConfirm, setHostKeyToConfirm] = useState<string | null>(null);
  const [acceptedHostKeyFingerprint, setAcceptedHostKeyFingerprint] = useState<string | null>(null);

  const [path, setPath] = useState("/");
  const [entries, setEntries] = useState<SFTPFileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Edição manual do caminho — pedido explícito do usuário: o breadcrumb (clique-a-clique) não
  // basta quando já se sabe o caminho exato de cor (ex: "/etc/nginx/"). Alterna entre breadcrumb
  // (padrão) e um <Input> de texto livre; Enter navega, Escape/perder foco cancela sem navegar.
  const [editingPath, setEditingPath] = useState(false);
  const [pathInput, setPathInput] = useState("/");
  const pathInputRef = useRef<HTMLInputElement>(null);

  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [creatingFolder, setCreatingFolder] = useState(false);

  const [renaming, setRenaming] = useState<SFTPFileEntry | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [savingRename, setSavingRename] = useState(false);

  const [deleting, setDeleting] = useState<SFTPFileEntry | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // Resize do modal — mesmo padrão de VMTerminalModal.tsx/PodQuickViewModal.tsx.
  const [modalSize, setModalSize] = useState({ width: 860, height: 620 });
  const resizing = useRef(false);
  const resizeDir = useRef<"se" | "e" | "s">("se");
  const lastResizePos = useRef({ x: 0, y: 0 });

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return;
      const dx = e.clientX - lastResizePos.current.x;
      const dy = e.clientY - lastResizePos.current.y;
      lastResizePos.current = { x: e.clientX, y: e.clientY };
      setModalSize((prev) => ({
        width: resizeDir.current !== "s" ? Math.max(560, prev.width + dx) : prev.width,
        height: resizeDir.current !== "e" ? Math.max(360, prev.height + dy) : prev.height,
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

  // Reset completo sempre que o modal é reaberto — mesmo princípio de VMTerminalModal.tsx.
  useEffect(() => {
    if (!open) return;
    setConnectionMode(initialConnectionMode || "ssh");
    setHost(instance.publicIp || instance.privateIp || "");
    setPort("22");
    setProfileId("");
    setRemotePort("22");
    setTunnelSessionId(null);
    setTunnelStarting(false);
    setHostKeyToConfirm(null);
    setAcceptedHostKeyFingerprint(null);
    setPhase("form");
    setPath("/");
    setEntries([]);
    setError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, instance.id]);

  // Encerra o túnel SSM (se algum estiver aberto) sempre que o modal desmonta/fecha — nunca
  // deixa um subprocesso `aws ssm` órfão pra trás. Roda tanto no unmount quanto quando `open`
  // vira false (o componente pode continuar montado pelo pai — VMsTab.tsx só desmonta quando
  // sftpTarget vira null — então dependemos do próprio `open` aqui, não só do cleanup do efeito).
  const stopTunnelIfAny = useCallback(() => {
    if (tunnelSessionId) {
      apiClient.stopVMSSMTunnel(instance.id, tunnelSessionId).catch(() => {
        // best-effort — o reaper de ociosidade do backend derruba de qualquer forma
      });
      setTunnelSessionId(null);
    }
  }, [instance.id, tunnelSessionId]);

  useEffect(() => {
    if (!open) stopTunnelIfAny();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    return () => stopTunnelIfAny();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const basePath = `/api/v1/vms/${encodeURIComponent(instance.id)}/sftp`;
  // opPath — cada operação tem um par de rotas gêmeas (list/list-ssm, download/download-ssm etc.,
  // ver vm_sftp_ssm.go) — só o sufixo muda conforme o transporte, sem duplicar toda a lógica de
  // upload/download/mkdir/rename/remove entre os dois modos.
  const opPath = (op: "list" | "download" | "upload" | "mkdir" | "rename" | "remove") =>
    `${basePath}/${op}${connectionMode === "ssm-command" ? "-ssm" : ""}`;

  const targetParams = () => {
    // Modo SSM sem SSH — nunca precisa de credencial SSH/host key nenhuma, só profile+região
    // (mesmos já usados pra listar a instância, ver vm_sftp_ssm.go: profile/region na query).
    if (connectionMode === "ssm-command") {
      return { profile: profile ?? "", region: region ?? "" };
    }
    return {
      ...(connectionMode === "ssm" ? { tunnelSessionId: tunnelSessionId ?? "" } : { host: host.trim(), port }),
      credentialProfileId: profileId,
      // Só vai na query quando já existe uma fingerprint aceita nesta sessão do modal — em toda
      // outra chamada fica de fora (URLSearchParams descarta chave com valor vazio do jeito que é
      // consumida abaixo, então "" aqui equivale a "ausente").
      ...(acceptedHostKeyFingerprint ? { acceptHostKeyFingerprint: acceptedHostKeyFingerprint } : {}),
    };
  };

  const load = useCallback(
    async (targetPath: string) => {
      setLoading(true);
      setError(null);
      try {
        const params = new URLSearchParams({ ...targetParams(), path: targetPath });
        const resp = await apiFetch(`${opPath("list")}?${params.toString()}`);
        const data = await resp.json();
        throwIfFailed(data);
        const sorted: SFTPFileEntry[] = (data.entries ?? []).sort((a: SFTPFileEntry, b: SFTPFileEntry) => {
          if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
          return a.name.localeCompare(b.name);
        });
        setEntries(sorted);
      } catch (e) {
        if (e instanceof ApiFetchError && e.code === "SSH_HOSTKEY_UNKNOWN" && e.fingerprint) {
          setHostKeyToConfirm(e.fingerprint);
        } else {
          setError(e instanceof Error ? e.message : "Erro ao listar diretório");
        }
        setEntries([]);
      } finally {
        setLoading(false);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [basePath, connectionMode, host, port, profileId, tunnelSessionId, acceptedHostKeyFingerprint, profile, region]
  );

  const handleTrustHostKey = () => {
    if (!hostKeyToConfirm) return;
    setAcceptedHostKeyFingerprint(hostKeyToConfirm);
    setHostKeyToConfirm(null);
    // load() só é recriada (useCallback) depois que acceptedHostKeyFingerprint mudar de estado —
    // chamar aqui ainda pegaria a versão ANTIGA (sem a fingerprint); o useEffect abaixo, que já
    // depende de `path`, dispara de novo sozinho quando setAcceptedHostKeyFingerprint muda `load`.
  };

  const handleRejectHostKey = () => {
    setHostKeyToConfirm(null);
    setError("Host key rejeitada — a conexão não pode continuar sem confiar nela.");
  };

  useEffect(() => {
    if (phase === "browsing" && !hostKeyToConfirm) load(path);
    // `load` está nas deps de propósito (não só phase/path): sua IDENTIDADE muda quando
    // acceptedHostKeyFingerprint muda (useCallback), e é exatamente essa mudança que precisa
    // re-disparar a chamada depois de handleTrustHostKey — sem isso, confirmar a fingerprint
    // nunca re-tentaria a listagem sozinho.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, path, load]);

  const handleConnectSSH = () => {
    if (!host.trim() || !port.trim() || !profileId) {
      toast.error("Preencha host, porta e perfil de credencial");
      return;
    }
    setPhase("browsing");
  };

  const handleConnectSSM = async () => {
    if (!profile || !region) {
      toast.error("Profile e região AWS não disponíveis — volte pra lista de instâncias e tente de novo");
      return;
    }
    if (!profileId) {
      toast.error("Selecione um perfil de credencial SSH (autentica através do túnel)");
      return;
    }
    setTunnelStarting(true);
    try {
      const { sessionId } = await apiClient.startVMSSMTunnel(instance.id, profile, region, Number(remotePort) || 22);
      setTunnelSessionId(sessionId);
      setPhase("browsing");
    } catch (e) {
      toast.error("Erro ao abrir túnel SSM", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setTunnelStarting(false);
    }
  };

  // handleConnectSSMCommand — sem sshd algum, sem túnel, sem credencial SSH: cada operação já
  // resolve profile/região por conta própria (targetParams()), então "conectar" aqui é só a
  // transição de fase — nenhum estado de conexão persistente pra abrir/fechar (diferente do túnel
  // SSM, que precisa ficar vivo enquanto o modal está aberto).
  const handleConnectSSMCommand = () => {
    if (!profile || !region) {
      toast.error("Profile e região AWS não disponíveis — volte pra lista de instâncias e tente de novo");
      return;
    }
    setPhase("browsing");
  };

  const handleConnect = () => {
    if (connectionMode === "ssm") handleConnectSSM();
    else if (connectionMode === "ssm-command") handleConnectSSMCommand();
    else handleConnectSSH();
  };

  const handleDisconnect = () => {
    stopTunnelIfAny();
    setPhase("form");
  };

  const navigateTo = (p: string) => setPath(p || "/");
  const breadcrumbSegments = path === "/" ? [] : path.split("/").filter(Boolean);

  const handleEntryClick = (entry: SFTPFileEntry) => {
    if (entry.is_dir) navigateTo(entry.path);
  };

  const startEditingPath = () => {
    setPathInput(path);
    setEditingPath(true);
  };

  const commitPathInput = () => {
    const trimmed = pathInput.trim();
    // Sempre absoluto — um caminho relativo não tem significado sem saber o cwd remoto (que este
    // modal nunca rastreia), então normaliza adicionando "/" na frente em vez de navegar errado
    // silenciosamente.
    navigateTo(trimmed.startsWith("/") ? trimmed : `/${trimmed}`);
    setEditingPath(false);
  };

  useEffect(() => {
    if (editingPath) {
      pathInputRef.current?.focus();
      pathInputRef.current?.select();
    }
  }, [editingPath]);

  const handleUploadClick = () => fileInputRef.current?.click();

  const handleFileSelected = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;

    setUploading(true);
    try {
      const remotePath = path === "/" ? `/${file.name}` : `${path}/${file.name}`;
      const params = new URLSearchParams({ ...targetParams(), path: remotePath });
      const form = new FormData();
      form.append("file", file);
      const resp = await apiFetch(`${opPath("upload")}?${params.toString()}`, { method: "POST", body: form });
      throwIfFailed(await resp.json());
      toast.success(`Enviado: ${file.name}`);
      load(path);
    } catch (e) {
      toast.error("Erro ao enviar arquivo", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setUploading(false);
    }
  };

  const handleDownload = async (entry: SFTPFileEntry) => {
    try {
      const params = new URLSearchParams({ ...targetParams(), path: entry.path });
      const resp = await apiFetch(`${opPath("download")}?${params.toString()}`);
      // download-ssm (vm_sftp_ssm.go) responde 200 tanto no sucesso (bytes crus,
      // application/octet-stream) quanto numa falha "lógica" (JSON, ex: FILE_TOO_LARGE/arquivo não
      // encontrado) — resp.ok sozinho não distingue os dois casos, só o Content-Type. Sem essa
      // checagem, uma falha viraria um "download" corrompido com o texto do erro em JSON no lugar
      // do conteúdo real do arquivo.
      if ((resp.headers.get("content-type") || "").includes("application/json")) {
        throwIfFailed(await resp.json());
      }
      const blob = await resp.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = entry.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);
    } catch (e) {
      toast.error("Erro ao baixar arquivo", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    }
  };

  const handleCreateFolder = async () => {
    if (!newFolderName.trim()) return;
    setCreatingFolder(true);
    try {
      const folderPath = path === "/" ? `/${newFolderName.trim()}` : `${path}/${newFolderName.trim()}`;
      const resp = await apiFetch(`${opPath("mkdir")}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...targetParams(), port: Number(port), path: folderPath }),
      });
      throwIfFailed(await resp.json());
      toast.success(`Pasta criada: ${newFolderName.trim()}`);
      setNewFolderOpen(false);
      setNewFolderName("");
      load(path);
    } catch (e) {
      toast.error("Erro ao criar pasta", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setCreatingFolder(false);
    }
  };

  const openRename = (entry: SFTPFileEntry) => {
    setRenaming(entry);
    setRenameValue(entry.name);
  };

  const handleRename = async () => {
    if (!renaming || !renameValue.trim() || renameValue.trim() === renaming.name) {
      setRenaming(null);
      return;
    }
    setSavingRename(true);
    try {
      const dir = path === "/" ? "" : path;
      const newPath = `${dir}/${renameValue.trim()}`;
      const resp = await apiFetch(`${opPath("rename")}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...targetParams(), port: Number(port), old_path: renaming.path, new_path: newPath }),
      });
      throwIfFailed(await resp.json());
      toast.success(`Renomeado para: ${renameValue.trim()}`);
      setRenaming(null);
      load(path);
    } catch (e) {
      toast.error("Erro ao renomear", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setSavingRename(false);
    }
  };

  const handleConfirmDelete = async () => {
    if (!deleting) return;
    setConfirmingDelete(true);
    try {
      const params = new URLSearchParams({ ...targetParams(), path: deleting.path, is_dir: String(deleting.is_dir) });
      const resp = await apiFetch(`${opPath("remove")}?${params.toString()}`, { method: "DELETE" });
      throwIfFailed(await resp.json());
      toast.success(`Removido: ${deleting.name}`);
      setDeleting(null);
      load(path);
    } catch (e) {
      toast.error("Erro ao remover", { description: e instanceof Error ? e.message : "Erro desconhecido" });
    } finally {
      setConfirmingDelete(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          className="flex flex-col overflow-hidden"
          style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
        >
          <DialogHeader>
            {/* pr-8: reserva espaço pro botão fechar (absolute right-4 top-4 em ui/dialog.tsx) —
                mesmo padrão documentado em VMTerminalModal.tsx/ContainersTab.tsx/PodsPanel.tsx. */}
            <DialogTitle className="flex items-center justify-between gap-2 pr-8">
              <span className="flex items-center gap-2">
                <FolderOpen className="h-4 w-4" />
                Arquivos (SFTP) — {instance.name}
              </span>
              {phase === "browsing" && (
                <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={handleDisconnect}>
                  <PlugZap className="h-3.5 w-3.5" /> Desconectar
                </Button>
              )}
            </DialogTitle>
            <DialogDescription>
              Navegue, envie e baixe arquivos direto da instância via SFTP — sem precisar de nenhum cliente externo.
            </DialogDescription>
          </DialogHeader>

          {phase === "form" && (
            <div className="space-y-4 flex-1 min-h-0 overflow-y-auto">
              <div className="space-y-1.5">
                <Label className="text-xs">Modo de conexão</Label>
                <div className="flex gap-1">
                  <Button
                    type="button"
                    variant={connectionMode === "ssh" ? "default" : "outline"}
                    size="sm"
                    className="text-xs"
                    onClick={() => setConnectionMode("ssh")}
                  >
                    SSH direto
                  </Button>
                  <Button
                    type="button"
                    variant={connectionMode === "ssm" ? "default" : "outline"}
                    size="sm"
                    className="text-xs"
                    onClick={() => setConnectionMode("ssm")}
                    disabled={!profile || !region}
                    title={!profile || !region ? "Profile/região AWS não disponíveis" : "SFTP através de um túnel SSM"}
                  >
                    SSM (túnel)
                  </Button>
                  <Button
                    type="button"
                    variant={connectionMode === "ssm-command" ? "default" : "outline"}
                    size="sm"
                    className="text-xs"
                    onClick={() => setConnectionMode("ssm-command")}
                    disabled={!profile || !region}
                    title={!profile || !region ? "Profile/região AWS não disponíveis" : "Sem sshd — via SSM Run Command"}
                  >
                    SSM (sem SSH)
                  </Button>
                </div>
                {connectionMode === "ssm" && (
                  <p className="text-xs text-muted-foreground">
                    Abre um túnel de port-forwarding via SSM (profile "{profile}", região "{region}") e fala SSH/SFTP
                    através dele — a instância só precisa ter o sshd rodando, mesmo sem rede/VPN direta.
                  </p>
                )}
                {connectionMode === "ssm-command" && (
                  <p className="text-xs text-muted-foreground">
                    Sem SSH/sshd nenhum — os comandos rodam via AWS SSM Run Command (profile "{profile}", região
                    "{region}"), o mesmo agente já exigido pelo terminal. Sem credencial SSH nenhuma, mas com um teto
                    de 40KB por arquivo (documento SSM tem limite real de 64KB) — arquivos maiores exigem SSH direto
                    ou SSM (túnel).
                  </p>
                )}
              </div>

              {connectionMode === "ssh" && (
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label className="text-xs">Host</Label>
                    <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="IP ou hostname" />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-xs">Porta</Label>
                    <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="22" />
                  </div>
                </div>
              )}
              {connectionMode === "ssm" && (
                <div className="space-y-1.5 max-w-[160px]">
                  <Label className="text-xs">Porta remota (sshd)</Label>
                  <Input value={remotePort} onChange={(e) => setRemotePort(e.target.value)} placeholder="22" />
                </div>
              )}
              {connectionMode !== "ssm-command" && (
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <Label className="text-xs">Perfil de credencial SSH</Label>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs gap-1 px-1.5"
                      onClick={() => setCredentialsModalOpen(true)}
                    >
                      <Plus className="h-3 w-3" /> Novo perfil
                    </Button>
                  </div>
                  <ProfileSelect value={profileId} onChange={setProfileId} profiles={profiles} disabled={loadingProfiles} />
                  {!loadingProfiles && profiles.length === 0 && (
                    <p className="text-xs text-muted-foreground">
                      Nenhum perfil de credencial cadastrado — clique em "Novo perfil" acima (gerar par novo, importar
                      de ~/.ssh ou colar uma chave existente) antes de conectar.
                    </p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    Se a host key desta instância ainda não foi confiada, abra um terminal SSH pra ela primeiro (aba
                    VMs/EC2) e aceite a fingerprint — SFTP nunca pergunta, só reaproveita o known_hosts já gravado.
                  </p>
                </div>
              )}
              <div className="flex justify-end">
                <Button
                  onClick={handleConnect}
                  disabled={connectionMode === "ssm-command" ? !profile || !region : profiles.length === 0}
                >
                  Conectar
                </Button>
              </div>
            </div>
          )}

          {phase === "browsing" && hostKeyToConfirm && (
            <div className="flex-1 min-h-0 flex items-center justify-center p-6">
              <div className="max-w-md space-y-3 border rounded-md p-4 bg-card">
                <div className="flex items-center gap-2 text-amber-500">
                  <ShieldAlert className="h-5 w-5" />
                  <span className="font-medium">Host key SSH desconhecida</span>
                </div>
                <p className="text-sm text-muted-foreground">
                  Este destino nunca foi acessado antes por este app (a rota de SFTP não tem canal interativo, então a
                  confirmação acontece aqui). Confirme a fingerprint com o administrador da VM antes de aceitar —
                  aceitar uma fingerprint errada pode expor suas credenciais a um ataque MITM.
                </p>
                <div className="font-mono text-xs bg-muted p-2 rounded break-all">{hostKeyToConfirm}</div>
                <div className="flex justify-end gap-2">
                  <Button variant="outline" size="sm" onClick={handleRejectHostKey}>
                    Rejeitar
                  </Button>
                  <Button size="sm" onClick={handleTrustHostKey}>
                    Confiar e continuar
                  </Button>
                </div>
              </div>
            </div>
          )}

          {phase === "browsing" && !hostKeyToConfirm && (
            <>
              <div className="flex-shrink-0 flex items-center gap-2 flex-wrap">
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
                    className="h-8 text-xs font-mono flex-1 min-w-0"
                  />
                ) : (
                  <>
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
                    <Button variant="ghost" size="icon" className="h-8 w-8 flex-shrink-0" onClick={startEditingPath} title="Digitar o caminho diretamente">
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                  </>
                )}

                <Button variant="ghost" size="icon" className="h-8 w-8 flex-shrink-0" onClick={() => load(path)} title="Atualizar" disabled={loading}>
                  <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
                </Button>

                <ProtectedAction>
                  <Button variant="outline" size="sm" className="h-8 text-xs gap-1 flex-shrink-0" onClick={() => setNewFolderOpen(true)}>
                    <FolderPlus className="h-3.5 w-3.5" /> Nova pasta
                  </Button>
                </ProtectedAction>
                <ProtectedAction>
                  <Button variant="outline" size="sm" className="h-8 text-xs gap-1 flex-shrink-0" onClick={handleUploadClick} disabled={uploading}>
                    {uploading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                    Enviar
                  </Button>
                </ProtectedAction>
                <input ref={fileInputRef} type="file" className="hidden" onChange={handleFileSelected} />
              </div>

              <div className="flex-1 min-h-0 overflow-hidden border rounded-lg">
                {error ? (
                  <div className="flex flex-col items-center justify-center h-full gap-2 text-center px-4">
                    <p className="text-sm text-destructive">{error}</p>
                    <Button variant="outline" size="sm" onClick={() => load(path)}>Tentar de novo</Button>
                  </div>
                ) : loading && entries.length === 0 ? (
                  <div className="flex items-center justify-center h-full gap-2 text-sm text-muted-foreground">
                    <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
                  </div>
                ) : entries.length === 0 ? (
                  <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                    Pasta vazia.
                  </div>
                ) : (
                  <ScrollArea className="h-full">
                    <div className="divide-y divide-border/50">
                      {entries.map((entry) => (
                        <div
                          key={entry.path}
                          className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-accent/50 transition-colors group"
                        >
                          <button
                            className="flex items-center gap-2 flex-1 min-w-0 text-left"
                            onClick={() => handleEntryClick(entry)}
                            disabled={!entry.is_dir}
                          >
                            {entry.is_dir ? (
                              <Folder className="h-4 w-4 text-blue-400 flex-shrink-0" />
                            ) : (
                              <File className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                            )}
                            <span className={`truncate ${entry.is_dir ? "font-medium" : ""}`}>{entry.name}</span>
                          </button>
                          <span className="text-xs text-muted-foreground w-20 text-right flex-shrink-0">
                            {entry.is_dir ? "" : formatBytes(entry.size)}
                          </span>
                          <span className="text-xs text-muted-foreground w-36 text-right flex-shrink-0 hidden sm:block">
                            {formatModTime(entry.mod_time)}
                          </span>
                          <div className="flex items-center gap-0.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                            {!entry.is_dir && (
                              <Button variant="ghost" size="icon" className="h-7 w-7" title="Baixar" onClick={() => handleDownload(entry)}>
                                <Download className="h-3.5 w-3.5" />
                              </Button>
                            )}
                            <ProtectedAction>
                              <Button variant="ghost" size="icon" className="h-7 w-7" title="Renomear" onClick={() => openRename(entry)}>
                                <Pencil className="h-3.5 w-3.5" />
                              </Button>
                            </ProtectedAction>
                            <ProtectedAction>
                              <Button
                                variant="ghost" size="icon"
                                className="h-7 w-7 text-destructive hover:text-destructive"
                                title="Excluir"
                                onClick={() => setDeleting(entry)}
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            </ProtectedAction>
                          </div>
                        </div>
                      ))}
                    </div>
                  </ScrollArea>
                )}
              </div>
            </>
          )}

          {/* Handles de resize — mesmo padrão de VMTerminalModal.tsx/PodQuickViewModal.tsx. */}
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
      </Dialog>

      {/* Nova pasta */}
      <Dialog open={newFolderOpen} onOpenChange={(v) => { if (!creatingFolder) setNewFolderOpen(v); }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Nova pasta</DialogTitle>
          </DialogHeader>
          <Input
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            placeholder="nome-da-pasta"
            autoFocus
            onKeyDown={(e) => { if (e.key === "Enter") handleCreateFolder(); }}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" size="sm" onClick={() => setNewFolderOpen(false)} disabled={creatingFolder}>Cancelar</Button>
            <Button size="sm" onClick={handleCreateFolder} disabled={!newFolderName.trim() || creatingFolder}>
              {creatingFolder && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
              Criar
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Renomear */}
      <Dialog open={!!renaming} onOpenChange={(v) => { if (!v && !savingRename) setRenaming(null); }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Renomear</DialogTitle>
          </DialogHeader>
          <Input
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            autoFocus
            onKeyDown={(e) => { if (e.key === "Enter") handleRename(); }}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" size="sm" onClick={() => setRenaming(null)} disabled={savingRename}>Cancelar</Button>
            <Button size="sm" onClick={handleRename} disabled={!renameValue.trim() || savingRename}>
              {savingRename && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
              Salvar
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Confirmação de exclusão */}
      <AlertDialog open={!!deleting} onOpenChange={(v) => { if (!v && !confirmingDelete) setDeleting(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmar Exclusão</AlertDialogTitle>
            <AlertDialogDescription>
              Tem certeza que deseja excluir {deleting?.is_dir ? "a pasta" : "o arquivo"}{" "}
              <strong>{deleting?.name}</strong>
              {deleting?.is_dir && " e todo o seu conteúdo"}? Esta ação não pode ser desfeita.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={confirmingDelete}>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmDelete} disabled={confirmingDelete} className="bg-destructive hover:bg-destructive/90">
              {confirmingDelete && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Excluir
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Aninhado sobre o modal principal — permite gerar/importar uma chave sem sair do fluxo de
          conexão. Ao fechar, refetchProfiles() faz o perfil recém-criado aparecer na hora no
          ProfileSelect, sem precisar reabrir este modal. */}
      <VMCredentialsModal
        open={credentialsModalOpen}
        onClose={() => {
          setCredentialsModalOpen(false);
          refetchProfiles();
        }}
      />
    </>
  );
}
