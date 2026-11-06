const a=`Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso). Aqui está só o que é específico dela.

## Para que serve

Lista, edita e exclui **VerticalPodAutoscalers (VPA)** que já existem. **Não cria** VPAs.

## Quando usar

- Ver e ajustar a configuração de um VPA (modo de atualização, limites).
- Remover um VPA que não deve mais mexer nos recursos do workload.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace e busque o VPA.
2. Edite o YAML, use **Validar** e depois **Aplicar** (veja [o fluxo de edição](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar)). A confirmação se chama **Aplicar alterações**.

## O que é específico desta aba

- Não há botão de criar.
- O editor **não tem Desfazer/Refazer** nem **Edição Lado a Lado**; tem **tela cheia**.
- Esta aba **não consulta o RBAC do cluster** antes de habilitar os botões: se faltar permissão, o erro vem do cluster ao aplicar.

## Impacto

**Altera o cluster.** Aplicar (sem dry-run) e excluir ficam no Change History.

## Cuidados e erros comuns

- Um VPA em modo que **atualiza automaticamente** pode **recriar pods** para aplicar novos \`requests\`.
- Não combine um VPA em modo automático com um HPA que escala pela mesma métrica (CPU ou memória): eles brigam entre si.
- Excluir o VPA não devolve os \`requests\` antigos do workload; só para de ajustá-los.

## Veja também

[Deployments](ajuda:deployments), [FinOps](ajuda:finops).
`;export{a as default};
