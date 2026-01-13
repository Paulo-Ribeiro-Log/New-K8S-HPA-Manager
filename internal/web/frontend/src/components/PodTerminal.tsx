import { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebLinksAddon } from "xterm-addon-web-links";
import "xterm/css/xterm.css";
import { Button } from "@/components/ui/button";
import { X, Maximize2, Minimize2, Copy, Check } from "lucide-react";
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
}

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
}: PodTerminalProps) => {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!terminalRef.current) return;

    // Inicializar terminal com suporte a PT-BR
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      convertEol: true, // Converte \n para \r\n automaticamente
      scrollback: 10000, // Histórico de 10k linhas
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
    const webLinksAddon = new WebLinksAddon();

    terminal.loadAddon(fitAddon);
    terminal.loadAddon(webLinksAddon);

    terminal.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = terminal;
    fitAddonRef.current = fitAddon;

    // Conectar WebSocket
    connectWebSocket(terminal);

    // Resize handler
    const handleResize = () => {
      if (fitAddonRef.current) {
        fitAddonRef.current.fit();
        // Enviar novo tamanho para o servidor
        if (wsRef.current?.readyState === WebSocket.OPEN) {
          const dims = {
            type: "resize",
            cols: terminal.cols,
            rows: terminal.rows,
          };
          wsRef.current.send(JSON.stringify(dims));
        }
      }
    };

    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      if (wsRef.current) {
        wsRef.current.close();
      }
      terminal.dispose();
    };
  }, [cluster, namespace, pod, container, shell, ephemeral]);

  const connectWebSocket = (terminal: Terminal) => {
    // Construir URL do WebSocket
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const host = window.location.host;
    const endpoint = ephemeral ? "debug" : "shell";
    
    // Obter token de autenticação
    const token = localStorage.getItem("auth_token") || "poc-token-123";
    
    const wsUrl = `${protocol}//${host}/api/v1/pods/${cluster}/${namespace}/${pod}/${endpoint}?container=${container}&shell=${shell}${ephemeral ? '&image=nicolaka/netshoot' : ''}&token=${encodeURIComponent(token)}`;

    console.log("[PodTerminal] Connecting to WebSocket:", wsUrl.replace(/token=[^&]+/, 'token=***'));
    
    terminal.writeln(ephemeral 
      ? "\x1b[1;34m⚡ Criando ephemeral debug container...\x1b[0m\r"
      : "\x1b[1;34m⚡ Conectando ao pod...\x1b[0m\r");

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setIsConnected(true);
      terminal.writeln("\x1b[1;32m✓ Conectado!\x1b[0m\r");
      if (ephemeral) {
        terminal.writeln("\x1b[1;33m🛠️  nicolaka/netshoot\x1b[0m - Ephemeral Debug Container\r");
      }
      terminal.writeln(`\x1b[1;36mPod:\x1b[0m ${pod}\r`);
      terminal.writeln(`\x1b[1;36mContainer:\x1b[0m ${container}\r`);
      terminal.writeln(`\x1b[1;36mShell:\x1b[0m ${shell}\r`);
      terminal.writeln(`\x1b[1;36mLocale:\x1b[0m C.UTF-8 (suporta acentuação)\r`);
      terminal.writeln(`\x1b[1;36mTeclado:\x1b[0m ABNT2 (ç automático)\r`);
      terminal.writeln(`\x1b[1;36mAtalhos:\x1b[0m Ctrl+C=Copiar | Ctrl+V=Colar\r`);
      terminal.writeln("\x1b[2m─────────────────────────────────\x1b[0m\r\n");

      // Enviar dimensões iniciais
      const dims = {
        type: "resize",
        cols: terminal.cols,
        rows: terminal.rows,
      };
      ws.send(JSON.stringify(dims));

      // Handler para input do terminal com suporte a ABNT2 e copiar/colar
      terminal.attachCustomKeyEventHandler((event) => {
        if (event.type !== "keydown") return true;

        // CTRL+C = Copiar (não enviar SIGINT)
        if (event.ctrlKey && event.key === "c" && terminal.hasSelection()) {
          event.preventDefault();
          const selection = terminal.getSelection();
          navigator.clipboard.writeText(selection);
          console.log("[Terminal] Copiado:", selection.substring(0, 50) + "...");
          return false; // Bloqueia xterm de processar
        }

        // CTRL+V = Colar
        if (event.ctrlKey && event.key === "v") {
          event.preventDefault();
          navigator.clipboard.readText().then((text) => {
            if (ws.readyState === WebSocket.OPEN) {
              ws.send(JSON.stringify({ type: "input", data: text }));
              console.log("[Terminal] Colado:", text.substring(0, 50) + "...");
            }
          });
          return false; // Bloqueia xterm de processar
        }

        // Mapeamento ABNT2 customizado por KeyCode
        // O navegador detecta o layout físico do teclado, não o layout lógico
        // Precisamos interceptar os keycodes e mapear para ABNT2
        
        // Debug completo - mostre TODAS as teclas para diagnóstico (comentado para produção)
        // console.log("[Keyboard DEBUG]", {
        //   key: event.key,
        //   code: event.code,
        //   shift: event.shiftKey,
        //   ctrl: event.ctrlKey,
        //   alt: event.altKey,
        //   charCode: event.key.charCodeAt(0),
        // });

        // Semicolon -> ç/Ç (tecla do ç no ABNT2)
        if (event.code === "Semicolon" && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          const char = event.shiftKey ? "Ç" : "ç";
          // Envia via WebSocket para o backend processar corretamente
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "input", data: char }));
          }
          return false; // Bloqueia processamento padrão do xterm
        }

        // Quote -> ~/ (til/crase no ABNT2)
        if (event.code === "Quote" && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          const char = event.shiftKey ? "`" : "~";
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "input", data: char }));
          }
          return false;
        }

        // BracketLeft -> ´/` (acento agudo no ABNT2)
        if (event.code === "BracketLeft" && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          const char = event.shiftKey ? "`" : "´";
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "input", data: char }));
          }
          return false;
        }

        // BracketRight -> [/{ (colchetes no ABNT2)
        if (event.code === "BracketRight" && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          const char = event.shiftKey ? "{" : "[";
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "input", data: char }));
          }
          return false;
        }

        // Backslash (acima do Enter) -> ]/} (colchetes fechamento no ABNT2)
        if (event.code === "Backslash" && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          const char = event.shiftKey ? "}" : "]";
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "input", data: char }));
          }
          return false;
        }
        
        return true; // Deixa xterm processar outras teclas
      });

      terminal.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: "input", data }));
        }
      });
    };

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);
        if (message.type === "output" && message.data) {
          terminal.write(message.data);
        } else if (message.type === "error") {
          terminal.writeln(`\r\n\x1b[1;31m✗ Erro: ${message.data}\x1b[0m\r`);
        }
      } catch (e) {
        // Mensagem raw (sem JSON)
        terminal.write(event.data);
      }
    };

    ws.onerror = (error) => {
      console.error("[PodTerminal] WebSocket error:", error);
      console.error("[PodTerminal] WebSocket URL was:", wsUrl);
      console.error("[PodTerminal] ReadyState:", ws.readyState);
      setIsConnected(false);
      terminal.writeln("\r\n\x1b[1;31m✗ Erro na conexão WebSocket\x1b[0m\r");
      terminal.writeln(`\x1b[2mURL: ${wsUrl}\x1b[0m\r`);
      toast.error("Erro na conexão", {
        description: "Não foi possível conectar ao pod. Verifique o console (F12) para mais detalhes.",
      });
    };

    ws.onclose = (event) => {
      console.log("[PodTerminal] WebSocket closed:", event.code, event.reason);
      setIsConnected(false);
      terminal.writeln("\r\n\x1b[1;33m⚠ Conexão fechada\x1b[0m\r");
      if (event.code !== 1000) {
        terminal.writeln(`\x1b[2mCódigo: ${event.code}, Razão: ${event.reason || 'N/A'}\x1b[0m\r`);
      }
    };
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
            Terminal: {pod} / {container}
          </span>
          <span className="text-xs text-muted-foreground">({shell})</span>
        </div>
        <div className="flex items-center gap-1">
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
            {namespace} / {cluster}
          </span>
        </div>
      </div>
    </div>
  );
};
