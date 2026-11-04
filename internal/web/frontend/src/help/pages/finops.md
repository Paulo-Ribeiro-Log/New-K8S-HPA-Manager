## Para que serve

Mostra para onde vai o dinheiro de um cluster Kubernetes (AKS, EKS ou GKE): custo por node pool, namespace e workload, **desperdício** (o que é pedido e não é usado), capacidade ociosa e **oportunidades de economia**.

## Quando usar

- Antes de reduzir node pools ou trocar o tamanho das VMs.
- Para achar workloads com `requests` muito acima do uso real.
- Para cobrar ou apresentar custo por namespace ou dono (**Showback**).
- Para comparar clusters entre si.

## Como usar (passo a passo)

1. Escolha o **cluster** no cabeçalho.
2. Se quiser uso real, deixe marcado **Análise histórica**. A fonte principal é o **Dynatrace** (quando configurado) e o **Prometheus** é o plano B. Escolha a janela: **7, 14 ou 30 dias**. Um selo mostra se o Prometheus está disponível.
3. Clique em **Analisar**. A coleta pode levar alguns minutos.
4. Navegue pelas abas. Antes de analisar, só ficam disponíveis **Armazenamento e Órfãos**, **Showback** e **Todos os clusters** (leem dados já gravados).

## As abas

| Aba | O que mostra |
|---|---|
| **Dashboard** | visão geral do cluster: custo por dia (médio), economia certa por mês, eficiência de CPU e de memória, HPA e câmbio USD/BRL |
| **Relatório** | visão executiva: para onde vai o dinheiro, tendência, maiores custos, plano de ação com economia, riscos e qualidade dos dados. Exporta em **PDF** |
| **Workloads** | três visões: **Rightsizing** (request/limit por workload), **Oportunidades** (ações por workload: HPA, requests, riscos) e **Acompanhamento** (recomendações já enviadas) |
| **Node Pools** | capacidade × requests × uso real por pool, custo da capacidade ociosa, sugestão de scale-down e alternativas de SKU. O botão de **Deep Analysis** abre o diagnóstico completo do pool (alocação, DaemonSets, workloads, HPAs/throttling e simulação de VMs) e exporta em Markdown |
| **HPA (Nd)** | histórico de réplicas e HPAs que poderiam ser removidos |
| **Armazenamento e Órfãos** | discos e PVCs sem uso e outros recursos órfãos. Só leitura: cada item traz o comando de exclusão para copiar |
| **Recursos de Dados** | recursos do Resource Group de dados do Azure, fora do Kubernetes (**só AKS**) |
| **Showback** | custo por namespace ou dono, por mês, exportável em CSV |
| **Todos os clusters** | ranking de custo e desperdício a partir do último relatório gravado de cada cluster (sem novo scan) |

## Como ler os números

- Os valores em reais do relatório são de **preço de tabela** da nuvem. Pools com **reserva ou Savings Plan** são mostrados pelo **custo efetivo**.
- `requests` de CPU e memória do workload são **por pod**; o total é multiplicado pelo número de pods.
- **Economia em pool sob reserva nunca é "certa"**: tirar nodes não devolve dinheiro enquanto a reserva continuar paga.
- A ocupação de um pool usa o recurso **mais cheio** (CPU ou memória), o maior entre request e uso.

## Aplicar uma recomendação

O FinOps **não altera o cluster sozinho**. **Aplicar** manda a mudança para o **Staging** (HPA real e requests do Deployment) ou gera o patch de values do Helm. Quem aplica é você, pelo Staging. O envio fica registrado no **Acompanhamento**.

## Permissões

- Lê pods, ReplicaSets, nodes e HPAs do cluster. Se o `list` de HPA no escopo do cluster for **negado**, a coleta tenta namespace por namespace e, onde também for negado, segue sem dados de HPA e mostra o aviso **Dados parciais**.
- Consulta **Dynatrace/Prometheus** (uso) e as **APIs da nuvem** (`az`, `aws`, `gcloud`) para preços e custo faturado; para isso é preciso estar logado na nuvem e, em cluster privado, na **VPN**.
- As ações que gravam (analisar, enviar recomendação, isenções) são rotas de escrita (perfil SRE no desenho).

## Impacto

**Somente leitura no cluster.** Grava só localmente: o cache do relatório, as recomendações enviadas e as isenções.

## Cuidados e erros comuns

- **"Análise feita sem Prometheus":** o uso real veio só do Dynatrace (ou de lugar nenhum). Sem uso real, desperdício, CPU/Mem e oportunidades não refletem o consumo.
- **"Nenhum workload recebeu dado real de uso":** os valores de desperdício provavelmente não valem. Se as consultas pesadas estouraram o tempo, use **Reanalisar com N dias** (janela menor); se completaram sem erro e sem dados, o problema é de configuração do Dynatrace/Prometheus, e reanalisar não resolve.
- **Dados parciais (HPA):** workloads escalados por HPA podem aparecer como se não tivessem, e o relatório pode sugerir reduzir réplicas deles. O conserto é liberar `list` em `horizontalpodautoscalers` para o seu usuário.
- Prometheus fora do endereço padrão: informe `prometheusUrl` do cluster em `~/.k8s-hpa-manager/clusters-config.json` e reinicie o servidor.

## Veja também

Node Pools, Staging, Dynatrace, Prometheus.
