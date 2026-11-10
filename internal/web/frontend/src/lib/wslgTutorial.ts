// Tutorial de instalação do modo gráfico (WSLg) no WSL2. Lógica pura: recebe o relatório do
// backend (GET /api/v1/system/display) e devolve os passos, já marcando o que está resolvido.

export interface DisplayCapability {
  id: string;
  label: string;
  ok: boolean;
  via?: string;
  detail?: string;
}

export interface DisplayReport {
  os: string;
  is_wsl: boolean;
  wsl_version?: number;
  distro?: string;
  interop: boolean;
  wslg: { present: boolean; x11_reachable: boolean; wayland_reachable: boolean; display_env: string; suggest_display?: string };
  graphical_display: boolean;
  chrome_path?: string;
  mode: string;
  summary: string;
  capabilities: DisplayCapability[];
  notes?: string[];
}

export interface TutorialStep {
  id: string;
  title: string;
  where: "windows" | "wsl";
  done: boolean;
  text: string;
  commands: string[];
}

/** A mensagem de falta de suporte só aparece no WSL, quando o robô (Teams/ServiceNow) não tem display. */
export function needsGraphicalNotice(r: DisplayReport | undefined): boolean {
  if (!r || !r.is_wsl) return false;
  return r.capabilities.some((c) => (c.id === "teams_automation" || c.id === "servicenow_login") && !c.ok);
}

export function tutorialSteps(r: DisplayReport): TutorialStep[] {
  const distro = r.distro || "<distro>";
  const wsl2 = r.wsl_version === 2;
  return [
    {
      id: "wsl2",
      title: "Usar o WSL 2",
      where: "windows",
      done: wsl2,
      text: "O WSLg só existe no WSL 2. No PowerShell do Windows, confira a versão da distribuição e converta se for 1.",
      commands: ["wsl -l -v", `wsl --set-version ${distro} 2`],
    },
    {
      id: "update",
      title: "Atualizar o WSL",
      where: "windows",
      done: r.wslg.present,
      text: "O WSLg vem com o pacote do WSL da Microsoft. Atualize; se falhar, baixe direto da web.",
      commands: ["wsl --version", "wsl --update", "wsl --update --web-download"],
    },
    {
      id: "wslconfig",
      title: "Conferir o .wslconfig",
      where: "windows",
      done: r.wslg.present,
      text: "Em %UserProfile%\\.wslconfig, a seção [wsl2] não pode ter guiApplications=false (remova a linha ou use true).",
      commands: ["notepad $env:UserProfile\\.wslconfig"],
    },
    {
      id: "restart",
      title: "Reiniciar o WSL",
      where: "windows",
      done: r.wslg.present && r.graphical_display,
      text: "Desligue todas as distribuições e abra o terminal do WSL de novo.",
      commands: ["wsl --shutdown"],
    },
    {
      id: "verify",
      title: "Conferir no Linux",
      where: "wsl",
      done: r.graphical_display,
      text: "Deve existir /mnt/wslg, o socket X11 e DISPLAY=:0.",
      commands: ["ls /mnt/wslg", "ls /tmp/.X11-unix", "echo $DISPLAY"],
    },
    {
      id: "chrome",
      title: "Instalar o Google Chrome no Linux",
      where: "wsl",
      done: !!r.chrome_path,
      text: "O robô usa o Chrome instalado na distribuição.",
      commands: [
        "wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb",
        "sudo apt install -y ./google-chrome-stable_current_amd64.deb",
      ],
    },
    {
      id: "restart-app",
      title: "Reiniciar o servidor e testar de novo",
      where: "wsl",
      done: false,
      text: "Reinicie o new-k8s-hpa a partir de um terminal que já tenha o display (DISPLAY definido) e clique em “Testar de novo”.",
      commands: ["./build/new-k8s-hpa diagnose-display"],
    },
  ];
}
