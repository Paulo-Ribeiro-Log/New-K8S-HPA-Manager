## Para que serve

Lista os **problems do Dynatrace** (os incidentes que o Davis AI detecta) e ajuda a **investigar cada um**: entidades afetadas, **causa raiz**, métricas, o caminho de propagação entre serviços e o **dono** do serviço (squad, tribo, repositórios). Tem **diagnóstico por IA**, em modo rápido e em modo profundo.

## Quando usar

- Ver **o que está aberto agora** no Dynatrace, filtrado pela sua equipe.
- Entender **por que** um problem aconteceu e **quem é o dono** do serviço afetado.
- Cruzar o problem com o que o Kubernetes mostra (pods, HPAs, nodes) em um só lugar.
- Gerar um relatório do problem (PDF ou Markdown).

## Antes de usar

Configure a **URL** e o **token** do Dynatrace no seu **Perfil → Dynatrace**. Sem isso, a aba mostra **"Dynatrace não configurado"**. A credencial é a **sua**: o aplicativo a resolve pelo e-mail corporativo do seu Perfil SSO, com a identidade de login como alternativa. Existe um **tenant de homologação** separado: clusters não produtivos usam o tenant de homologação, com a mesma tela.

## Como usar (passo a passo)

1. A lista da esquerda mostra os **problems**. Filtre por **status** (**abertos** por padrão, **fechados** ou **todos**), por **management zone**, por **tag ou texto livre** (`tag:valor`) e por **período** (datas inicial e final, para o histórico). Os contadores mostram quantos são **críticos**, **altos** e o **total**. **Auto-atualizar** recarrega a lista (a cada 60 segundos por padrão) enquanto você olha os abertos.
2. **Clique em um problem** para ver o detalhe à direita, em quatro abas:

| Aba | O que mostra |
|---|---|
| **Detalhes** | o título, a duração, a **causa raiz** (quando o Davis aponta), as **entidades afetadas** com os dados de Kubernetes (cluster, namespace, workload, node pool), o **Fluxo de propagação** (um grafo com zoom e arrastar) e o guia **O que investigar**; a **Rastreabilidade** traz squad, tribo, jornada e repositórios |
| **Métricas** | gráficos de desempenho por entidade e por família de métrica e, no mesmo painel, o **contexto do Davis AI**: evidências com mini gráficos, topologia, traces e eventos |
| **GitHub** | correlaciona o problem com os **GitHub Releases**: a versão em produção, o repositório do serviço e as **releases publicadas depois dessa versão**, como **candidatas à causa**, com atalho para comparar (veja [GitHub Releases](ajuda:github-releases)) |
| **Diagnóstico** | a análise por IA (veja abaixo) |

3. **Escanear Clusters** cataloga os **node pools** dos clusters, para a ferramenta conseguir ligar um nó do Dynatrace (nome `aks-<pool>-…-vmss…`) ao cluster certo. Faça uma vez, ou quando surgirem pools novos.
4. **Exportar como PDF** ou **como Markdown** gera o relatório do problem.

## Diagnóstico por IA

- **Análise Rápida:** usa só as **métricas do Dynatrace** e a IA, sem consultar o cluster. É mais rápida.
- **Investigar Profundo:** identifica o **cluster do Kubernetes** do problem, roda um **Health Check direcionado** e cruza com as métricas, as evidências do Davis e as correlações, e devolve um **Plano de Ação**. O caminho é: registro de node pools, Health Check do K8s, métricas do Dynatrace e análise por IA.

As duas usam o provedor de IA configurado em [AI Diagnostics](ajuda:ai-diagnostics).

## Permissões

É uma aba de **consulta**. Nada exige o grupo SRE. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Somente leitura no Dynatrace.** O **Investigar Profundo** roda um Health Check no cluster, e a verificação de Services dele pode criar um pod temporário (veja [Health Checking](ajuda:healthcheck)). Os diagnósticos por IA ficam no histórico de análises.

## Cuidados e erros comuns

- **Dados enviados para a IA.** Com um provedor externo, as métricas, os nomes de entidades e o contexto do problem **saem do servidor** (com mascaramento parcial, veja [AI Diagnostics](ajuda:ai-diagnostics)). Prefira o Ollama se isso for um problema.
- **O token do Dynatrace fica guardado sem criptografia** no banco local do servidor (o mesmo `ai_diagnostics.db`), e o arquivo é criado com a permissão padrão do sistema. Use um token de **escopo mínimo** (só leitura de problems, entidades e métricas).
- **Uma URL errada** causa erros de conexão. A ferramenta rejeita um endereço sem esquema (por exemplo, um nome solto no lugar do endereço): confira em **Perfil → Dynatrace**.
- **"Não identificado, execute Escanear Clusters":** o node pool do problem não está no catálogo. Escaneie os clusters.
- **Problems fechados só aparecem se você escolher o status** e, para históricos antigos, um **período**: a busca padrão mostra os abertos.
- **A causa raiz do Davis nem sempre existe** ("No root cause"): isso não quer dizer que o problem seja menos grave.
- **Uma análise por IA é uma sugestão**: confirme no Kubernetes antes de agir.

## Veja também

[Cobertura Dynatrace](ajuda:dynatrace-coverage), [Health Checking](ajuda:healthcheck), [AI Diagnostics](ajuda:ai-diagnostics).
