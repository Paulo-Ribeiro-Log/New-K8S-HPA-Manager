import { useEffect, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  ShieldAlert,
  Loader2,
  Terminal as TerminalIcon,
  PlugZap,
  Check,
  ChevronsUpDown,
  Plus,
  Folder,
  File,
  Home,
  ChevronRight,
  RefreshCw,
  Pencil,
} from "lucide-react";
import { toast } from "sonner";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import "xterm/css/xterm.css";
import { useVMCredentialProfiles } from "@/hooks/useVMs";
import { apiClient } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import VMCredentialsModal from "@/components/VMCredentialsModal";
import type { VMInstance, SSHCredentialProfile } from "@/lib/api/types";

// ProfileSelect — mesmo combobox Popover+Command já usado em VMTerminalModal.tsx/VMSFTPModal.tsx
// (o <Select> nativo não abria dentro destes modais).
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

interface VMConnectModalProps {
  instance: VMInstance;
  mode: "ssh" | "ssm";
  // profile/region — só usados no modo SSM (autorização via IAM, mesmos já usados pra listar a
  // instância na aba VMs/EC2).
  profile?: string;
  region?: string;
  open: boolean;
  onClose: () => void;
}

type TerminalMessage = {
  type: "input" | "output" | "resize" | "error" | "hostkey_confirm" | "hostkey_response";
  data?: string;
  cols?: number;
  rows?: number;
};

type Phase = "form" | "connecting" | "elevating" | "elevate_failed" | "browsing" | "connected" | "closed";

interface BrowseEntry {
  name: string;
  isDir: boolean;
}

