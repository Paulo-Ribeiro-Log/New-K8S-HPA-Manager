Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso). Aqui está só o que é específico dela.

## Para que serve

Lista **DaemonSets**, mostra os pods de cada um, edita o YAML, reinicia o rollout e exclui.

## Quando usar

- Ver se um DaemonSet (agentes, coletores, CNI) está saudável em todos os nodes.
- Reiniciar um agente depois de mudar a configuração.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace. DaemonSets com problema ficam destacados ("DaemonSet com problemas").
2. **Clique em uma linha** para ver os **pods** do DaemonSet, com as métricas de cada um.
3. Para editar, abra o editor do DaemonSet e siga o fluxo [Diff, Validar, Aplicar](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar). A confirmação se chama **Confirmar Aplicação**.

## O que é específico desta aba

| Ação | O que faz |
|---|---|
| **Rollout Restart** | recria os pods do DaemonSet, com diálogo de confirmação |
| **Excluir** | apaga o DaemonSet, com diálogo de confirmação |

A lista de pods casa os pods **pelo prefixo do nome** (`<daemonset>-...`), não pelo dono real. Dois DaemonSets de nomes parecidos podem, por isso, aparecer misturados.

## Permissões

Os botões que alteram só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster.** Aplicar e **Rollout Restart** ficam no Change History; **excluir não fica**.

## Cuidados e erros comuns

- **Rollout Restart** recria o agente **em todos os nodes**: um DaemonSet de rede ou de logs pode interromper o serviço no cluster inteiro enquanto reinicia.
- Excluir um DaemonSet de sistema (rede, armazenamento) pode **derrubar o cluster**.
- A estratégia de atualização do DaemonSet define quantos nodes reiniciam por vez: confira antes.

## Veja também

[Deployments](ajuda:deployments), [Remoção de Aplicação](ajuda:app-removal).
