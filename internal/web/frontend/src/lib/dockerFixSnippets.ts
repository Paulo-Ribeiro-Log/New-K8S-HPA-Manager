// Textos de instrução pra corrigir o Docker do servidor no modo "local" (Direto do servidor) das
// ferramentas de Teste sob Demanda (Banco de Dados e Kafka) — compartilhado entre
// DatabaseTestTab.tsx e KafkaTestTab.tsx porque a checagem (checkDockerStatus no backend) não tem
// nada específico de engine, é sobre o Docker do host em si.

// Passo a passo de instalação do Docker Engine — cobre Ubuntu e WSL2 (ambiente alvo principal
// desta app, ver CLAUDE.md). Script oficial da Docker (get.docker.com) funciona nos dois; o passo
// 3 é o que mais varia — WSL2 normalmente não sobe serviços sozinho no boot (sem systemd por
// padrão), por isso o `service docker start` como primeira opção + systemctl como alternativa.
export const DOCKER_INSTALL_SNIPPET = `# 1. Instala o Docker Engine (script oficial — cobre Ubuntu e WSL2)
curl -fsSL https://get.docker.com | sudo sh

# 2. Permite rodar docker sem sudo (evita pedir senha em cada teste)
sudo usermod -aG docker $USER
newgrp docker   # ou feche e reabra o terminal/WSL

# 3. Inicia o serviço — WSL2 normalmente não sobe serviços sozinho no boot
sudo service docker start
# alternativa em distros com systemd habilitado no WSL2:
# sudo systemctl enable --now docker`;

// "address_pool_exhausted": dockerd chega a tentar subir mas crasha ao criar a rede bridge padrão
// com "all predefined address pools have been fully subnetted" — causa mais comum é VPN
// corporativa com split-tunnel "route everything", que anuncia rota pras faixas 10.0.0.0/8 +
// 172.16.0.0/12 + 192.168.0.0/16 inteiras (exatamente onde o Docker tenta alocar por padrão).
// Fix: apontar o Docker pra uma faixa fora do que a VPN cobre — 100.64.0.0/10 (RFC 6598, CGNAT,
// raramente roteada por VPN corporativa).
export const DOCKER_ADDRESS_POOL_FIX_SNIPPET = `# O Docker não conseguiu criar a rede padrão porque a VPN corporativa está
# roteando toda a faixa 10.0.0.0/8 + 172.16.0.0/12 + 192.168.0.0/16 — não
# sobra bloco livre nessas faixas privadas clássicas. Fix: usar uma faixa
# fora do que a VPN cobre.
sudo tee /etc/docker/daemon.json > /dev/null <<'JSON'
{
  "default-address-pools": [
    { "base": "100.64.0.0/10", "size": 24 }
  ]
}
JSON
sudo systemctl reset-failed docker.service
sudo systemctl restart docker`;

// Escolhe título + snippet conforme DBDockerStatus.reason — cada causa tem um fix diferente,
// mostrar sempre "instale o Docker" quando ele já está instalado/rodando seria confuso (ver
// db_test_docker.go no backend, mesma classificação — reaproveitada pelo Teste de Kafka).
export const DOCKER_FIX_BY_REASON: Record<string, { title: string; snippet: string }> = {
  not_installed: { title: "Docker não está instalado neste servidor", snippet: DOCKER_INSTALL_SNIPPET },
  permission_denied: { title: "Usuário do servidor sem permissão para o Docker", snippet: DOCKER_INSTALL_SNIPPET },
  address_pool_exhausted: { title: "Docker não conseguiu criar a rede padrão (conflito com VPN)", snippet: DOCKER_ADDRESS_POOL_FIX_SNIPPET },
  daemon_unreachable: { title: "Docker instalado, mas o daemon não respondeu", snippet: DOCKER_INSTALL_SNIPPET },
};
