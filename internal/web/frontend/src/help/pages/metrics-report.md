## Para que serve

Gera um **relatório por aplicação** numa janela de tempo que você escolhe: requisições, pico de RPS, percentis de latência (P50, P95 e P99), erros 4xx e 5xx (vindos do **Dynatrace**) e uso de CPU e memória (vindos do **Prometheus** do cluster). Serve para olhar um evento (um stress test, um pico, um incidente) com a mesma régua em várias aplicações.

## Quando usar

- Comparar o comportamento de várias aplicações no mesmo período.
- Documentar um evento com números: a janela e a resolução ficam registradas no relatório.
- Ver quem está apertado: CPU e memória em **% do request** e em **% do limit**.

## Como usar (passo a passo)

1. À esquerda, em **Alvos da coleta**, cada **alvo** é um cartão com um cluster e o que coletar nele:
   - **Cluster:** o seletor com busca tem o filtro **Todos / HLG / PRD** (e a jornada, quando há mais de uma), igual ao das outras abas. Clusters de produção aparecem em destaque.
   - **Namespaces:** seleção múltipla com busca, **Marcar todos** e **Limpar**; a opção **sistema** mostra os namespaces de sistema.
   - **Deployments:** seleção múltipla dos deployments dos namespaces escolhidos, agrupados por namespace e com as réplicas prontas. Tirar um namespace tira os deployments dele da seleção.
   Em cada seletor as marcações ficam em **rascunho**: só valem ao clicar em **Aplicar** (**Cancelar**, ou fechar o seletor, descarta). Ao trocar o cluster, o aviso lembra que os namespaces e deployments do alvo serão limpos.
2. O botão **+** (no topo, ou **Adicionar alvo** embaixo) cria outro alvo, por exemplo para incluir um segundo cluster. O **×** remove o alvo e a seta recolhe o cartão; o resumo de uma linha ("5 deployments em 2 namespaces") continua visível. Esta aba **não depende** da lista de HPAs monitorados nem de nenhuma outra aba. Os alvos ficam lembrados no navegador.
3. Para repetir uma seleção, digite um nome e use o botão de **salvar conjunto**; o conjunto volta pelo seletor **Conjuntos salvos** (substitui os alvos atuais; ficam salvos no servidor, por usuário, e sobrevivem a reinício; o navegador guarda só uma cópia).
4. Escolha a **janela de tempo**: um atalho (1 h, 3 h, 6 h, 12 h, 24 h, 7 dias, 14 dias) ou **Personalizado**, com data e hora de início e de fim em horário de Brasília (por exemplo, de 01/10/2026 23:00 até 02/10/2026 04:30). A tela mostra a **resolução** que será usada e os avisos.
5. Em **Percentis de latência (pico)** marque quais coletar: **P50**, **P95** e/ou **P99** (o padrão é só o P99; sempre fica ao menos um). **Só os marcados são consultados** e aparecem como colunas; a escolha fica lembrada no navegador. Marque também **Contexto de node** se quiser saber em que node pool a aplicação rodou e como estavam os nodes (veja a seção abaixo; é opcional porque faz mais consultas ao Prometheus). Marque **Réplicas, HPA e rede** para acrescentar as métricas do Prometheus do Tempo real ao relatório (veja abaixo). Clique em **Coletar**. O andamento aparece ("2 de 5 concluídas"); **Cancelar** interrompe.
6. O resultado vem em **árvore**: **cluster → namespace → deployment**. Cada nível recolhe com um clique (há **Expandir tudo** e **Recolher tudo**) e o cabeçalho mostra quantas aplicações há e quantas estão com 🔴, ⚠️ ou erro de coleta, para achar o problema sem abrir tudo.
7. Clique numa linha de deployment para ver os **serviços usados**, a **fonte** de cada número e as **notas** de como a aplicação foi encontrada.

## Como ler a tabela

| Coluna | O que é |
|---|---|
| Requisições | total na janela (soma dos pontos de entrada da aplicação no Dynatrace) |
| Max RPS | pico da série **somada** de todos os pontos de entrada, não a soma dos picos |
| P50 / P95 / P99 (pico) | maior valor do percentil na janela (um por coluna, só os escolhidos), considerando apenas serviços com pelo menos 1% do tráfego; o serviço que gerou o pico do P99 aparece ao abrir a linha |
| 4xx / 5xx | erros por classe de status |
| CPU % / Mem % | máximo, mínimo e média; a linha `req` é em relação ao **request**, a linha `lim` em relação ao **limit** (dados do Prometheus: ~14 dias) |

