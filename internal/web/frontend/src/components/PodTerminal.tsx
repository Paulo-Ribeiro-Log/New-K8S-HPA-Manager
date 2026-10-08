import { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebLinksAddon } from "xterm-addon-web-links";
import "xterm/css/xterm.css";
import { attachTerminalClipboard, decodeTerminalOutput, encodeTerminalInput } from "@/lib/xtermShared";
import { Button } from "@/components/ui/button";
import { X, Maximize2, Minimize2, Copy, Check, Power, FileCode } from "lucide-react";
import { toast } from "sonner";

interface PodTerminalProps {
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
  shell: string;
  isFullscreen: boolean;
  onToggleFullscreen: () => void;
  onClose: () => void;
  ephemeral?: boolean;
  // "tshoot": pod netshoot próprio no namespace, criado pela sessão e APAGADO pelo servidor ao
  // sair (kubectl run --rm). `pod`/`container` são ignorados nesse modo.
  // "node": shell no node (kubectl debug node/<node>), pod privilegiado apagado ao sair.
  mode?: "shell" | "debug" | "tshoot" | "node";
  node?: string; // modo "node"
  // Ephemeral Debug com --rm: o servidor encerra o container quando a última sessão termina
  // (a API não remove ephemeral containers do pod — ele vira Terminated).
  rm?: boolean;
}

// Roda um script local dentro do shell remoto, como `bash < script.sh` (heredoc, sem arquivo no pod).
const SCRIPT_DELIM = "__K8S_HPA_SCRIPT_EOF__";
const scriptHeredoc = (text: string) =>
  `bash -s <<'${SCRIPT_DELIM}'\n${text.replace(/\r\n/g, "\n").replace(/\n?$/, "\n")}${SCRIPT_DELIM}\n`;

