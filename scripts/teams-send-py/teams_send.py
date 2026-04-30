#!/usr/bin/env python3
"""
teams_send.py — Envia mensagem para um chat/grupo do Teams via CDP.

Sem dependências externas (apenas stdlib Python 3.6+).
Reusa a sessão Chrome em ~/.k8s-hpa-manager/teams-session/

Uso:
    python3 teams_send.py "Sua mensagem aqui"
    python3 teams_send.py           # usa DEFAULT_MESSAGE abaixo
    python3 teams_send.py --html "<b>negrito</b>"

O script lança o Chrome com a sessão existente, aguarda o Teams carregar
e executa o envio via fetch() dentro do contexto da página (bypass MCAS).
"""

import base64
import hashlib
import http.client
import json
import os
import socket
import struct
import subprocess
import sys
import time

# ─── Configuração ─────────────────────────────────────────────────────────────
THREAD_ID = "19:981ac15833424e878e5b276cf84ff84a@thread.v2"
SESSION_DIR = os.path.expanduser("~/.k8s-hpa-manager/teams-session")
CDP_PORT = 9224          # Porta CDP dedicada (9222=padrão, 9223=ServiceNow)
WAIT_LOAD_S = 20         # Segundos para o Teams inicializar após detectar URL
DEFAULT_MESSAGE = "Olá! Mensagem de teste enviada via script Python."

# ─── WebSocket mínimo (stdlib) ─────────────────────────────────────────────────
class _WS:
    """Cliente WebSocket mínimo para uso com CDP (sem dependências)."""

    def __init__(self, host: str, port: int, path: str):
        self._s = socket.create_connection((host, port), timeout=30)
        key = base64.b64encode(os.urandom(16)).decode()
        hs = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {host}:{port}\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\n"
            "Sec-WebSocket-Version: 13\r\n\r\n"
        )
        self._s.sendall(hs.encode())
        buf = b""
        while b"\r\n\r\n" not in buf:
            buf += self._s.recv(4096)
        if b"101" not in buf:
            raise ConnectionError(f"Upgrade WebSocket falhou: {buf[:200]}")

    def send_text(self, text: str):
        """Envia frame de texto (mascarado — obrigatório cliente→servidor)."""
        data = text.encode()
        n = len(data)
        mask = os.urandom(4)
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(data))
        header = bytearray([0x81])                      # FIN + opcode=text
        if n <= 125:
            header.append(0x80 | n)
        elif n <= 65535:
            header += bytearray([0x80 | 126]) + struct.pack(">H", n)
        else:
            header += bytearray([0x80 | 127]) + struct.pack(">Q", n)
        header += mask
        self._s.sendall(bytes(header) + masked)

    def recv_text(self, timeout: float = 90) -> str:
        """Recebe próximo frame de texto."""
        self._s.settimeout(timeout)
        h = self._exact(2)
        masked = bool(h[1] & 0x80)
        n = h[1] & 0x7F
        if n == 126:
            n = struct.unpack(">H", self._exact(2))[0]
        elif n == 127:
            n = struct.unpack(">Q", self._exact(8))[0]
        mask_key = self._exact(4) if masked else b""
        data = self._exact(n)
        if masked:
            data = bytes(b ^ mask_key[i % 4] for i, b in enumerate(data))
        return data.decode("utf-8", errors="replace")

    def _exact(self, n: int) -> bytes:
        buf = b""
        while len(buf) < n:
            chunk = self._s.recv(n - len(buf))
            if not chunk:
                raise ConnectionError("Conexão CDP fechada inesperadamente")
            buf += chunk
        return buf

    def close(self):
        try:
            self._s.close()
        except Exception:
            pass


def _cdp(ws: _WS, method: str, params: dict = None, msg_id: int = 1, timeout: float = 90):
    """Envia comando CDP e aguarda resposta com o mesmo id (descarta eventos)."""
    ws.send_text(json.dumps({"id": msg_id, "method": method, "params": params or {}}))
    deadline = time.time() + timeout
    while time.time() < deadline:
        remaining = deadline - time.time()
        if remaining <= 0:
            break
        try:
            obj = json.loads(ws.recv_text(timeout=min(remaining, 10)))
            if obj.get("id") == msg_id:
                return obj
        except socket.timeout:
            continue
    raise TimeoutError(f"Timeout aguardando resposta CDP para {method}")


# ─── Helpers Chrome ────────────────────────────────────────────────────────────
def _find_chrome() -> str:
    for p in [
        "/usr/bin/google-chrome-stable", "/usr/bin/google-chrome",
        "/usr/bin/chromium-browser", "/usr/bin/chromium",
    ]:
        if os.path.exists(p):
            return p
    for name in ["google-chrome-stable", "google-chrome", "chromium"]:
        r = subprocess.run(["which", name], capture_output=True, text=True)
        if r.returncode == 0:
            return r.stdout.strip()
    return ""


