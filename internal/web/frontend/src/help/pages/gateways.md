Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso) (lista, editor YAML, Diff, Validar e Aplicar). Aqui está o que é específico dela.

## Para que serve

Gerencia os recursos da **Gateway API** do Kubernetes: **Gateway**, **HTTPRoute**, **GRPCRoute**, **TCPRoute** e **GatewayClass**.

## Quando usar

- Ver quais Gateways e rotas existem e se foram programados.
- Ajustar uma rota (host, caminho, backend) pelo YAML.
- Criar uma rota nova a partir de um modelo.

## Como usar (passo a passo)

1. Escolha o cluster e, no seletor de tipo, o **tipo de recurso**. O namespace vale para todos, menos **GatewayClass**, que é do cluster inteiro (o seletor de namespace fica desabilitado).
2. A lista mostra namespace e nome. Em **Gateway** também mostra **Classe**, **Endereço** e **Programado** (se o controlador já configurou). A busca filtra por nome e namespace.
3. **Clique em um item** para abrir o YAML, ver endereços e editar. Vale o fluxo [Diff, Validar, Aplicar](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar).
4. **Criar** abre um modelo do tipo escolhido, com Validar (dry-run) antes de criar.
5. **Describe** mostra o equivalente a `kubectl describe`.

## Permissões

As ações que alteram só ficam disponíveis com permissão de escrita. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster, e a mudança vale na hora para o tráfego.** Criar (fora do dry-run), aplicar e excluir ficam no Change History.

## Cuidados e erros comuns

- **Lista vazia não prova que não há Gateway API.** Se o `kubectl` falha por qualquer motivo (CRDs não instalados, **sem permissão, rede**), a aba mostra a lista vazia em vez de um erro. Confirme com o time se o cluster tem a Gateway API instalada.
- Mexer num **GatewayClass** ou num **Gateway** afeta todas as rotas ligadas a ele.
- Excluir um Gateway derruba o tráfego das rotas que o usam; confirme as rotas antes.
- O Kubernetes valida o YAML contra o CRD: um erro de campo aparece na validação (dry-run).

## Veja também

[Ingresses](ajuda:ingresses), [Services](ajuda:services).
