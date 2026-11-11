const a=`## Para que serve

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
3. Para repetir uma seleção, digite um nome e use o botão de **salvar conjunto**; o conjunto volta pelo seletor **Conjuntos salvos** (substitui os alvos atuais; ficam no seu navegador).
4. Escolha a **janela de tempo**: um atalho (1 h, 6 h, 24 h, 7 dias) ou **Personalizado**, com data e hora de início e de fim em horário de Brasília (por exemplo, de 01/10/2026 23:00 até 02/10/2026 04:30). A tela mostra a **resolução** que será usada e os avisos.
5. Em **Percentis de latência (pico)** marque quais coletar: **P50**, **P95** e/ou **P99** (o padrão é só o P99; sempre fica ao menos um). **Só os marcados são consultados** e aparecem como colunas; a escolha fica lembrada no navegador. Clique em **Coletar**. O andamento aparece ("2 de 5 concluídas"); **Cancelar** interrompe.
6. O resultado vem em **árvore**: **cluster → namespace → deployment**. Cada nível recolhe com um clique (há **Expandir tudo** e **Recolher tudo**) e o cabeçalho mostra quantas aplicações há e quantas estão com 🔴, ⚠️ ou erro de coleta, para achar o problema sem abrir tudo.
7. Clique numa linha de deployment para ver os **serviços usados**, a **fonte** de cada número e as **notas** de como a aplicação foi encontrada.

## Como ler a tabela

| Coluna | O que é |
|---|---|
| Requisições | total na janela (soma dos pontos de entrada da aplicação no Dynatrace) |
| Max RPS | pico da série **somada** de todos os pontos de entrada, não a soma dos picos |
| P50 / P95 / P99 (pico) | maior valor do percentil na janela (um por coluna, só os escolhidos), considerando apenas serviços com pelo menos 1% do tráfego; o serviço que gerou o pico do P99 aparece ao abrir a linha |
| 4xx / 5xx | erros por classe de status |
| CPU % / Mem % | máximo, mínimo e média; a linha \`req\` é em relação ao **request**, a linha \`lim\` em relação ao **limit** |

Selos: 🔴 P99 acima de 1 s (só quando o P99 foi coletado) ou qualquer 5xx; ⚠️ qualquer 4xx.

**"sem dado" não é zero.** Significa que a fonte não devolveu o valor (aplicação sem Dynatrace, container sem limit definido, janela fora da retenção).

## Exportar o relatório

Depois da coleta, a barra **Exportar** gera o relatório **no seu navegador** (nada é enviado a lugar nenhum):

- **Copiar Markdown** e **Ver código Markdown**: o código fica à vista (selecionável) para colar em um relatório feito à mão. Sai uma **tabela por cluster**, com as mesmas colunas da tela, os selos 🔴/⚠️, a legenda e a seção de observações (erros de coleta). **.md** baixa o arquivo.
- **CSV**: um registro por aplicação, com números crus (ponto decimal, sem separador de milhar; **sem dado = célula vazia, nunca zero**) e os percentis escolhidos. O separador padrão é **\`;\`** (abre certo no Excel em português); escolha **\`,\`** para o formato internacional. O arquivo é UTF-8 e textos que começariam com \`=\`, \`+\`, \`-\` ou \`@\` recebem um apóstrofo, para o Excel nunca os tratar como fórmula.
- **PDF**: A4 em paisagem, uma tabela por cluster, com **fundo vermelho** (P99 acima de 1 s ou 5xx) e **amarelo** (4xx) no lugar dos emojis, legenda e observações.

Todos levam a **janela de tempo e o fuso**, a **resolução**, os **percentis coletados**, as **fontes** (Dynatrace e Prometheus) e o aviso de **pico aproximado**, para o número continuar rastreável fora da ferramenta.

## Janelas antigas e pico aproximado

As fontes guardam o detalhe fino por pouco tempo (no Dynatrace, em geral 1 minuto por cerca de 14 dias, 5 minutos por cerca de 28 dias e 1 hora por cerca de 400 dias). Numa janela antiga, **picos curtos são suavizados**. Quando isso acontece, a tela avisa **"pico aproximado"** e mostra a resolução usada; o sinal ≈ aparece ao lado do RPS. Janela mais antiga que a retenção da fonte não é aceita.

## Permissões

É uma aba de **consulta**. Precisa de credencial do Dynatrace configurada no seu perfil e de um Prometheus acessível pelo servidor. A coleta usa a conta do servidor.

## Impacto

**Somente leitura.** Não altera nada nos clusters, no Dynatrace nem no Prometheus. Os relatórios ficam só na memória do servidor por cerca de 1 hora; a seleção e os conjuntos ficam no seu navegador. Listar namespaces e deployments faz leituras no cluster, como as outras abas.

## Cuidados e erros comuns

- **"nenhum process group … casa":** o Dynatrace não achou a aplicação. O erro lista os Deployments que ele conhece naquele namespace.
- **Linha só com CPU e memória:** o Dynatrace falhou ou não monitora a aplicação; o Prometheus respondeu.
- Um relatório pode ter até **200 aplicações**. Coletas grandes demoram: a primeira consulta de um cluster lista os processos monitorados dele.
- **Cluster offline** (ponto cinza) ou sem acesso: a lista de namespaces mostra o erro. Reconecte (por exemplo, a VPN) e use o botão de recarregar.

## Veja também

[Monitoramento](ajuda:monitoring), [Deployments](ajuda:deployments), [Dynatrace](ajuda:dynatrace), [Cobertura Dynatrace](ajuda:dynatrace-coverage).
`;export{a as default};