Selos: 🔴 P99 acima de 1 s (só quando o P99 foi coletado) ou qualquer 5xx; ⚠️ qualquer 4xx.

**"sem dado" não é zero.** Significa que a fonte não devolveu o valor (aplicação sem Dynatrace, container sem limit definido, janela fora da retenção).

## Exportar o relatório

Depois da coleta, a barra **Exportar** gera o relatório **no seu navegador** (nada é enviado a lugar nenhum):

- **Copiar Markdown** e **Ver código Markdown**: o código abre num **modal** (selecionável, com **Copiar** e **Baixar .md**) para colar em um relatório feito à mão. Sai uma **tabela por cluster**, com as mesmas colunas da tela, os selos 🔴/⚠️, a legenda e a seção de observações (erros de coleta). **.md** baixa o arquivo.
- **CSV**: um registro por aplicação, com números crus (ponto decimal, sem separador de milhar; **sem dado = célula vazia, nunca zero**) e os percentis escolhidos. O separador padrão é **`;`** (abre certo no Excel em português); escolha **`,`** para o formato internacional. O arquivo é UTF-8 e textos que começariam com `=`, `+`, `-` ou `@` recebem um apóstrofo, para o Excel nunca os tratar como fórmula.
- **PDF**: A4 em paisagem, uma tabela por cluster, com **fundo vermelho** (P99 acima de 1 s ou 5xx) e **amarelo** (4xx) no lugar dos emojis, legenda e observações.

Todos levam a **janela de tempo e o fuso**, a **resolução**, os **percentis coletados**, as **fontes** (Dynatrace e Prometheus) e o aviso de **pico aproximado**, para o número continuar rastreável fora da ferramenta.

## Comparar com um evento

Com um relatório pronto e ao menos um evento salvo, a seção **Comparar com um evento** coleta os **mesmos alvos e percentis** na janela do evento e mostra, por aplicação, o valor **atual**, o do **evento** e a variação: RPS médio, pico de RPS, pico de latência, % de erros 5xx e 4xx sobre as requisições e pico de CPU e memória. Só **taxas e picos** são comparados, nunca os totais: uma janela de 2 h tem o dobro das requisições de uma de 1 h sem que nada tenha piorado. Variação até 5% conta como igual; latência, erros, CPU e memória subindo aparecem em vermelho. Aplicações que só existem num dos lados ficam marcadas. Se o evento for antigo, o pico é aproximado (resolução menor).

## Resposta média no relatório

A coluna **Resposta média** é o tempo de resposta médio da **janela inteira** (ponderado pelas requisições de cada instante e de cada serviço de entrada), em segundos. Ao contrário das colunas de P50, P95 e P99, que são **picos**, ela é uma média: por isso um P99 de pico alto pode conviver com uma média baixa. Entra nas exportações (Markdown, CSV e PDF) e na comparação com um evento.

## Réplicas, HPA e rede (opcional)

A caixa **Réplicas, HPA e rede** acrescenta seis colunas, só do Prometheus e só para a janela do relatório: **réplicas disponíveis** (máx/mín/méd), **HPA** (limites mínimo e máximo do HPA do Deployment; vazio se não houver HPA cujo alvo ou nome case), **restarts na janela** (soma), **rede recebida** e **enviada** (bytes/s somando os pods, pico / média) e **descartes + erros de rede** (pico por segundo). É contexto, não veredito: não gera selo. O **throttling de CPU** continua no **Contexto de node**. A escolha fica lembrada no navegador, as colunas entram nas exportações (Markdown, CSV e PDF) e o dado só existe para os últimos ~14 dias (retenção do Prometheus).

## Contexto de node (opcional)

A caixa **Contexto de node** acrescenta quatro colunas, para ajudar a interpretar a latência (um P99 alto pode ser da aplicação, do node ou de throttling):

| Coluna | O que é |
|---|---|
| Node pool | pool(s) onde os pods rodaram na janela, com o número de nodes e o tipo de instância (ex.: `calculofret2 (27 nodes, Standard_D4s_v5)`) |
| Node CPU % (pior/média) | uso de CPU dos nodes que hospedaram a aplicação: o **pior** pico entre eles e a **média** das médias de cada node |
| Node Mem % (pior/média) | o mesmo para memória |
| Throttling CPU % (máx/méd) | fração dos períodos em que a CPU da **própria aplicação** foi limitada pelo limit |

É **contexto, não veredito**: não gera selo. Cobertura parcial aparece escrita (por exemplo "9 de 27 nodes") e as notas ficam ao abrir a linha. O pool vem do rótulo `label_agentpool` do node (ou o equivalente de AKS, EKS e GKE); sem ele aparece "sem rótulo de pool".

