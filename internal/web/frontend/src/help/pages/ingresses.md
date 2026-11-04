Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso). Aqui está só o que é específico dela.

## Para que serve

Lista, edita, cria e exclui **Ingresses**.

## Quando usar

- Publicar ou ajustar uma rota HTTP(S) para um Service.
- Achar qual Ingress atende um **host**.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace.
2. Busque por **nome, host ou label** e abra o Ingress.
3. Edite o YAML, confira o **Diff**, use **Validar** e depois **Aplicar** (veja [o fluxo de edição](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar)).
4. Para um Ingress novo, escolha um namespace específico e use **Criar novo Ingress**.

## O que é específico desta aba

- A busca também casa pelo **host**, além do nome e das labels.
- **Ctrl+S** guarda um *checkpoint* no histórico de desfazer do editor (no seu navegador) e **não aplica nada** no cluster.
- Para recursos da **Gateway API** (Gateways e rotas), use a aba Gateways.

## Permissões

Os botões que alteram só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster, e a mudança vale na hora para o tráfego.** Aplicar, criar e excluir ficam no Change History.

## Cuidados e erros comuns

- Apagar ou alterar um Ingress de produção **tira a aplicação do ar** para aquele host.
- Dois Ingresses com o mesmo host e caminho podem conflitar, dependendo do controller.
- O que o controller faz com o manifesto (annotations, TLS, balanceador) depende do controller instalado no cluster; a validação (dry-run) não prova que a rota vai funcionar, só que o manifesto é aceito.

## Veja também

[Services](ajuda:services), [Remoção de Aplicação](ajuda:app-removal).
