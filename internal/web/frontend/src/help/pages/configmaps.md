Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso). Aqui está só o que é específico dela.

## Para que serve

Lista, edita, cria e exclui **ConfigMaps**, e mostra se cada um está em uso.

## Quando usar

- Ajustar a configuração de uma aplicação guardada num ConfigMap.
- Descobrir ConfigMaps **órfãos** que podem ser removidos.
- Saber quais workloads dependem de um ConfigMap antes de alterá-lo.

## Como usar (passo a passo)

1. Escolha o cluster e, se quiser, o namespace.
2. Busque por **nome ou label** e abra o ConfigMap.
3. Edite o YAML, confira o **Diff**, use **Validar** e depois **Aplicar** (veja [o fluxo de edição](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar)).
4. Para um ConfigMap novo, escolha um namespace e use **Criar novo ConfigMap**.

## O que é específico desta aba

| Item | O que mostra |
|---|---|
| **Uso** | **Órfão** quando nenhum Pod do cluster o referencia; senão **Usado por: ...** com os workloads (Deployment, DaemonSet, StatefulSet ou Job). O cruzamento considera volumes, `envFrom` e `env` dos pods |
| **Labels** | seção que expande as labels do ConfigMap |
| **Checkpoint (Ctrl+S)** | guarda o estado atual no histórico de desfazer do editor, no seu navegador. **Não aplica nada no cluster** |

## Permissões

Os botões que alteram só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster.** Aplicar, criar e excluir ficam no Change History.

## Cuidados e erros comuns

- **Órfão não significa inútil:** o cruzamento só enxerga pods existentes agora. Um ConfigMap usado por um CronJob que ainda não rodou pode aparecer como órfão.
- Pods já em execução **não recarregam** um ConfigMap alterado quando ele foi usado como variável de ambiente; é preciso reiniciar o workload.

## Veja também

[Secrets](ajuda:geral-abas-de-recurso), [Deployments](ajuda:deployments).