**Retenção:** CPU, memória e o contexto de node vêm do **Prometheus, que guarda cerca de 14 dias**. Numa janela mais antiga essas colunas saem "sem dado" (o tráfego, que vem do Dynatrace, continua). A tela avisa **antes de coletar**, logo abaixo da janela.

## Tempo real

A sub-aba **Tempo real** (ao lado de **Relatório**, no topo) acompanha as aplicações marcadas no painel de alvos, com as métricas do Dynatrace, num **cartão por aplicação**, agrupado por cluster: **RPS** atual, **barras do RPS** no período (com o máximo), **erros 5xx e 4xx** (contagem no período, como 19.197, mais a taxa por segundo de agora), **latências** (P50, P95 e P99, os mesmos percentis marcados no relatório) e a **tendência**. Cada linha tem a situação à direita: OK, ALERTA ou ERRO (P99 acima de 1 s, qualquer 5xx é ERRO e qualquer 4xx é ALERTA, a mesma regra do relatório). O ícone de gráfico no cartão abre os **gráficos completos** (RPS, latência e erros, com a projeção).

- **Itens do Prometheus:** acima do RPS o cartão mostra **réplicas** (disponíveis / desejadas; menos disponíveis que desejadas é ERRO), **restarts** dos últimos 15 min (qualquer um é ALERTA), **CPU** e **memória** (valor total dos pods e a % do limit e do request; acima de 70% do limit é ALERTA e acima de 85% é ERRO; sem limit definido não há veredito). A caixa **Réplicas, restarts, CPU e memória** liga e desliga esse bloco (9 consultas por cluster a cada atualização). **P50, P95 e P99, tráfego e erros vêm sempre do Dynatrace**, nunca do Prometheus. A % é a soma do uso dividida pela soma do limit (ou do request) dos pods do Deployment; se só alguns containers têm limit, a % do limit sai maior que a real. Se o Prometheus não responder, o cartão mantém os dados do Dynatrace e avisa na nota; se o Dynatrace falhar, o cartão mantém os itens do Prometheus.
- **HPA, throttling e rede (Prometheus):** o cartão também mostra o **HPA** (mínimo–máximo de réplicas; ALERTA quando as desejadas chegam ao **teto**, porque a aplicação não escala mais), o **throttling de CPU** (% dos períodos do CFS em que algum container foi limitado; acima de 10% é ALERTA e de 25% é ERRO, e pode haver P99 alto mesmo com CPU abaixo do limit) e a **rede** (entrada ↓ e saída ↑ em bytes/s, somando os pods; descartes e erros de pacote acima de 0,5/s são ALERTA). As linhas de HPA, throttling e rede só aparecem quando há dado: sem HPA (ou HPA com nome e alvo diferentes do Deployment), a linha some.
- **Gráfico de rede e link para o Dynatrace:** o ícone de gráfico do cartão abre o modal; além de RPS, latência e erros, ele traz o **gráfico de rede** (↓ recebido e ↑ enviado em bytes/s, somando os pods, e os descartes e erros de pacote quando houver), buscado no Prometheus só ao abrir o modal. No cartão e no modal, **Abrir no Dynatrace** leva a cada serviço de entrada da aplicação (abre em outra aba).
- **Resposta média:** a linha **Resp. média** mostra o tempo de resposta **médio** do Dynatrace (`response.time:avg`), ao lado dos percentis. Entre serviços de entrada e entre pontos de um grupo a média é **ponderada pelas requisições** (um minuto de pico pesa mais que um de madrugada), não uma média simples. A média esconde caudas longas: use-a junto com o P95/P99, nunca no lugar deles. Ela também aparece como linha no gráfico de latência do modal e no resumo da sessão.
- **Motivo dos restarts e pods:** ao lado dos restarts dos últimos 15 min aparece o **motivo** (a última razão de término dos containers que reiniciaram, ex.: `OOMKilled ×2, Error ×1`; OOMKilled fica em vermelho). A linha **Pods** só aparece quando há pod **pendente** ou **não pronto** (scale-out ou rollout em andamento, falta de capacidade).
- **Resumo da sessão:** a linha "sessão: N atualizações desde HH:MM" guarda, por aplicação, só **agregados** (mín/méd/máx de RPS, erros, latência, réplicas, CPU, memória, throttling e rede, vezes no teto do HPA, restarts e pods pendentes), então a memória não cresce com o tempo. **Resumo .md** e **CSV** baixam o resumo; **Zerar** começa outra sessão. Os valores são **amostrados a cada atualização**, não o pico entre duas atualizações.
- **Memória da aba:** o indicador "memória JS da aba" mostra o heap JavaScript (só Chromium), lido a cada 10 s. Oscila em serra; se o **pico** só sobe com o tempo, há vazamento.
- **Eventos:** o botão **Eventos** guarda janelas de tempo com nome (ex.: "Stress test 11/11 14h-16h"); clicar num evento aplica a janela em **Personalizado**. Valem para o Relatório e para o Tempo real, ficam no servidor (por usuário) e sobrevivem a reinício.
- **Parar e Iniciar:** o botão **Parar** interrompe as consultas ao Dynatrace; os cartões ficam como estavam. Ao parar, mudar período ou seleção só vale ao **Iniciar** ou em **Atualizar agora**.
- **Período:** "últimos X" como no Grafana (5 min, 15 min, 30 min, 1 h, 3 h, 6 h, 12 h, 24 h, 2 dias, 7 dias) ou **Personalizado** (início e fim). O período relativo termina sempre agora.
- **Atualização:** a cada **1 a 5 minutos** (ou pausada), com a contagem até a próxima. O Dynatrace entrega dado de 1 em 1 minuto, por isso não há intervalo menor. A atualização para sozinha quando o navegador está em segundo plano. O período personalizado é passado e não atualiza.
- **Barras:** a resposta vem reduzida a no máximo 120 pontos por série; em períodos longos, cada barra agrupa vários intervalos (a tela diz "cada barra = N min"): **média** para RPS e erros, **maior valor** para a latência.
- **Último intervalo:** só aparece quando já terminou. O Dynatrace entrega o minuto corrente incompleto, e ele pareceria uma queda falsa de tráfego.
- **Tendência (projeção sazonal):** usa a **curva de ontem no mesmo horário**, escalada pelo nível de hoje (fator hoje/ontem dos últimos 30 min), estendida por +15 min, +30 min ou +1 h (ou desligada). O cartão diz "base: ontem"; sem dado de ontem utilizável cai numa **reta ajustada** ao período ("reta"). É extrapolação, **não previsão**: se ontem foi atípico, a projeção herda isso. A base de ontem é buscada a cada 5 min (uma consulta a mais por métrica e cluster).
- **Selos de status:** no topo, ERRO/ALERTA/OK/INFO com a contagem de aplicações; o tooltip lista quais e o motivo (P99, 5xx, 4xx, falha de coleta).
- **Minimizar e remover:** a seta no nome do cluster esconde os cartões dele (mostra o pior nível); o **X** do cartão remove a aplicação do acompanhamento **e a desmarca da seleção** do painel de alvos.
- **Custo:** uma consulta por métrica **por cluster** (todas as aplicações do cluster juntas), a cada atualização. Falta de dado aparece como lacuna, nunca como zero.

