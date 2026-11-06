const e=`## Para que serve

Mostra a **topologia de tráfego entre serviços** do Istio no cluster, em um grafo ao vivo: quem chama quem, com **taxa de requisições, erros e latência**. Os dados vêm do **Kiali**.

## Quando usar

- Entender **de onde vem e para onde vai** o tráfego de uma aplicação.
- Achar **uma aresta com erro** (por exemplo, um serviço devolvendo 5xx para outro).
- Ver **quais serviços não recebem tráfego** (nós e arestas ociosos).
- Investigar um aumento de latência olhando o serviço no meio do caminho.

## Como usar (passo a passo)

1. Escolha o **cluster** no topo do aplicativo. A aba usa o cluster global.
2. Em **Selecionar Namespaces**, escolha os namespaces que entram no grafo. A lista de namespaces vem da API do Kubernetes, não do Kiali.
3. Escolha o **Tipo de Grafo** (**Workload**, **App**, **Service** ou **Versioned App**) e a **Duração** da janela de tráfego (por exemplo, 1, 5, 10 ou 30 minutos).
4. O grafo aparece. **Clique em um nó ou aresta** para o painel **Detalhes do Serviço**, com as métricas.
5. Em **Display Options**, ligue ou desligue o que quiser ver: **Cluster Boxes**, **Namespace Boxes**, **Service Nodes**, **Operation Nodes**, **Idle Edges**, **Idle Nodes**, **Show Badges**, **Show Edge Labels** e **Traffic Animation**. Em **Traffic Options**, escolha o que a aresta representa (taxa de requisições, vazão, bytes ou mensagens).
6. **Auto-Refresh** atualiza o grafo sozinho (de 15 segundos a 10 minutos). **Tela cheia** amplia o grafo, e a **Legenda** explica as cores e símbolos.

## Como a ferramenta chega ao Kiali

A aba consulta o Kiali pela **URL externa do cluster**, montada pelo padrão \`https://kiali-<cluster>.grupocasasbahia.com.br/kiali/\`. Dependendo de como o Kiali está configurado:

- **Autenticação por token:** a ferramenta obtém um token **temporário (1 hora)** da conta de serviço **\`kiali\` do namespace \`istio-system\`**, pelo próprio Kubernetes, e faz login no Kiali com ele. O token fica em cache por cerca de 55 minutos.
- **Acesso anônimo:** consulta direto, sem token.

## Permissões

É uma aba de **consulta** e as rotas não exigem o grupo SRE. Para a autenticação por token, o servidor precisa poder **ler a conta de serviço \`kiali\`** e **pedir um token** para ela em \`istio-system\`.

## Impacto

**Não altera nada no cluster**, com uma ressalva: pedir o token da conta \`kiali\` é uma chamada de escrita ao Kubernetes (\`serviceaccounts/token\`) e **não é registrada no Change History**. O token não é gravado em disco.

## Cuidados e erros comuns

- **"Kiali não acessível":** o endereço do Kiali daquele cluster não responde para o servidor (rede, VPN ou o Kiali não está exposto nesse padrão de nome). Sem Kiali, não há grafo.
- **O grafo usa as permissões do Kiali**, não as suas. Você vê o tráfego que a conta \`kiali\` enxerga, mesmo de namespaces em que o seu RBAC não teria acesso.
- **A ferramenta aceita o certificado do Kiali sem validar a cadeia** (certificado autoassinado), então não confie nela para verificar a identidade do endpoint.
- **Grafo vazio** costuma ser janela curta demais, namespace sem tráfego na janela, ou sem sidecar do Istio. Aumente a **Duração** e confira se o namespace tem injeção.
- **Grafos grandes ficam pesados**: selecione poucos namespaces e desligue **Idle Nodes** e **Idle Edges**.
- Os números são **aproximações da janela escolhida**, não valores exatos de cada requisição.

## Veja também

[Monitoramento](ajuda:monitoring), [Pods](ajuda:pods), [Services](ajuda:services).
`;export{e as default};