// shellQuote — envolve um caminho em aspas simples pra uso seguro num comando de shell POSIX,
// escapando aspas simples embutidas via '\'' (fecha, insere aspas simples literal escapada,
// reabre). Mesma função já usada em VMTerminalModal.tsx/VMSSMTerminalModal.tsx.
function shellQuote(path: string): string {
  return `'${path.replace(/'/g, `'\\''`)}'`;
}

// Marcadores — TODOS (não só BEGIN/END) exigem "$$" (PID do shell atual) SUBSTITUÍDO em tempo de
// execução logo depois de "VMFM", nunca um texto fixo.
//
// Bug real corrigido, confirmado simulando uma PTY interativa de verdade (não hipótese): o
// terminal remoto ECOA cada linha que mandamos como "input" ANTES de executá-la (echo padrão de
// terminal) — então o próprio TEXTO da linha enviada (ex: "printf '@@VMFM:PWD:%s@@\n' "$PWD""),
// com "%s" ainda cru (não substituído), aparece no stream de saída MAIS CEDO que a execução real.
// A 1ª versão só exigia dígitos em BEGIN/END (achando que "estar dentro do bloco BEGIN...END" já
// bastava pra proteger PWD/D/F) — mas o eco da linha de PWD/D/F acontece DEPOIS que o BEGIN real
// já foi emitido (a shell lê/ecoa/executa linha a linha), então o eco cai DENTRO do bloco válido
// mesmo assim, e como PWD_RE/ENTRY_RE não exigiam dígito nenhum, `.exec()` (sempre retorna o
// PRIMEIRO match) pegava o eco (`%s` cru) em vez da execução real — sintoma relatado: erro
// mostrando literalmente `"%s"` no lugar do caminho, e navegação falhando pra QUALQUER pasta
// (porque `cwd` ficava travado em `"%s"` desde a primeira listagem, corrompendo todo `cd`
// calculado a partir dali). Corrigido exigindo o PID substituído em TODO marcador — o eco nunca
// tem dígito onde `"$$"` deveria aparecer (só o literal "$$" de duas letras), então nenhuma linha
// ecoada nunca bate com nenhuma dessas regexes, só a execução de verdade.
const BEGIN_RE = /@@VMFM(\d+):BEGIN@@/;
const END_RE = /@@VMFM(\d+):END@@/;
const UID_RE = /@@VMFM\d+:UID:(\d+)@@/;
const PWD_RE = /@@VMFM\d+:PWD:([^\n]*)@@/;
const ENTRY_RE = /@@VMFM\d+:([DF]):([^\n]*)@@/g;

// buildListingScript — comando enviado como "input" cru pro shell remoto (SSH ou SSM, mesmo
// protocolo). `cdCmd` (opcional) roda ANTES da listagem — usado só na navegação entre pastas, não
// na primeira listagem (que já começa no diretório de login/home do usuário elevado).
function buildListingScript(cdCmd?: string): string {
  const lines = [
    ...(cdCmd ? [cdCmd] : []),
    `printf '\\n@@VMFM%s:BEGIN@@\\n' "$$"`,
    `printf '@@VMFM%s:UID:%s@@\\n' "$$" "$(id -u)"`,
    `printf '@@VMFM%s:PWD:%s@@\\n' "$$" "$PWD"`,
    `for f in .* *; do [ "$f" = "." ] && continue; [ "$f" = ".." ] && continue; if [ -d "$f" ]; then printf '@@VMFM%s:D:%s@@\\n' "$$" "$f"; elif [ -f "$f" ]; then printf '@@VMFM%s:F:%s@@\\n' "$$" "$f"; fi; done`,
    `printf '@@VMFM%s:END@@\\n' "$$"`,
  ];
  return lines.join("\n") + "\n";
}

const LISTING_TIMEOUT_MS = 10000;

// VMConnectModal — pedido explícito do usuário, corrigindo o desenho anterior desta mesma feature
// (que rodava o "gerenciador de arquivos" sobre SFTP de verdade, exigindo credencial SSH mesmo em
// modo SSM — errado, SSM não tem SFTP nativo e não deveria pedir credencial nenhuma). Fluxo real
// pedido: "escolho a vm, me conecto por ssm e a conexão acontece, mas sem exibir o terminal. é
// executado um 'sudo su -'. o gerenciador de arquivos é exibido para a escolha das pastas/sub-
// pastas e depois disso escolhido o terminal é exibido" — confirmado que vale pros dois modos
// (SSH e SSM), reaproveitando a MESMA sessão de shell já conectada (nunca SFTP) tanto pra elevar
// privilégio quanto pra listar diretórios, via comandos de texto cujo resultado é parseado da
// própria saída do terminal — sem protocolo/credencial extra nenhuma além da conexão em si.
//
// Perda aceita explicitamente pelo usuário: sem upload/download/renomear/excluir nesta tela (isso
// continua existindo separado, no botão "Arquivos (SFTP)" da aba VMs/EC2, que segue usando SFTP de
// verdade quando o usuário quer transferir arquivos de propósito).
export default function VMConnectModal({ instance, mode, profile, region, open, onClose }: VMConnectModalProps) {
  const { profiles, loading: loadingProfiles, refetch: refetchProfiles } = useVMCredentialProfiles();
  const [credentialsModalOpen, setCredentialsModalOpen] = useState(false);

  const [host, setHost] = useState(instance.publicIp || instance.privateIp || "");
  const [port, setPort] = useState("22");
  const [profileId, setProfileId] = useState("");
  const [phase, setPhase] = useState<Phase>(mode === "ssh" ? "form" : "connecting");
  const [pendingHostKey, setPendingHostKey] = useState<string | null>(null);
  const [elevateError, setElevateError] = useState<string | null>(null);

  const [cwd, setCwd] = useState("");
  const [entries, setEntries] = useState<BrowseEntry[]>([]);
  const [browsingLoading, setBrowsingLoading] = useState(false);
  const [browsingError, setBrowsingError] = useState<string | null>(null);

  // Edição manual do caminho — mesmo padrão já usado em VMSFTPModal.tsx/VMCertificatePanel.tsx
  // (pedido explícito do usuário). Aqui `navigateTo` já roda um `cd` de verdade no shell remoto,
  // então aceita qualquer coisa que um `cd` aceitaria (absoluto, relativo, "~", ".."), sem
  // nenhuma normalização própria — diferente dos outros dois navegadores (baseados em listagem
  // SFTP simples, sem noção de cwd/caminho relativo).
  const [editingCwd, setEditingCwd] = useState(false);
  const [cwdInput, setCwdInput] = useState("");
  const cwdInputRef = useRef<HTMLInputElement>(null);

  const phaseRef = useRef<Phase>(phase);
  useEffect(() => {
    phaseRef.current = phase;
  }, [phase]);

  const wsRef = useRef<WebSocket | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const startedTerminalRef = useRef(false);
  // terminalOpened (state, não ref) — controla se o container do terminal continua MONTADO
  // (só escondido via CSS) depois de aberto pela 1a vez, pra permitir "Voltar ao gerenciador de
  // arquivos" sem perder a sessão xterm (scrollback, cursor, etc.). Sem isso, alternar phase pra
  // "browsing" desmontaria o <div> e, ao voltar pra "connected", um container NOVO seria criado —
  // mas startedTerminalRef já estaria true, então openTerminal nunca rodaria de novo pro novo nó,
  // deixando o xterm "pendurado" sem nenhum DOM visível.
  const [terminalOpened, setTerminalOpened] = useState(false);
  const removeResizeListenerRef = useRef<(() => void) | null>(null);
  const decoderRef = useRef<TextDecoder | null>(null);
  const rawBufferRef = useRef("");
  const consumedUpToRef = useRef(0);
  const elevationSentRef = useRef(false);
  const listingTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingCheckUidRef = useRef(true);
  // pendingNavFromRef — bug real corrigido, relatado pelo usuário: "o diretorio é listado mas não
  // me permite acessa-lo" (em QUALQUER pasta). Quando `cd` falha silenciosamente no shell remoto
  // (permissão negada, pasta removida, etc.), o script de listagem ainda roda normalmente — só que
  // reflete a MESMA pasta de antes, e sem verificação nenhuma isso parecia "nada acontece" (a tela
  // simplesmente continuava mostrando o mesmo conteúdo, sem nenhum aviso do motivo). Guarda o
  // diretório ANTES de cada navegação (nunca setado em refresh/listagem inicial, só em
  // `navigateTo`) — se o PWD reportado depois de rodar o `cd` vier IDÊNTICO ao de antes, é sinal
  // inequívoco de que o `cd` não teve efeito nenhum, e um erro visível é mostrado em vez de ficar
  // silencioso.
  const pendingNavFromRef = useRef<string | null>(null);

  // Resize do modal — mesmo padrão de VMTerminalModal.tsx/PodQuickViewModal.tsx.
  const [modalSize, setModalSize] = useState({ width: 900, height: 560 });
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
        height: resizeDir.current !== "e" ? Math.max(320, prev.height + dy) : prev.height,
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

  // Também reage a `phase`: voltar de "browsing" pra "connected" (handleBackToFiles→"Abrir
  // Terminal" de novo) reexibe o container via CSS (nunca desmonta — ver terminalOpened), e um
  // container que esteve com `display:none` pode medir 0×0 até um fit() explícito rodar de novo;
  // sem isso, o terminal voltaria com o tamanho/cols errado até o próximo resize manual da janela.
  useEffect(() => {
    if (!fitAddonRef.current || !xtermRef.current || phase !== "connected") return;
    requestAnimationFrame(() => {
      if (!fitAddonRef.current || !xtermRef.current) return;
      fitAddonRef.current.fit();
      const t = xtermRef.current;
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(JSON.stringify({ type: "resize", cols: t.cols, rows: t.rows }));
      }
    });
  }, [modalSize.width, modalSize.height, phase]);

  const send = (msg: TerminalMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) wsRef.current.send(JSON.stringify(msg));
  };

  const sendRaw = (text: string) => {
    send({ type: "input", data: btoa(unescape(encodeURIComponent(text))) });
  };

  const clearListingTimeout = () => {
    if (listingTimeoutRef.current) {
      clearTimeout(listingTimeoutRef.current);
      listingTimeoutRef.current = null;
    }
  };

  // runListingScript — envia um comando (elevação+listagem na 1a vez; cd+listagem depois) e marca
  // o estado de espera; o parsing real acontece em processIncomingText, chamado a cada mensagem
  // "output" recebida via WS.
  const runListingScript = (script: string, opts: { checkUid: boolean }) => {
    clearListingTimeout();
    setBrowsingError(null);
    setBrowsingLoading(true);
    pendingCheckUidRef.current = opts.checkUid;
    sendRaw(script);
    listingTimeoutRef.current = setTimeout(() => {
      setBrowsingLoading(false);
      if (phaseRef.current !== "connected") {
        if (opts.checkUid) {
          setElevateError(
            "Sem resposta do shell dentro do tempo esperado — a instância pode estar lenta ou travada. Tente de novo."
          );
          setPhase("elevate_failed");
        } else {
          setBrowsingError("Sem resposta do shell dentro do tempo esperado. Tente atualizar de novo.");
        }
      }
    }, LISTING_TIMEOUT_MS);
  };

  // processIncomingText — acumula todo texto decodificado desde a conexão (rawBufferRef) e só
  // interpreta o que está ENTRE um par de marcadores BEGIN/END válidos (dígitos reais, nunca o "%s"
  // cru ecoado do próprio comando digitado — ver comentário de BEGIN_RE acima). Tudo antes/fora
  // desse par é ignorado pra fins de parsing (mas segue acumulado no buffer bruto).
  const processIncomingText = (text: string) => {
    rawBufferRef.current += text;

    if (phaseRef.current === "connected") return; // terminal já revelado, nada mais a parsear aqui

    const unconsumed = rawBufferRef.current.slice(consumedUpToRef.current);
    const beginMatch = BEGIN_RE.exec(unconsumed);
    if (!beginMatch) return;
    const endMatch = END_RE.exec(unconsumed);
    if (!endMatch || endMatch.index < beginMatch.index) return; // END ainda não chegou

    const block = unconsumed.slice(beginMatch.index, endMatch.index + endMatch[0].length);
    consumedUpToRef.current += beginMatch.index + block.length;
    // Aparar o que já foi consumido evita o buffer bruto crescer sem limite numa sessão longa.
    rawBufferRef.current = rawBufferRef.current.slice(consumedUpToRef.current);
    consumedUpToRef.current = 0;

    clearListingTimeout();
    setBrowsingLoading(false);

    const pwdMatch = PWD_RE.exec(block);
    const newEntries: BrowseEntry[] = [];
    let m: RegExpExecArray | null;
    ENTRY_RE.lastIndex = 0;
    while ((m = ENTRY_RE.exec(block)) !== null) {
      newEntries.push({ name: m[2], isDir: m[1] === "D" });
    }
    newEntries.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    if (pendingCheckUidRef.current) {
      const uidMatch = UID_RE.exec(block);
      if (!uidMatch || uidMatch[1] !== "0") {
        setElevateError(
          "Não foi possível elevar privilégios via \"sudo su -\" sem senha nesta instância — o usuário conectado não " +
            "tem permissão de sudo sem senha configurada, ou a política exige autenticação interativa. Ajuste o " +
            "sudoers da VM (NOPASSWD) ou trate manualmente por fora desta ferramenta."
        );
        setPhase("elevate_failed");
        return;
      }
    }

    // Sem PWD nenhum reconhecido no bloco — nunca deveria acontecer (o script sempre emite essa
    // linha), mas se acontecer é melhor avisar explicitamente do que deixar `cwd` desalinhado em
    // silêncio (root cause exata do bug relatado: com `cwd` errado, o caminho calculado pro `cd`
    // de qualquer pasta filha sai errado, o `cd` falha, e a mesma pasta é sempre re-listada,
    // parecendo "nada muda" ao clicar em qualquer lugar).
    if (!pwdMatch) {
      setBrowsingError(
        "Não foi possível determinar o diretório atual — o parsing da saída do shell falhou. Tente atualizar."
      );
      setEntries(newEntries);
      return;
    }

    const newCwd = pwdMatch[1];
    // Verificação real de que o `cd` funcionou: se esta chamada veio de uma navegação (não de um
    // refresh/listagem inicial) e o diretório reportado depois é IDÊNTICO ao de antes, o `cd`
    // silenciosamente não teve efeito nenhum (permissão negada, pasta removida, symlink quebrado,
    // etc.) — mostra um erro explícito em vez de só re-listar a mesma pasta sem dizer por quê.
    if (pendingNavFromRef.current !== null && pendingNavFromRef.current === newCwd) {
      setBrowsingError(
        `Não foi possível entrar na pasta escolhida — o diretório continua em "${newCwd}". Permissão negada, a pasta ` +
          "não existe mais, ou é um link simbólico quebrado."
      );
      pendingNavFromRef.current = null;
      setEntries(newEntries);
      setPhase("browsing");
      return;
    }
    pendingNavFromRef.current = null;

    setCwd(newCwd);
    setEntries(newEntries);
    setPhase("browsing");
  };

  const startElevationAndInitialListing = () => {
    if (elevationSentRef.current) return;
    elevationSentRef.current = true;
    setPhase("elevating");
    // "sudo su -" pedido explícito do usuário — "-n" faz o sudo falhar rápido (sem nunca travar
    // esperando senha) se não houver NOPASSWD configurado, em vez de ficar parado esperando um
    // input que esta ferramenta nunca vai mandar. O comando de listagem é enviado logo em seguida,
    // sem esperar: se o "su -" deu certo, quem lê a próxima linha é o shell NOVO (root); se falhou,
    // quem lê é o shell ORIGINAL — dos dois jeitos, a checagem de UID abaixo revela o que houve.
    sendRaw("sudo -n su -\n");
    runListingScript(buildListingScript(), { checkUid: true });
  };

  // Abre o WebSocket (SSH ou SSM, mesmo protocolo) — chamado ao entrar em "connecting" (form
  // enviado, no modo SSH; ou imediatamente ao abrir, no modo SSM).
  const openConnection = () => {
    const decoder = new TextDecoder();
    decoderRef.current = decoder;

    const wsUrl =
      mode === "ssh"
        ? apiClient.getVMTerminalWsUrl(instance.id, host.trim(), parseInt(port, 10) || 22, profileId)
        : apiClient.getVMSSMTerminalWsUrl(instance.id, profile || "", region || "");
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onmessage = (event) => {
      let msg: TerminalMessage;
      try {
        msg = JSON.parse(event.data);
      } catch {
        return;
      }
      switch (msg.type) {
        case "output": {
          if (!msg.data) break;
          const binary = atob(msg.data);
          const bytes = new Uint8Array(binary.length);
          for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);

          if (phaseRef.current === "connected" && xtermRef.current) {
            xtermRef.current.write(bytes);
            break;
          }

          const text = decoderRef.current?.decode(bytes, { stream: true }) ?? "";
          if (!elevationSentRef.current) {
            // Primeira saída recebida = shell pronto (mesmo heurístico já usado nos terminais
            // originais desta feature) — dispara a elevação+1a listagem uma única vez.
            startElevationAndInitialListing();
          } else {
            processIncomingText(text);
          }
          break;
        }
        case "hostkey_confirm":
          setPendingHostKey(msg.data || "");
          break;
        case "error":
          toast.error(msg.data || "Erro na conexão");
          break;
      }
    };

    ws.onerror = () => {
      toast.error("Erro na conexão WebSocket");
    };

    ws.onclose = () => {
      decoderRef.current = null;
      if (phaseRef.current === "connected" && xtermRef.current) {
        xtermRef.current.writeln("\r\n\x1b[1;33m⚠ Conexão encerrada\x1b[0m\r");
      }
      setPhase("closed");
    };
  };

  // Reset completo sempre que o modal é (re)aberto — nunca reaproveita WS/estado de uma sessão
  // anterior. Modo SSM não tem formulário — conecta direto (só IAM via profile/região, sem
  // credencial SSH nenhuma).
  useEffect(() => {
    if (!open) return;
    setHost(instance.publicIp || instance.privateIp || "");
    setPort("22");
    setProfileId("");
    setPendingHostKey(null);
    setElevateError(null);
    setCwd("");
    setEntries([]);
    setBrowsingLoading(false);
    setBrowsingError(null);
    setEditingCwd(false);
    startedTerminalRef.current = false;
    setTerminalOpened(false);
    // Descarta qualquer terminal de uma sessão anterior — nunca reaproveita xterm entre
    // instâncias/reconexões diferentes (mesmo princípio de "nunca reaproveita WS/estado" já
    // documentado acima; sem isso, com o container agora persistente — ver terminalOpened — um
    // xterm órfão ficaria retido em memória, listeners inclusos).
    removeResizeListenerRef.current?.();
    removeResizeListenerRef.current = null;
    xtermRef.current?.dispose();
    xtermRef.current = null;
    fitAddonRef.current = null;
    elevationSentRef.current = false;
    rawBufferRef.current = "";
    consumedUpToRef.current = 0;
    pendingNavFromRef.current = null;
    clearListingTimeout();
    if (mode === "ssh") {
      setPhase("form");
    } else {
      setPhase("connecting");
      openConnection();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, instance.id, mode]);

  useEffect(() => {
    return () => {
      clearListingTimeout();
      removeResizeListenerRef.current?.();
      wsRef.current?.close();
      xtermRef.current?.dispose();
      xtermRef.current = null;
      wsRef.current = null;
    };
  }, []);

  const handleConnect = () => {
    if (!host.trim() || !port.trim() || !profileId) {
      toast.error("Preencha host, porta e perfil de credencial");
      return;
    }
    setPhase("connecting");
    openConnection();
  };

  const respondHostKey = (accept: boolean) => {
    send({ type: "hostkey_response", data: accept ? "accept" : "reject" });
    setPendingHostKey(null);
    if (!accept) {
      toast.error("Host key rejeitada — a conexão foi encerrada.");
    }
  };

  const handleRetryElevate = () => {
    setElevateError(null);
    elevationSentRef.current = false;
    rawBufferRef.current = "";
    consumedUpToRef.current = 0;
    setPhase("elevating");
    startElevationAndInitialListing();
  };

  const navigateTo = (path: string) => {
    if (browsingLoading) return;
    // Guarda o diretório ANTES do cd — comparado depois em processIncomingText pra detectar um
    // `cd` que silenciosamente não teve efeito nenhum (permissão negada, pasta removida, etc.).
    pendingNavFromRef.current = cwd;
    runListingScript(buildListingScript(`cd ${shellQuote(path)} 2>&1`), { checkUid: false });
  };

  const refreshCurrent = () => {
    if (browsingLoading || !cwd) return;
    pendingNavFromRef.current = null;
    runListingScript(buildListingScript(), { checkUid: false });
  };

  const breadcrumbSegments = cwd === "/" || !cwd ? [] : cwd.split("/").filter(Boolean);

  const startEditingCwd = () => {
    setCwdInput(cwd);
    setEditingCwd(true);
  };

  const commitCwdInput = () => {
    const trimmed = cwdInput.trim();
    if (trimmed) navigateTo(trimmed);
    setEditingCwd(false);
  };

  useEffect(() => {
    if (editingCwd) {
      cwdInputRef.current?.focus();
      cwdInputRef.current?.select();
    }
  }, [editingCwd]);

  // Revela o terminal de verdade — cria o xterm SÓ agora (nunca antes), escreve primeiro qualquer
  // texto já pendente no buffer bruto (tipicamente o prompt atual, ex: "root@ip-10-x-x-x:/etc#")
  // pra não abrir uma tela em branco sem contexto, e a partir daqui todo "output" novo passa a ser
  // escrito direto nele (ver processIncomingText/ws.onmessage acima, guardado por phaseRef).
  const openTerminal = (container: HTMLDivElement) => {
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      convertEol: true,
      scrollback: 10000,
      theme: {
        background: "#1e1e1e",
        foreground: "#d4d4d4",
        cursor: "#ffffff",
        cursorAccent: "#1e1e1e",
        selectionBackground: "#264f78",
      },
      allowProposedApi: true,
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    xtermRef.current = terminal;
    fitAddonRef.current = fitAddon;

    const leftover = rawBufferRef.current.slice(consumedUpToRef.current);
    consumedUpToRef.current = rawBufferRef.current.length;
    if (leftover) terminal.write(leftover);

    requestAnimationFrame(() => {
      fitAddon.fit();
      send({ type: "resize", cols: terminal.cols, rows: terminal.rows });
    });

    terminal.onData((data) => sendRaw(data));

    const handleResize = () => {
      fitAddonRef.current?.fit();
      const t = xtermRef.current;
      if (t) send({ type: "resize", cols: t.cols, rows: t.rows });
    };
    window.addEventListener("resize", handleResize);
    removeResizeListenerRef.current = () => window.removeEventListener("resize", handleResize);
  };

  const containerRefCallback = (el: HTMLDivElement | null) => {
    if (el && phase === "connected" && !startedTerminalRef.current) {
      startedTerminalRef.current = true;
      openTerminal(el);
      setTerminalOpened(true);
    }
  };

  const handleDisconnect = () => {
    wsRef.current?.close();
  };

  // handleBackToFiles — pedido explícito do usuário: volta do terminal pro gerenciador de
  // arquivos SEM fechar a conexão. Reaproveita a MESMA sessão de shell (nunca abre um WS novo) —
  // roda a listagem de novo (sem `cd`, equivalente a refreshCurrent) pra refletir onde quer que o
  // shell esteja agora, já que o usuário pode ter navegado livremente enquanto usava o terminal.
  // Risco aceito e sinalizado no tooltip do botão: se o shell estiver no meio de um programa
  // interativo (vim, top, um REPL...) em vez de um prompt ocioso, o script de listagem vira input
  // literal PRO PROGRAMA, não comandos de shell — mesma limitação estrutural já existente no
  // mecanismo de navegação (não há como distinguir "prompt ocioso" de "programa rodando" só pela
  // saída do terminal).
  const handleBackToFiles = () => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      toast.error("Conexão fechada — não é possível voltar ao gerenciador de arquivos.");
      return;
    }
    rawBufferRef.current = "";
    consumedUpToRef.current = 0;
    pendingNavFromRef.current = null;
    setPhase("browsing");
    runListingScript(buildListingScript(), { checkUid: false });
  };

  const title = mode === "ssh" ? "Conectar via SSH" : "Conectar via SSM";

  return (
    <>
      <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
        <DialogContent
          className="flex flex-col overflow-hidden p-4"
          style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
        >
          <DialogHeader className="flex-shrink-0">
            <DialogTitle className="flex items-center justify-between gap-2 pr-8">
              <span className="flex items-center gap-2">
                <TerminalIcon className="h-4 w-4" /> {title} — {instance.name}
              </span>
              <div className="flex items-center gap-2 flex-shrink-0">
                {phase === "connected" && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs gap-1"
                    onClick={handleBackToFiles}
                    title="Volta ao gerenciador de arquivos sem fechar a conexão. Se o shell estiver rodando um programa interativo (vim, top, etc.) em vez de um prompt ocioso, isso manda o comando de listagem como input literal pra ele — use só com um prompt ocioso."
                  >
                    <Folder className="h-3.5 w-3.5" /> Gerenciador de Arquivos
                  </Button>
                )}
                {(phase === "elevating" || phase === "browsing" || phase === "connected") && (
                  <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={handleDisconnect}>
                    <PlugZap className="h-3.5 w-3.5" /> Desconectar
                  </Button>
                )}
              </div>
            </DialogTitle>
            {phase === "form" && (
              <DialogDescription>
                A host key SSH deste destino nunca é aceita silenciosamente — na primeira conexão você vai confirmar a
                fingerprint manualmente.
              </DialogDescription>
            )}
          </DialogHeader>

          {phase === "form" && (
            <div className="space-y-4 flex-1 min-h-0 overflow-y-auto">
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
                    Nenhum perfil de credencial cadastrado — clique em "Novo perfil" acima antes de conectar.
                  </p>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                Ao conectar, um gerenciador de arquivos aparece primeiro (já elevado via <code className="font-mono">sudo su -</code>) —
                o terminal só é exibido depois de escolher a pasta e clicar em "Abrir Terminal".
              </p>
              <div className="flex justify-end">
                <Button onClick={handleConnect} disabled={profiles.length === 0}>
                  Conectar
                </Button>
              </div>
            </div>
          )}

          {phase !== "form" && (
            <div className="flex-1 min-h-0 flex flex-col relative">
              {pendingHostKey !== null && (
                <div className="absolute inset-0 z-20 bg-background/95 flex items-center justify-center p-6">
                  <div className="max-w-md space-y-3 border rounded-md p-4 bg-card">
                    <div className="flex items-center gap-2 text-amber-500">
                      <ShieldAlert className="h-5 w-5" />
                      <span className="font-medium">Host key SSH desconhecida</span>
                    </div>
                    <p className="text-sm text-muted-foreground">
                      Este servidor nunca foi acessado antes por este app. Confirme a fingerprint com o administrador
                      da VM antes de aceitar — aceitar uma fingerprint errada pode expor suas credenciais a um ataque
                      MITM.
                    </p>
                    <div className="font-mono text-xs bg-muted p-2 rounded break-all">{pendingHostKey}</div>
                    <div className="flex justify-end gap-2">
                      <Button variant="outline" size="sm" onClick={() => respondHostKey(false)}>
                        Rejeitar
                      </Button>
                      <Button size="sm" onClick={() => respondHostKey(true)}>
                        Confiar e conectar
                      </Button>
                    </div>
                  </div>
                </div>
              )}

              {(phase === "connecting" || phase === "elevating") && pendingHostKey === null && (
                <div className="flex-1 flex items-center justify-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {phase === "connecting" ? "Conectando..." : "Elevando privilégio (sudo su -) e listando pastas..."}
                </div>
              )}

              {phase === "elevate_failed" && (
                <div className="flex-1 flex items-center justify-center p-6">
                  <div className="max-w-md space-y-3 border rounded-md p-4 bg-card">
                    <div className="flex items-center gap-2 text-destructive">
                      <ShieldAlert className="h-5 w-5" />
                      <span className="font-medium">Não foi possível elevar privilégio</span>
                    </div>
                    <p className="text-sm text-muted-foreground">{elevateError}</p>
                    <div className="flex justify-end gap-2">
                      <Button variant="outline" size="sm" onClick={onClose}>
                        Fechar
                      </Button>
                      <Button size="sm" onClick={handleRetryElevate}>
                        Tentar de novo
                      </Button>
                    </div>
                  </div>
                </div>
              )}

              {phase === "browsing" && (
                <>
                  <div className="flex-shrink-0 flex items-center gap-2 flex-wrap pb-1">
                    {editingCwd ? (
                      <Input
                        ref={cwdInputRef}
                        value={cwdInput}
                        onChange={(e) => setCwdInput(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") commitCwdInput();
                          else if (e.key === "Escape") setEditingCwd(false);
                        }}
                        onBlur={() => setEditingCwd(false)}
                        placeholder="/etc/nginx/"
                        className="h-8 text-xs font-mono flex-1 min-w-0"
                      />
                    ) : (
                      <div className="flex items-center gap-1 text-xs flex-1 min-w-0 overflow-x-auto whitespace-nowrap">
                        <button className="p-1 rounded hover:bg-accent flex-shrink-0" onClick={() => navigateTo("/")} title="Raiz">
                          <Home className="h-3.5 w-3.5" />
                        </button>
                        {breadcrumbSegments.map((seg, i) => {
                          const segPath = "/" + breadcrumbSegments.slice(0, i + 1).join("/");
                          return (
                            <span key={segPath} className="flex items-center gap-1 flex-shrink-0">
                              <ChevronRight className="h-3 w-3 text-muted-foreground" />
                              <button className="hover:underline" onClick={() => navigateTo(segPath)}>
                                {seg}
                              </button>
                            </span>
                          );
                        })}
                        {browsingLoading && (
                          <Loader2 className="h-3 w-3 animate-spin text-muted-foreground flex-shrink-0 ml-1" />
                        )}
                      </div>
                    )}
                    {!editingCwd && (
                      <Button variant="ghost" size="icon" className="h-8 w-8 flex-shrink-0" onClick={startEditingCwd} title="Digitar o caminho diretamente" disabled={browsingLoading}>
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    <Button variant="ghost" size="icon" className="h-8 w-8 flex-shrink-0" onClick={refreshCurrent} title="Atualizar" disabled={browsingLoading}>
                      <RefreshCw className={`h-3.5 w-3.5 ${browsingLoading ? "animate-spin" : ""}`} />
                    </Button>
                    <Button
                      variant="default"
                      size="sm"
                      className="h-8 text-xs gap-1 flex-shrink-0"
                      onClick={() => setPhase("connected")}
                      title={`Abrir terminal já posicionado em ${cwd}`}
                    >
                      <TerminalIcon className="h-3.5 w-3.5" /> Abrir Terminal
                    </Button>
                  </div>

                  {/* Caminho completo sempre visível, texto puro — pra confirmar de relance que a
                      navegação de fato mudou de pasta (a barra de breadcrumb sozinha é sutil
                      demais pra perceber rápido se o clique teve efeito). */}
                  <p className="flex-shrink-0 text-[11px] font-mono text-muted-foreground pb-1.5 truncate" title={cwd}>
                    {cwd || "(diretório desconhecido)"}
                  </p>

                  {browsingError && (
                    <p className="flex-shrink-0 text-xs text-destructive pb-2">{browsingError}</p>
                  )}

                  <div className="flex-1 min-h-0 overflow-hidden border rounded-lg relative">
                    {browsingLoading && entries.length > 0 && (
                      <div className="absolute top-0 left-0 right-0 h-0.5 bg-primary/70 animate-pulse z-10" />
                    )}
                    {browsingLoading && entries.length === 0 ? (
                      <div className="flex items-center justify-center h-full gap-2 text-sm text-muted-foreground">
                        <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
                      </div>
                    ) : entries.length === 0 ? (
                      <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                        Pasta vazia.
                      </div>
                    ) : (
                      <ScrollArea className={cn("h-full", browsingLoading && "pointer-events-none opacity-60")}>
                        <div className="divide-y divide-border/50">
                          {entries.map((entry) => {
                            const entryPath = cwd === "/" ? `/${entry.name}` : `${cwd}/${entry.name}`;
                            return (
                              <button
                                key={entry.name}
                                className={cn(
                                  "flex items-center gap-2 px-3 py-2 text-sm w-full text-left transition-colors",
                                  entry.isDir ? "hover:bg-accent/50" : "cursor-default opacity-70"
                                )}
                                onClick={() => entry.isDir && navigateTo(entryPath)}
                                disabled={!entry.isDir}
                              >
                                {entry.isDir ? (
                                  <Folder className="h-4 w-4 text-blue-400 flex-shrink-0" />
                                ) : (
                                  <File className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                                )}
                                <span className={entry.isDir ? "font-medium" : ""}>{entry.name}</span>
                              </button>
                            );
                          })}
                        </div>
                      </ScrollArea>
                    )}
                  </div>
                </>
              )}

              {/* Fica montado (só escondido via classe) uma vez aberto — troca de phase pra
                  "browsing" via handleBackToFiles NUNCA desmonta o container, senão o xterm
                  ficaria sem nenhum DOM pra reanexar ao voltar (ver comentário de terminalOpened). */}
              {(phase === "connected" || terminalOpened) && (
                <div ref={containerRefCallback} className={cn("w-full flex-1 min-h-0", phase !== "connected" && "hidden")} />
              )}
              {phase === "closed" && (
                <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                  Conexão encerrada.
                </div>
              )}
            </div>
          )}

          {/* Handles de resize — mesmo padrão de PodQuickViewModal.tsx (fixed já vem da classe base
              de DialogContent, nunca adicionar `relative` aqui). */}
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
