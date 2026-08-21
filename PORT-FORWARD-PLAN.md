# Plano: Port Forward (modal, ferramenta genérica para pods)

**Status**: ✅ implementado (não requereu fases — escopo definido de uma vez).

## Pedido original

> "percebi que nossa aplicação não tem um mecanismo de port forward para os pods. desenvolva um
> mecanismo extremamente completo e que seja exibido em um modal."

## Desenho

Diferente dos dois mecanismos de port-forward que já existiam no projeto:

- `internal/config/portforward.go` — cache **interno** de túneis, usado só pelo próprio servidor
  pra alcançar um `Service` (ex: Prometheus in-cluster no GKE). Sem controle de porta local/bind
  address, sem estatísticas, sem start/stop pelo usuário.
- `internal/web/handlers/portforward.go` — infraestrutura **antiga e não relacionada**, específica
  do pod do Kiali (`kubectl port-forward` como subprocesso, porta fixa 20001). Mantida intocada —
  ver nota no topo do arquivo.

Este plano cobre uma ferramenta nova, **genérica**, pra encaminhar qualquer porta de qualquer pod,
com controle total do usuário — `internal/portforward/` (pacote novo).

### Mecanismo

Reaproveita o túnel SPDY real do `client-go` (`k8s.io/client-go/tools/portforward`, o mesmo usado
por `kubectl port-forward`), mas não expõe a porta efêmera que essa biblioteca abre internamente
(sempre em `127.0.0.1`, sem alternativa — confirmado lendo o código vendorizado). Em vez disso:

1. Abre o túnel SPDY normalmente, deixando a biblioteca escolher uma porta efêmera em
   `127.0.0.1`.
2. Abre um **listener TCP próprio**, no endereço/porta que o usuário escolheu (padrão `0.0.0.0`,
   pra ficar acessível de qualquer host que já alcance o servidor — importante em cenários WSL2
   servidor + browser Windows, ou o servidor acessado por outros usuários da rede).
3. Faz proxy bidirecional (`io.Copy`) entre esse listener e a porta efêmera do túnel — ganha de
   quebra visibilidade total por conexão (bytes trafegados, contagem de conexões, última
   atividade), que a biblioteca do client-go não expõe.

### Ciclo de vida

- `IdleTimeout` (60min sem nenhuma conexão nova aceita) e `MaxDuration` (8h) — auto-encerram a
  sessão, mesma rede de segurança de outros caches TTL do projeto.
- Sessões encerradas (erro ou parada manual) continuam visíveis por 15min (`retentionAfterStop`)
  antes de sumir da lista — dá tempo do usuário ver o motivo antes de acumular lixo.
- `StopAll()` plugado nos 3 caminhos de shutdown do servidor (mesmo padrão de
  `teams.CloseBrowser()`).

### Segurança

- `Start`/`Stop` atrás de `RequireSREGroup()` — abre acesso de rede real a um pod.
- Bind address restrito a só dois valores (`0.0.0.0` ou `127.0.0.1`), nunca um IP arbitrário.
- Lista de sessões é **global** (visível a qualquer usuário autenticado) — mesma transparência de
  outras ferramentas server-side do projeto (evita duas pessoas abrirem túneis duplicados pro
  mesmo pod sem saber).

### Frontend

Dois pontos de entrada pro mesmo `PortForwardModal.tsx`: um genérico (botão "Port Forward" na
barra de abas) e um contextual (botão dentro do `PodQuickViewModal.tsx`, pré-preenchido com o pod
sendo visto) — ambos compartilham a mesma lista global de sessões via polling (3s, só enquanto o
modal está aberto).

## Validação

Testado ponta a ponta via API HTTP real (não só unit test) contra um pod real de um cluster AKS
(`nginx-ingress-controller`, porta 80): túnel aberto, requisição HTTP real através dele (resposta
`404` genuína do nginx dentro do pod), estatísticas (bytes/conexões) conferidas, `Stop` confirmado
via teste de reconexão (porta local fecha de fato). Erro de pod inexistente também validado
(mensagem clara, sem crash).

## Nota de incidente durante o desenvolvimento

Um comando de limpeza (`rm -rf .../internal/portforward`) executado por engano com `;` em vez de
`&&` também apagou `internal/web/handlers/portforward.go` — que **já existia no git**, rastreado,
como parte da infraestrutura do Kiali (não relacionado a este plano). Detectado imediatamente
(`git status` mostrou `M` em vez de `??` num arquivo que eu pensava ser novo) e restaurado via
`git checkout HEAD -- internal/web/handlers/portforward.go` antes de prosseguir. Lição: `rm -rf`
com múltiplos alvos deve sempre confirmar cada caminho individualmente antes de rodar, e
`git status`/`git diff --stat HEAD` é a forma mais rápida de detectar esse tipo de dano — um
arquivo "novo" que aparece como `M` (modificado) em vez de `??` (não rastreado) é sinal de que ele
já existia e foi sobrescrito, não criado. Handler novo movido para `internal/web/handlers/
pod_portforward.go` (nome que não colide) pra não repetir o problema.
