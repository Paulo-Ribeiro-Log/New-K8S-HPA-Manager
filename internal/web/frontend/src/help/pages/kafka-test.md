## Para que serve

Testa a conexão com um **broker Kafka** (ou **Azure Event Hub**) e, se você quiser, **lê mensagens**, **conta mensagens** e **publica uma mensagem de teste**. Usa o `kcat` dentro de um container, então consegue mostrar **em qual etapa** a conexão falha: rede, autenticação, TLS ou protocolo.

## Quando usar

- Descobrir por que uma aplicação não consegue falar com o Kafka (rede bloqueada, SASL errado, TLS).
- Confirmar que um **workload específico** alcança o broker (NetworkPolicy, Istio).
- Ver as últimas mensagens de um tópico, ou procurar uma mensagem por texto.
- Saber quantas mensagens um tópico retém e quais tópicos existem.

## Como usar (passo a passo)

1. Escolha **onde o teste roda**:
   - **Via Pod do Cluster:** anexa um *ephemeral container* a um pod real do **Deployment** escolhido. A conexão parte da **identidade de rede desse workload**. Com várias réplicas ou containers, dá para escolher qual **pod** e qual **container** recebem o teste.
   - **Direto do servidor (terminal local):** roda o `kcat` em um `docker run` na máquina do servidor. Serve quando o broker é alcançável pela rede do servidor (VPN, endpoint público). Exige Docker no servidor, com pré-checagem na tela.
2. No modo pod, escolha cluster, namespace e Deployment. Informe o **broker** (`host:porta`) e o **Timeout** (padrão 5 s, no máximo 15 s).
3. Se o broker exige autenticação, ligue **SASL** e escolha o **mecanismo**: `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512` ou `OAUTHBEARER` (Azure AD, por service principal, com Client ID, Client Secret, Token Endpoint e Scope opcional). Escolha a origem da credencial: **digitada**, **connection string do Event Hub** (que força `PLAIN` com TLS e deriva o broker se ficar vazio) ou **lida de um Secret** do Kubernetes, com opção de decodificar mais uma vez em base64.
4. Marque **Usar TLS** se necessário, e **ignorar verificação do certificado** só se precisar.
5. Escolha o que fazer além da conexão (cada um é opcional):
   - **Produzir e consumir mensagem de teste**;
   - **Visualizar mensagens existentes (só leitura)**, com **busca por texto** opcional;
   - **Contar mensagens por offset (só leitura)**.
6. O campo **Tópico** pode ser preenchido por uma **busca de tópicos**: a lista só é carregada quando você pede, porque cada busca abre uma execução real no pod.
7. Execute. O resultado mostra a conectividade em etapas: **TCP/DNS**, **Autenticação** (só se houver SASL) e **Protocolo Kafka**, com número de brokers e de tópicos e, quando o broker informa, **os mecanismos SASL que ele aceita**.

## Produzir e consumir (escreve de verdade)

Publica **uma mensagem real** no tópico, com um marcador único (`k8s-hpa-manager-test-<id>`), e depois lê o tópico à procura dela para medir o **tempo de ida e volta**. É preciso marcar **"Entendo que isso publica uma mensagem real neste tópico"**; sem isso, a etapa não roda. A leitura olha no máximo **50 mensagens**, desde o início do tópico.

## Visualizar mensagens

Mostra as **últimas mensagens** (padrão 10, no máximo 50) em uma janela de duas colunas, lista e detalhe, com partição, offset, horário, chave e conteúdo. Mensagens binárias são marcadas. Com **Buscar texto** (chave ou conteúdo, sem diferenciar maiúsculas), a ferramenta **varre as últimas N mensagens por partição** (padrão 20 mil, no máximo 1 milhão) e devolve só as que casam, até 200. O filtro roda **dentro do container**, então só as que casam voltam. A varredura tem teto de **60 segundos**: ao estourar, devolve o que já achou, como resultado parcial.

## Visão geral de tópicos

Lista os tópicos com **partições** e **mensagens aproximadas**, até **200 tópicos** na consulta de offsets (os demais aparecem sem estatística). No **modo local**, também calcula o **tamanho em disco** por tópico, usando uma imagem completa do Kafka no Docker do servidor; essa parte é melhor esforço e, se falhar, o resto continua.

## Permissões

Executar, buscar tópicos, ver a visão geral e cancelar exigem perfil SRE. No modo pod é preciso poder criar *ephemeral containers* no pod. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Pode escrever no tópico** (só na etapa de produzir, com confirmação). O resto é leitura. Cada execução fica no Change History (`kafka_test`), com broker, tópico, mecanismo SASL, uso de TLS e o resultado de cada etapa. **Credenciais nunca vão para o histórico.** O texto da **busca** é gravado, então evite colocar nele dado sensível.

## Cuidados e erros comuns

- **Mensagens podem ter dados pessoais ou sensíveis.** Visualizar mostra o conteúdo na tela do seu navegador; use com critério em tópicos de produção.
- **Publicar em tópico de produção** cria uma mensagem que outros consumidores também vão ler. Prefira um tópico de teste.
- **Erro de autenticação** costuma vir com a dica do mecanismo certo: confira o campo "mecanismo sugerido".
- **A imagem do `kcat` (`ueisele/kcat:1.7.1-librdkafka2.1.1`)** precisa ser baixada pelo node; no primeiro uso pode demorar. Se o cluster não alcança o registro, o teste falha ao subir o container.
- **`OAUTHBEARER`** depende de uma versão mais nova do `librdkafka`, por isso a imagem não é a oficial do `kcat`.
- Varrer centenas de milhares de mensagens é pesado para o broker: comece com o padrão.

## Veja também

[Pods](ajuda:pods), [Events](ajuda:events).