export const PodTerminal = ({
  cluster,
  namespace,
  pod,
  container,
  shell,
  isFullscreen,
  onToggleFullscreen,
  onClose,
  ephemeral = false,
  mode: modeProp,
  node = "",
  rm = false,
}: PodTerminalProps) => {
  const mode = modeProp ?? (ephemeral ? "debug" : "shell");
  const sendRef = useRef<(data: string) => void>(() => {});
  const scriptInputRef = useRef<HTMLInputElement>(null);
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const el = terminalRef.current; // elemento DOM (a prop `container` é o nome do container do pod)
    if (!el) return;

    // Mesma base do terminal do Code Editor (RepoTerminal): bytes crus em base64 nos dois
    // sentidos, copiar/colar de lib/xtermShared e refit pelo tamanho real do container. Antes
    // havia um remapeamento "ABNT2" manual de teclas (quebrava acentos com tecla morta e mandava
    // caractere errado em outros layouts), o colar vazava os marcadores de bracketed paste e o
    // tamanho só era reajustado no resize da janela — com a largura do shell remoto diferente da
    // do xterm, editar linhas longas embaralhava o texto.
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
        black: "#000000",
        red: "#cd3131",
        green: "#0dbc79",
        yellow: "#e5e510",
        blue: "#2472c8",
        magenta: "#bc3fbc",
        cyan: "#11a8cd",
        white: "#e5e5e5",
        brightBlack: "#666666",
        brightRed: "#f14c4c",
        brightGreen: "#23d18b",
        brightYellow: "#f5f543",
        brightBlue: "#3b8eea",
        brightMagenta: "#d670d6",
        brightCyan: "#29b8db",
        brightWhite: "#e5e5e5",
      },
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());
    terminal.open(el);
    fitAddon.fit();

    xtermRef.current = terminal;
    fitAddonRef.current = fitAddon;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const token = localStorage.getItem("auth_token") ?? "";
    let wsUrl: string;
    if (mode === "tshoot") {
      wsUrl = `${protocol}//${window.location.host}/api/v1/tshoot/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/ws?${new URLSearchParams({ token }).toString()}`;
    } else if (mode === "node") {
      wsUrl = `${protocol}//${window.location.host}/api/v1/node-shell/${encodeURIComponent(cluster)}/${encodeURIComponent(node)}/ws?${new URLSearchParams({ namespace, token }).toString()}`;
    } else {
      const params = new URLSearchParams({ container, shell, token });
      if (mode === "debug") params.set("image", "nicolaka/netshoot");
      if (mode === "debug" && rm) params.set("rm", "true");
      wsUrl = `${protocol}//${window.location.host}/api/v1/pods/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(pod)}/${mode === "debug" ? "debug" : "shell"}?${params.toString()}`;
      terminal.writeln(mode === "debug"
        ? "\x1b[1;34m⚡ Criando ephemeral debug container...\x1b[0m"
        : "\x1b[1;34m⚡ Conectando ao pod...\x1b[0m");
    }

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    const sendResize = () => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", size: { cols: terminal.cols, rows: terminal.rows } }));
      }
    };
    const sendRawInput = (data: string) => {
      if (!data || ws.readyState !== WebSocket.OPEN) return;
      ws.send(JSON.stringify({ type: "input", data: encodeTerminalInput(data) }));
    };

    ws.onopen = () => {
      setIsConnected(true);
      if (mode === "tshoot") {
        terminal.writeln("\x1b[1;33m🧪 Pod de troubleshooting (nicolaka/netshoot) com --rm\x1b[0m — apagado pelo servidor ao sair (exit ou fechar o terminal)");
      } else if (mode === "node") {
        terminal.writeln(`\x1b[1;33m🖥️  Shell no node ${node} (nicolaka/netshoot, privilegiado) com --rm\x1b[0m — apagado pelo servidor ao sair`);
      } else {
        terminal.writeln("\x1b[1;32m✓ Conectado!\x1b[0m");
        if (mode === "debug") {
          terminal.writeln("\x1b[1;33m🛠️  nicolaka/netshoot\x1b[0m - Ephemeral Debug Container");
          terminal.writeln(rm
            ? "\x1b[1;36mEncerramento (--rm):\x1b[0m ao sair (exit ou fechar o terminal), pelo botão ⏻, ou após 10 min sem atividade"
            : "\x1b[1;36mEncerramento:\x1b[0m botão ⏻ no topo, ou automático após 10 min sem atividade");
        }
        terminal.writeln(`\x1b[1;36mPod:\x1b[0m ${pod}`);
        terminal.writeln(`\x1b[1;36mContainer:\x1b[0m ${container}`);
        terminal.writeln(`\x1b[1;36mShell:\x1b[0m ${shell}`);
      }
      terminal.writeln("\x1b[1;36mAtalhos:\x1b[0m Ctrl+C com texto selecionado = copiar (sem seleção = interromper) | Ctrl+V = colar");
      terminal.writeln("\x1b[2m─────────────────────────────────\x1b[0m");
      fitAddon.fit();
      sendResize();
      terminal.focus();
    };

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);
        if (message.type === "output" && message.data) {
          terminal.write(decodeTerminalOutput(message.data));
        } else if (message.type === "error") {
          terminal.writeln(`\r\n\x1b[1;31m✗ Erro: ${message.data}\x1b[0m`);
        }
      } catch {
        terminal.write(event.data);
      }
    };

    ws.onerror = () => {
      setIsConnected(false);
      terminal.writeln("\r\n\x1b[1;31m✗ Erro na conexão WebSocket\x1b[0m");
      toast.error("Erro na conexão", { description: "Não foi possível conectar ao pod." });
    };

    ws.onclose = (event) => {
      setIsConnected(false);
      terminal.writeln("\r\n\x1b[1;33m⚠ Conexão fechada\x1b[0m");
      if (event.code !== 1000) {
        terminal.writeln(`\x1b[2mCódigo: ${event.code}, Razão: ${event.reason || "N/A"}\x1b[0m`);
      }
    };

    // Teclado: o xterm já recebe o caractere certo do layout do sistema (ABNT2 incluso, com teclas
    // mortas) — nada de remapear tecla por posição física.
    const dataSub = terminal.onData(sendRawInput);
    sendRef.current = sendRawInput;
    const detachClipboard = attachTerminalClipboard(terminal, el, sendRawInput);

    // Refit sempre que o container muda de tamanho (tela cheia, modal, janela) e avisa o shell
    // remoto — larguras diferentes entre xterm e PTY embaralham a edição de linhas longas.
    let frame = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (el.clientWidth === 0 || el.clientHeight === 0) return; // escondido
        const { cols, rows } = terminal;
        fitAddon.fit();
        if (terminal.cols !== cols || terminal.rows !== rows) sendResize();
      });
    });
    observer.observe(el);

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      detachClipboard();
      dataSub.dispose();
      ws.close();
      terminal.dispose();
    };
  }, [cluster, namespace, pod, container, shell, mode, node, rm]);

  // "Rodar script": lê um .sh local e executa no shell remoto (bash -s <<heredoc), como
  // `bash < script.sh` — a saída aparece no terminal.
  const handleRunScript = async (file: File | undefined) => {
    if (!file) return;
    const text = await file.text();
    if (!text.trim()) {
      toast.error("Script vazio");
      return;
    }
    if (text.includes(SCRIPT_DELIM)) {
      toast.error("O script contém o delimitador reservado; não é possível enviá-lo assim");
      return;
    }
    sendRef.current(scriptHeredoc(text));
    xtermRef.current?.focus();
    toast.info(`Executando ${file.name}`);
  };

  // Encerra o ephemeral container de debug (o backend cria /tmp/.stop e o laço de vigia sai).
  // A API do Kubernetes não remove ephemeral containers do pod — ele fica como Terminated no spec.
  const handleTerminateDebug = () => {
    if (wsRef.current?.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: "terminate" }));
    toast.info("Encerrando o container de debug...");
  };

  const handleCopySelection = () => {
    if (!xtermRef.current) return;

    const selection = xtermRef.current.getSelection();
    if (selection) {
      navigator.clipboard.writeText(selection);
      setCopied(true);
      toast.success("Texto copiado");
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="h-full w-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border bg-muted/30 flex-shrink-0">
        <div className="flex items-center gap-2">
          <div
            className={`w-2 h-2 rounded-full ${
              isConnected ? "bg-green-500 animate-pulse" : "bg-red-500"
            }`}
          />
          <span className="text-sm font-medium">
            {mode === "tshoot" ? `Troubleshooting (--rm): ${namespace}`
              : mode === "node" ? `Shell no node (--rm): ${node}`
              : `Terminal: ${pod} / ${container}`}
          </span>
          <span className="text-xs text-muted-foreground">
            ({mode === "tshoot" || mode === "node" ? "netshoot · bash" : shell}{mode === "debug" && rm ? " · --rm" : ""})
          </span>
        </div>
        <div className="flex items-center gap-1">
          {(mode === "tshoot" || mode === "debug" || mode === "node") && (
            <>
              <input
                ref={scriptInputRef}
                type="file"
                accept=".sh,.bash,text/x-shellscript,text/plain"
                className="hidden"
                onChange={(e) => { handleRunScript(e.target.files?.[0]); e.target.value = ""; }}
              />
              <Button
                variant="ghost"
                size="sm"
                onClick={() => scriptInputRef.current?.click()}
                disabled={!isConnected}
                title="Executar um script local (.sh) no shell, como `bash < script.sh`"
                className="h-7 text-xs gap-1"
              >
                <FileCode className="w-3.5 h-3.5" />
                Rodar script
              </Button>
            </>
          )}
          {mode === "debug" && (
            <Button
              variant="ghost"
              size="icon"
              onClick={handleTerminateDebug}
              disabled={!isConnected}
              title="Encerrar container de debug (netshoot)"
              className="h-7 w-7 text-destructive hover:text-destructive"
            >
              <Power className="w-3.5 h-3.5" />
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon"
            onClick={handleCopySelection}
            title="Copiar seleção"
            className="h-7 w-7"
          >
            {copied ? (
              <Check className="w-3.5 h-3.5" />
            ) : (
              <Copy className="w-3.5 h-3.5" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onToggleFullscreen}
            title={isFullscreen ? "Sair de tela cheia" : "Tela cheia"}
            className="h-7 w-7"
          >
            {isFullscreen ? (
              <Minimize2 className="w-3.5 h-3.5" />
            ) : (
              <Maximize2 className="w-3.5 h-3.5" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onClose}
            title="Fechar terminal"
            className="h-7 w-7"
          >
            <X className="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>

      {/* Terminal */}
      <div
        ref={terminalRef}
        className="w-full flex-1"
      />

      {/* Footer com status */}
      <div className="px-4 py-1 border-t border-border bg-muted/50 text-xs text-muted-foreground flex-shrink-0">
        <div className="flex items-center justify-between">
          <span>
            {isConnected
              ? "Conectado - Digite comandos ou pressione Ctrl+C para interromper"
              : "Desconectado"}
          </span>
          <span className="font-mono">
            {mode === "tshoot" || mode === "node" || (mode === "debug" && rm) ? "--rm: removido ao sair · " : ""}{mode === "node" ? node : namespace} / {cluster}
          </span>
        </div>
      </div>
    </div>
  );
};
