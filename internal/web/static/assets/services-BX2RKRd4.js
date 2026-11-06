const e=`Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso). Aqui está só o que é específico dela.

## Para que serve

Lista, edita, cria e exclui **Services**.

## Quando usar

- Ajustar portas ou \`selector\` de um Service.
- Criar um Service para expor um workload dentro do cluster.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace e busque o Service.
2. Edite o YAML, use **Validar** e depois **Aplicar** (veja [o fluxo de edição](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar)). A confirmação se chama **Aplicar alterações**.
3. Para um Service novo, use a opção de criar.

## O que é específico desta aba

- O editor desta aba **não tem Desfazer/Refazer** nem **Edição Lado a Lado**; tem **tela cheia**.
- É possível **limpar a seleção** da tabela.

## Permissões

Os botões que alteram só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster.** Aplicar, criar e excluir ficam no Change History.

## Cuidados e erros comuns

- Um \`selector\` que não casa com nenhum pod deixa o Service **sem endpoints**: o tráfego falha mesmo com o Service "existindo".
- O \`clusterIP\` de um Service existente é **imutável**: o cluster recusa a mudança ao validar ou aplicar.
- Excluir um Service **derruba o acesso** de quem o usa por nome DNS.

## Veja também

[Ingresses](ajuda:ingresses), [Deployments](ajuda:deployments).
`;export{e as default};
