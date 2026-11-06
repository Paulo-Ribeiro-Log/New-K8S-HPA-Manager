const a=`Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso). Aqui está só o que é específico dela.

## Para que serve

Lista **StatefulSets**, edita o YAML, **escala**, reinicia o rollout e exclui.

## Quando usar

- Mudar o número de réplicas de um banco, fila ou outro serviço com estado.
- Reiniciar um StatefulSet depois de mudar a configuração.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace. StatefulSets com problema ficam destacados ("StatefulSet com problemas").
2. Para editar, abra o editor do StatefulSet e siga o fluxo [Diff, Validar, Aplicar](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar). A confirmação se chama **Confirmar Aplicação**.
3. Para escalar, use a ação de escala: ajuste o número de réplicas com **−** e **+** (ou digitando) e confirme.

## O que é específico desta aba

| Ação | O que faz |
|---|---|
| **Escalar** | muda o número de réplicas. **Zero réplicas encerra todas as instâncias** (a tela avisa) |
| **Rollout Restart** | recria os pods seguindo a estratégia de atualização do StatefulSet, com diálogo de confirmação |
| **Excluir** | apaga o StatefulSet, com diálogo de confirmação |

Esta aba **não abre a lista de pods**; para ver os pods, use a aba Pods.

## Permissões

Os botões que alteram só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster.** Aplicar, **Rollout Restart**, **Escalar** e excluir ficam no Change History.

## Cuidados e erros comuns

- **Por padrão, os PVCs não são apagados** ao escalar para baixo nem ao excluir o StatefulSet: os volumes e os dados ficam, e continuam gerando custo (a menos que o StatefulSet defina \`persistentVolumeClaimRetentionPolicy\`).
- Reduzir réplicas de um banco ou de um cluster com quórum pode **perder dados ou disponibilidade**: confira a replicação antes.
- Alguns campos do \`spec\` (como \`volumeClaimTemplates\` e \`serviceName\`) não podem ser alterados depois de criado: o cluster recusa ao validar.

## Veja também

[Deployments](ajuda:deployments), [Remoção de Aplicação](ajuda:app-removal).
`;export{a as default};
