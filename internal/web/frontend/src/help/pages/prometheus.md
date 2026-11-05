## Para que serve

Lista os recursos do **Prometheus Stack** do cluster, permite ajustar réplicas e **requests e limits**, reiniciar, e inclui o **Diagnóstico do WAL**: o passo a passo para quando o Prometheus não sobe (em geral `OOMKilled` ao reler o WAL), com a **limpeza do WAL** como último recurso.

## Quando usar

- O Prometheus está em `CrashLoopBackOff`, `OOMKilled` ou não sobe depois de um restart.
- Ajustar memória e CPU do Prometheus, do Alertmanager ou de outros componentes do stack.
- Descobrir por que o pod do Prometheus ficou `Pending` (não agenda em nenhum node).
- Aplicar tolerations ou nodeSelector pelo Helm de forma que persista.

## Como usar (passo a passo)

1. Escolha o cluster. A lista mostra os Deployments, StatefulSets e DaemonSets cujo nome contém `prometheus`, com réplicas e requests atuais. A busca filtra por nome, namespace ou componente. Sem nenhum selecionado, a direita mostra uma tabela com todos.
2. **Clique em um recurso** para abrir o editor: ícone do tipo, componente, **Replicas** (não existe para DaemonSet), **CPU Request**, **Memory Request**, **CPU Limit**, **Memory Limit** e o uso atual de CPU e memória, quando disponível.
3. **Editar Recursos** libera os campos (CPU como `100m`, `0.5` ou `1`; memória como `256Mi` ou `1Gi`) e **Salvar Alterações** aplica por server-side apply.
4. **Rollout** reinicia o recurso, sem alterar valores.
5. Quando o recurso é um **StatefulSet** do Prometheus, aparece o painel **Diagnóstico do WAL**.

## Salvar alterações: o que o aviso quer dizer

O Prometheus é gerenciado por um **operator**. Depois de salvar, a ferramenta avisa em um de três jeitos:

- **"Recurso atualizado com sucesso":** o workload já reflete o valor.
- **"Recurso atualizado no recurso dono":** o StatefulSet é gerado por um recurso `Prometheus`; a alteração foi feita **nele** e o pod é recriado. Um `helm upgrade` posterior **desfaz** a mudança: ajuste também os values do Helm.
- **"Salvo, mas o workload ainda não reflete o valor":** o operator ou o Helm reverteu ou ainda não aplicou. Ajuste o recurso dono ou os values do Helm.

## Diagnóstico do WAL

Somente leitura até você pedir uma limpeza. O botão **Analisar WAL** devolve um veredito, do menos para o mais destrutivo:

| Veredito | Quando | O que a ferramenta faz |
|---|---|---|
| `saudavel` | pod pronto, sem restarts relevantes | nada |
| `aumentar_memoria` | OOM com limit baixo para o tamanho do WAL | só **informa** o valor sugerido; você aplica no editor ou **via Helm** |
| `limpar_wal` | OOM mesmo com memória razoável, WAL corrompido ou limit já no teto | habilita **Limpar WAL…** |
| `investigar` | a causa não é o WAL (imagem, PVC cheio, permissão, pod que nunca agendou) | só mostra o diagnóstico |

A análise lê o recurso, os pods (restarts, OOM, CrashLoop), os logs da execução anterior, os eventos e quem gerencia o recurso (Helm ou Argo CD). Quando o pod **nunca agendou**, explica por que **nenhum node serve** (label faltando, taint não tolerado, node cordonado) e oferece corrigir.

**Análise profunda** (opcional, mas **obrigatória para liberar a limpeza**): cria um pod temporário **somente leitura** que monta o volume no mesmo node, lê os blocos e mostra o **intervalo de dados que seria perdido**. O pod é removido no fim.

## Limpar o WAL (destrutivo)

Apaga `wal/` e `chunks_head/`. **Perde todas as métricas que ainda não viraram bloco compactado** (os blocos já compactados ficam). Antes de começar:

