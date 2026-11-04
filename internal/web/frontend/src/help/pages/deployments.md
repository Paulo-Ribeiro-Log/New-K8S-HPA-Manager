## Para que serve

Lista os Deployments de um cluster e permite **inspecionar, editar e operar** cada um: ver os pods, editar o YAML, escalar, reiniciar, fazer rollback e rodar análises.

## Quando usar

- Ver rapidamente quais Deployments estão com problema e quais pods estão por trás.
- Editar o manifesto de um Deployment e aplicar direto no cluster.
- Escalar, reiniciar ou reverter uma versão.
- Operar vários Deployments de uma vez (lote).

## Como usar (passo a passo)

1. Escolha o **cluster** no cabeçalho e o **namespace** (ou todos).
2. A **tabela de Deployments** mostra, no estilo do k9s, as colunas **NAME**, **READY**, **UP-TO-DATE**, **AVAILABLE** e **AGE**. Deployments com problema ficam destacados, e um selo indica pods com falha individual (por exemplo `CrashLoopBackOff`) que o READY sozinho não mostra.
3. **Clique em uma linha** para ver os **pods daquele Deployment**. A lista traz só os pods cujo dono é o Deployment (pelo ReplicaSet), não os que apenas têm parte do nome igual. Se o Deployment não tem pod nenhum, a lista fica vazia.
4. **Clique em um pod** para o resumo rápido, com logs e detalhes.
5. Para editar, use o ícone de edição da linha (ou escolha o Deployment na lista da esquerda): abre o **manifesto YAML**.
6. Edite, confira o **Diff Visual** e use **Confirmar aplicação**. O YAML é aplicado direto no cluster.

## Ações do Deployment

| Ação | O que faz |
|---|---|
| **Análise Preditiva** | coleta métricas do Deployment e roda uma análise com IA (usa o provedor de IA configurado) |
| **Histórico de Análises** | análises preditivas anteriores |
| **Comportamento** | gráfico de comportamento do Deployment (o mesmo do resumo rápido do pod) |
| **Selo do Spinnaker** | aparece quando há dado do Spinnaker para o Deployment; clicar abre o acompanhamento do rollout |
| **Analisar com AI** | abre o diagnóstico por IA deste Deployment (veja a aba AI Diagnostics) |
| **Describe** | o equivalente a `kubectl describe` |
| **Recarregar YAML** | descarta a edição local e relê o manifesto do cluster |
| **Scale** | muda o número de réplicas |
| **Rollout Restart** | recria todos os pods, e mostra o progresso do rollout |
| **Rollback Deployment** | volta a uma versão anterior (veja abaixo) |
| **Deletar Deployment** | apaga o Deployment |
| **Edição Lado a Lado** | abre o Deployment na ferramenta de comparação |

### Ações em lote

Marque os Deployments na tabela (a caixa do cabeçalho seleciona todos, ou use **Espaço** na linha) e use **Escalar**, **Rollout restart** ou **Deletar** nos selecionados.

## Rollback

O rollback tem **6 modos**, e o diálogo só oferece os que fazem sentido para aquele Deployment:

| Modo | Disponível para |
|---|---|
| **Helm** | Deployments gerenciados por Helm |
| **K8s nativo** | Deployments não gerenciados por Helm |
| **Nexus** | Deployments gerenciados por Helm (aplica values históricos) |
| **Imagem** | Deployments não gerenciados por Helm (troca a tag da imagem) |
| **Spinnaker** | ambos |
| **Arquivos** | ambos |

Todos os modos exigem **ver o diff ou o resumo antes de confirmar**. Durante a operação, a ferramenta liga a label de bypass do Kyverno no namespace (`devops.k8s.io/kyverno-bypass`) e a remove no fim; se o cluster não usa essa política, a label não tem efeito.

## Permissões

- Os botões que alteram (**Scale**, **Rollout Restart**, **Rollback**, **Deletar**) ficam **desabilitados** quando o seu usuário não tem permissão de escrita no namespace (RBAC do cluster). O tooltip diz: "Sem permissão de escrita neste namespace (K8s RBAC)".
- As rotas de escrita são de perfil SRE no desenho da aplicação; quem barra de fato é o RBAC do cluster.

## Impacto

**Altera o cluster.** Aplicar YAML, escalar, reiniciar, reverter e deletar mudam recursos em produção. **Aplicar, escalar, reiniciar, reverter e deletar (individual ou em lote) ficam registrados no Change History.** **Deletar** e **Rollout Restart** derrubam pods.

## Cuidados e erros comuns

- **Rollout Restart** recria **todos** os pods, seguindo a estratégia de rolling update do Deployment: confira `maxUnavailable` antes de fazer isso em produção.
- **Deletar** em lote apaga todos os selecionados.
- A lista de pods vazia num Deployment com erro costuma indicar problema antes dos pods (quota, admissão, falha de réplica): olhe o **Describe**.
- Mensagem **"Rota sem backend"**: o Service do Deployment não tem pod pronto atrás dele.
- Se o Deployment é reaplicado por Helm, Spinnaker ou outro pipeline, a sua edição direta pode ser **sobrescrita** no próximo deploy.

## Veja também

[DaemonSets](ajuda:daemonsets), [StatefulSets](ajuda:statefulsets), [abas de recurso](ajuda:geral-abas-de-recurso), [Remoção de Aplicação](ajuda:app-removal); e as abas Pods, HPAs, Helm e Staging.