## Janelas antigas e pico aproximado

As fontes guardam o detalhe fino por pouco tempo (no Dynatrace, em geral 1 minuto por cerca de 14 dias, 5 minutos por cerca de 28 dias e 1 hora por cerca de 400 dias). Numa janela antiga, **picos curtos são suavizados**. Quando isso acontece, a tela avisa **"pico aproximado"** e mostra a resolução usada; o sinal ≈ aparece ao lado do RPS. Janela mais antiga que a retenção da fonte não é aceita.

## Permissões

É uma aba de **consulta**. Precisa de credencial do Dynatrace configurada no seu perfil e de um Prometheus acessível pelo servidor. A coleta usa a conta do servidor.

## Impacto

**Somente leitura.** Não altera nada nos clusters, no Dynatrace nem no Prometheus. Os relatórios ficam só na memória do servidor por cerca de 1 hora; a seleção fica no seu navegador e os conjuntos salvos no servidor (por usuário). Listar namespaces e deployments faz leituras no cluster, como as outras abas.

## Cuidados e erros comuns

- **"nenhum process group … casa":** o Dynatrace não achou a aplicação. O erro lista os Deployments que ele conhece naquele namespace.
- **Linha só com CPU e memória:** o Dynatrace falhou ou não monitora a aplicação; o Prometheus respondeu.
- Um relatório pode ter até **200 aplicações**. Coletas grandes demoram: a primeira consulta de um cluster lista os processos monitorados dele.
- **Cluster offline** (ponto cinza) ou sem acesso: a lista de namespaces mostra o erro. Reconecte (por exemplo, a VPN) e use o botão de recarregar.

## Veja também

[Monitoramento](ajuda:monitoring), [Deployments](ajuda:deployments), [Dynatrace](ajuda:dynatrace), [Cobertura Dynatrace](ajuda:dynatrace-coverage).
