const e=`## Para que serve

Mede a **latência real** de HTTP, HTTPS ou ICMP (ping) **a partir de dentro de um namespace do cluster**, contra um Service, uma URL interna ou uma região de nuvem. Mostra mínimo, média, mediana, P95, P99 e máximo.

## Quando usar

- Confirmar se uma aplicação está lenta para responder **dentro do cluster** (e não só para quem acessa de fora).
- Comparar a latência de um cluster até uma região de nuvem (por exemplo, até a AWS em São Paulo).
- Ver se um Service responde dentro do esperado depois de uma mudança.
- Mapear de quais clusters já se testou cada alvo (aba **Topologia**).

## Como usar (passo a passo)

1. Na aba **Teste**, escolha o **cluster** e o **namespace**. O teste parte de um pod criado **nesse namespace**, então respeita as NetworkPolicies de lá.
2. Preencha o alvo de um destes jeitos:
   - **Service (atalho):** preenche a URL com \`<service>.<namespace>.svc.cluster.local\` e a primeira porta do Service;
   - **Alvo rápido: região de nuvem:** preenche com o endpoint S3 regional da AWS (hoje só existem alvos da AWS na lista);
   - ou digite a **URL** ou o **host** à mão.
3. Escolha o **protocolo**: **HTTP**, **HTTPS** ou **ICMP (ping)**. Para ICMP o alvo é só o host, sem esquema nem caminho.
4. Defina **Requisições** (1 a 200, padrão 20) e **Timeout** em ms (100 a 10000, padrão 3000).
5. **Executar Teste.** A barra de progresso acompanha as etapas; **Cancelar** interrompe e limpa o pod.
6. O resultado mostra os seis indicadores, quantas requisições deram certo (e os erros ou timeouts), um **gráfico por requisição** e uma tabela dos testes anteriores **desta sessão**.

## Contexto histórico

Quando existe dado, o resultado também mostra o **P95 e o P99 dos últimos dias** vindos do **Dynatrace** (selo \`DT\`) ou, na falta dele, do **Prometheus** (selo \`Prom\`). É um complemento que **nunca atrasa nem bloqueia** o teste: se não houver dado, simplesmente não aparece.

## Aba Topologia

Um grafo com os clusters já testados e os alvos (Services e regiões de nuvem). Cada **aresta** é o **resultado mais recente** de um par cluster → alvo, colorida pelo P95: **verde** abaixo de 100 ms, **amarelo** a partir de 100 ms e **vermelho** a partir de 300 ms. Clique na aresta para ver os detalhes. Só aparecem testes que chegaram a medir alguma amostra; pares nunca testados não têm aresta.

## Permissões

Executar o teste exige perfil SRE e permissão para criar pods no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Cria um pod temporário e gera tráfego real contra o alvo.** O pod usa a imagem \`nicolaka/netshoot:v0.12\`, vive no máximo 5 minutos (o Kubernetes o encerra sozinho se o servidor cair) e é apagado ao final; uma varredura periódica remove pods de teste órfãos. Cada execução fica registrada no Change History (\`latency_test\`).

## Cuidados e erros comuns

- **Um teste por vez por usuário.** Se disparar outro, ele é recusado até o primeiro terminar.
- **Tráfego de teste é tráfego real.** 200 requisições seguidas contra um serviço frágil ou de terceiros podem causar impacto. Comece com poucas.
- **Os números de HTTP incluem tudo** (DNS, conexão, TLS e resposta). Um HTTPS lento pode ser o TLS, não a aplicação.
- **ICMP pode falhar por permissão:** o pod não pede a capability de rede, para funcionar em clusters com Pod Security restritivo. Dependendo da configuração do node, o \`ping\` não consegue abrir o socket, e o erro aparece na tela.
- **O pod depende de baixar a imagem.** Se o cluster não alcança o registro de imagens, o teste falha ao subir o pod (o tempo de espera é de 60 segundos).
- **Admission e políticas** (Kyverno, Gatekeeper) podem bloquear o pod de teste em alguns namespaces.
- O histórico da tabela é **da sessão do navegador**; o grafo da Topologia usa o que ficou **persistido no servidor**.

## Veja também

[Pods](ajuda:pods), [Events](ajuda:events).
`;export{e as default};