def _kill_chrome(session_dir: str):
    for sig in ["-TERM", "-9"]:
        r = subprocess.run(["pgrep", "-f", session_dir], capture_output=True, text=True)
        for pid in r.stdout.split():
            subprocess.run(["kill", sig, pid], capture_output=True)
        time.sleep(0.5)


def _cdp_pages(port: int):
    try:
        conn = http.client.HTTPConnection("localhost", port, timeout=5)
        conn.request("GET", "/json/list")
        return json.loads(conn.getresponse().read())
    except Exception:
        return []


def _teams_page(port: int, timeout: float = 180) -> dict:
    deadline = time.time() + timeout
    while time.time() < deadline:
        for p in _cdp_pages(port):
            u = p.get("url", "")
            if ("teams.microsoft.com" in u and
                    "/error" not in u and
                    "login.microsoftonline" not in u and
                    u not in ("about:blank", "")):
                return p
        time.sleep(2)
    return {}


# ─── JS de envio (portado do sender.go) ───────────────────────────────────────
_JS = """\
(async () => {
    // 1. Bearer token do localStorage (MSAL)
    let bearerToken = '', userMRI = '', displayName = '';

    for (let i = 0; i < localStorage.length; i++) {
        const k = localStorage.key(i);
        if (!k) continue;
        const v = localStorage.getItem(k);
        if (!v) continue;

        if (k.toLowerCase().includes('accesstoken') &&
            (k.toLowerCase().includes('ic3.teams') ||
             k.toLowerCase().includes('teams.officeclient') ||
             k.toLowerCase().includes('teams.communication'))) {
            try {
                const o = JSON.parse(v);
                const t = o.secret || o.access_token || o.token || '';
                if (t && t.length > 50) bearerToken = t;
            } catch {}
        }
        if (!userMRI && k.toLowerCase().includes('account')) {
            try {
                const o = JSON.parse(v);
                const mri = o.localAccountId || '';
                if (mri && !mri.includes('.')) userMRI = '8:orgid:' + mri;
                if (o.name && !displayName) displayName = o.name;
            } catch {}
        }
    }

    // Fallback: qualquer JWT longo (eyJ...)
    if (!bearerToken) {
        for (let i = 0; i < localStorage.length; i++) {
            const k = localStorage.key(i);
            if (!k) continue;
            try {
                const o = JSON.parse(localStorage.getItem(k) || '');
                for (const c of [o.secret, o.access_token, o.token]) {
                    if (typeof c === 'string' && c.length > 100 && c.startsWith('eyJ')) {
                        bearerToken = c; break;
                    }
                }
            } catch {}
            if (bearerToken) break;
        }
    }

    if (!bearerToken)
        return JSON.stringify({ ok: false, error: 'Bearer token não encontrado no localStorage' });

    // 2. MRI via payload JWT se não encontrado no localStorage
    if (!userMRI) {
        try {
            const p = bearerToken.split('.');
            if (p.length >= 2) {
                const pl = JSON.parse(atob(p[1].replace(/-/g,'+').replace(/_/g,'/')));
                if (pl.oid || pl.sub) userMRI = '8:orgid:' + (pl.oid || pl.sub);
                if (!displayName) displayName = pl.name || pl.upn || '';
            }
        } catch {}
    }
    if (!userMRI)     userMRI     = '8:orgid:unknown';
    if (!displayName) displayName = 'Unknown User';

    // 3. Envio via fetch (mesmo contexto da página — bypass MCAS)
    const baseURL     = location.protocol + '//' + location.host;
    const threadId    = '__THREAD_ID__';
    const enc         = encodeURIComponent(threadId);
    const url         = baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + enc + '/messages';
    const now         = new Date().toISOString();
    const msgId       = String(Date.now()) + String(Math.floor(Math.random() * 1000000)).padStart(6, '0');

    const body = JSON.stringify({
        amsreferences: [], callId: '', clientmessageid: msgId,
        composetime: now, content: '__HTML__', contenttype: 'Text',
        conversationLink: baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + enc,
        conversationid: threadId, crossPostChannels: [],
        from: userMRI, fromUserId: userMRI, id: '-1',
        imdisplayname: displayName, messagetype: 'RichText/Html',
        originalarrivaltime: now,
        properties: {
            cards:'[]', files:'[]', formatVariant:'TEAMS',
            importance:'', links:'[]', mentions:'[]',
            onbehalfof:null, policyViolation:null, subject:'', title:''
        },
        state: 0, type: 'Message', version: '0',
    });

    try {
        const r = await fetch(url, {
            method: 'POST',
            headers: {
                'authorization': 'Bearer ' + bearerToken,
                'content-type': 'application/json',
                'behavioroverride': 'redirectAs404',
                'x-ms-migration': 'True',
                'x-ms-request-priority': '0',
                'x-ms-test-user': 'False',
            },
            body,
        });
        let errMsg = '';
        if (!r.ok) { try { errMsg = (await r.text()).slice(0, 300); } catch {} }
        return JSON.stringify({ ok: r.ok, status: r.status, error: errMsg,
                                mri: userMRI, display_name: displayName });
    } catch (e) {
        return JSON.stringify({ ok: false, status: 0, error: String(e) });
    }
})()
"""