- A análise precisa ter dado `limpar_wal` e ter passado pela **análise profunda**. Cada análise vale por **10 minutos** e autoriza **uma única limpeza**.
- Você escolhe as réplicas e digita **`APAGAR`** e o **nome do recurso** para confirmar.
- **Todas as réplicas ficam desligadas durante a limpeza**, mesmo que só uma seja limpa, porque o operator só aceita `replicas` no recurso `Prometheus`. Em um par HA, a réplica saudável também sai do ar por alguns minutos.
- O progresso aparece ao vivo: anotação de restauração, desligar, aguardar pods, pod temporário, apagar, remover o pod, religar e aguardar Ready. **Cancelar** remove o pod temporário e **religa** o Prometheus.
- Se o servidor cair no meio, a anotação de restauração permanece: o painel mostra **Restaurar**, que religa com as réplicas originais.

**Reincidência:** duas ou mais limpezas em 30 dias marcam o Prometheus como problema crônico e mostram um aviso (não bloqueia). Limpar o WAL trata o sintoma; investigue cardinalidade, memory limit, disco e retenção.

## Aplicar via Helm

O botão **Helm** (e os atalhos **Aplicar … via Helm**) roda `helm upgrade --reuse-values` no release do **kube-prometheus-stack**. Os values atuais são mantidos (recursos e nodeSelector são mesclados), mas a **lista de tolerations de cada componente marcado é substituída** pela que você montou. É o caminho para a mudança **persistir**. É preciso **Simular (dry-run)** antes de **Aplicar**; cada aplicação vira uma **revisão** do release, com histórico e **rollback** no próprio diálogo. Dá para escolher os componentes (Prometheus, Alertmanager, kube-state-metrics, Prometheus Operator), requests e limits e nodeSelector.

## Alertas de integridade

Independente da aba, a ferramenta verifica os Prometheus do cluster ativo (só pela API do Kubernetes, a cada cerca de 60 s) e mostra alertas **críticos** (fora do ar, ou desligado por uma limpeza interrompida) e de **aviso** (reincidência), com o botão **Abrir diagnóstico do WAL**. Isso vale mesmo quando o Prometheus está fora do ar e não conseguiria avisar sozinho. Sem acesso ao cluster (VPN fora), o resultado é "não verificável", nunca um alerta falso.

## Permissões

Analisar e consultar não exigem o grupo SRE. Salvar, Rollout, Limpar, Restaurar, corrigir agendamento e Helm exigem perfil SRE **e** permissão real no cluster; a limpeza confere o RBAC **antes** de desligar qualquer coisa. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster e pode tirar o Prometheus do ar.** Ficam no Change History: salvar recursos, rollout, análise profunda, limpeza de WAL, restauração, correção de agendamento e as operações de Helm (aplicar tolerations e rollback). O histórico guarda recurso, volume, tamanhos e intervalo perdido, **nunca o conteúdo dos dados**.

## Cuidados e erros comuns

- **Se o Prometheus é gerenciado por Helm ou GitOps, pause o sync antes de limpar** o WAL; senão ele pode religar ou desfazer a operação no meio.
- **Aumentar memória costuma resolver sem perder dados.** Só apague o WAL quando o veredito indicar, e leia o intervalo que será perdido.
- Se o OOM aconteceu **depois** de o Prometheus ter servido, apagar o WAL não resolve; aumente a memória.
- A limpeza usa a imagem `busybox:1.36` no pod temporário: o node precisa conseguir baixá-la.
- Mudar o recurso à mão (`Salvar`) pode ser **revertido pelo operator ou pelo Helm**; para persistir, use **via Helm**.
- A ferramenta de limpeza é recente e, segundo o desenho, **não foi validada em cluster real**: na primeira vez em produção, acompanhe de perto.
- O fallback `scripts/prometheus-wal-cleanup.sh` existe para quando a aplicação web não estiver disponível; ele **não** faz a análise prévia.

## Veja também

[Helm](ajuda:helm), [Pods](ajuda:pods), [Events](ajuda:events), [Monitoramento](ajuda:monitoring).