def _escape_js(s: str) -> str:
    return (s.replace("\\", "\\\\")
             .replace("'", "\\'")
             .replace("\n", "\\n")
             .replace("\r", ""))


# ─── main ──────────────────────────────────────────────────────────────────────
def main():
    # Argumento: texto ou --html <html>
    raw_html = False
    message = DEFAULT_MESSAGE
    args = sys.argv[1:]
    if args and args[0] == "--html" and len(args) >= 2:
        html_content = args[1]
        raw_html = True
    elif args:
        message = " ".join(args)

    if not raw_html:
        html_content = f"<p>{message}</p>"

    if not os.path.isdir(SESSION_DIR):
        print(f"ERRO: Sessão não encontrada em {SESSION_DIR}")
        print("Autentique no Teams pelo servidor da aplicação primeiro.")
        sys.exit(1)

    print("=" * 62)
    print(" Teams — Envio de mensagem via CDP (sem dependências)")
    print("=" * 62)
    print(f" Thread  : {THREAD_ID}")
    preview = html_content[:80] + ("..." if len(html_content) > 80 else "")
    print(f" Conteúdo: {preview}")
    print(f" Sessão  : {SESSION_DIR}")
    print()

    # 1. Matar Chrome existente com essa sessão
    print("[1/4] Encerrando Chrome existente com essa sessão...")
    _kill_chrome(SESSION_DIR)
    time.sleep(0.8)

    chrome = _find_chrome()
    if not chrome:
        print("ERRO: Chrome/Chromium não encontrado.")
        sys.exit(1)
    print(f"      Binário: {chrome}")

    # 2. Lançar Chrome com CDP
    print(f"[2/4] Iniciando Chrome com CDP na porta {CDP_PORT}...")
    subprocess.Popen(
        [
            chrome,
            f"--remote-debugging-port={CDP_PORT}",
            f"--user-data-dir={SESSION_DIR}",
            "--no-first-run",
            "--no-default-browser-check",
            "--disable-blink-features=AutomationControlled",
            "--no-sandbox",
            "--disable-setuid-sandbox",
            "https://teams.microsoft.com/v2/",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    time.sleep(3)

    # 3. Aguardar Teams carregar
    print("[3/4] Aguardando Teams carregar (máx 3 min)...")
    page = _teams_page(CDP_PORT, timeout=180)
    if not page:
        print("ERRO: Timeout aguardando Teams. Verifique se a sessão está válida.")
        sys.exit(1)
    print(f"      URL: {page['url']}")
    print(f"      Aguardando inicialização ({WAIT_LOAD_S}s)...")
    time.sleep(WAIT_LOAD_S)

    # Pegar a página mais recente (pode ter redirecionado para MCAS)
    for p in _cdp_pages(CDP_PORT):
        u = p.get("url", "")
        if ("teams.microsoft.com" in u and "/error" not in u and
                "login.microsoftonline" not in u and u not in ("about:blank", "")):
            page = p
            break

    ws_url: str = page.get("webSocketDebuggerUrl", "")
    if not ws_url:
        print("ERRO: webSocketDebuggerUrl não disponível na página do Teams.")
        sys.exit(1)

    # Extrair path do ws URL (ws://localhost:PORT/devtools/page/XXX)
    ws_path = ws_url.split(f"localhost:{CDP_PORT}", 1)[-1]

    # 4. Enviar via CDP
    print("[4/4] Conectando via WebSocket CDP e enviando mensagem...")
    js = (_JS
          .replace("__THREAD_ID__", THREAD_ID)
          .replace("__HTML__", _escape_js(html_content)))

    ws = _WS("localhost", CDP_PORT, ws_path)
    try:
        resp = _cdp(ws, "Runtime.evaluate", {
            "expression": js,
            "awaitPromise": True,
            "returnByValue": True,
        }, msg_id=1, timeout=90)
    finally:
        ws.close()

    # Parse resposta
    try:
        value = resp.get("result", {}).get("result", {}).get("value", "")
        out = json.loads(value) if isinstance(value, str) else (value or {})
    except Exception as e:
        print(f"ERRO ao interpretar resposta CDP: {e}")
        print(f"Resposta raw: {json.dumps(resp, indent=2)[:500]}")
        sys.exit(1)

    if out.get("error"):
        print(f"\n✗ Erro: {out['error']}")
        sys.exit(1)

    if out.get("ok"):
        print(f"\n✓ Mensagem enviada com sucesso! (HTTP {out.get('status')})")
        print(f"  Remetente : {out.get('display_name')} ({out.get('mri')})")
    else:
        print(f"\n✗ Falha no envio (HTTP {out.get('status')}): {out.get('error', 'erro desconhecido')}")
        sys.exit(1)


if __name__ == "__main__":
    main()
