## O que é esta análise

A Deep Analysis olha **um node pool** e responde: o que está rodando nele, quanto ele custa e se outra VM caberia melhor. Ela só **lê**; nada é alterado no cluster. Cada número abaixo vem de uma conta descrita nesta página.

## De onde vêm os dados (coleta)

| Dado | Fonte | Observação |
|---|---|---|
| Nodes e pods do pool | API do Kubernetes, ao vivo | só os nodes do pool e os pods Running neles |
| Uso atual (CPU e memória) | metrics-server | sem ele, o uso ao vivo fica em branco |
| HPAs | API do Kubernetes, ao vivo | mínimo, máximo, alvo e réplicas atuais |
| Uso histórico (P95 e pico por pod) | último relatório FinOps **em cache** | não faz novo scan; vale a janela daquele relatório (por exemplo, 30 dias) |
| Throttling de CPU | Prometheus do cluster | P95 por pod, na mesma janela do histórico; sem Prometheus acessível, a coluna fica vazia |
| Preço das VMs | tabela de preços da Azure | ver "Como o preço é obtido" |
| Cobertura de reserva e Savings Plan | Cost Management, gravado pelo FinOps | ver "Custo amortizado" |
| Disponibilidade das SKUs | catálogo da região (`Microsoft.Compute/skus`) | na primeira análise de uma região, espera até 20 s para carregar |

A análise inteira tem um limite de 90 s. Quando um dado não pôde ser coletado, ele aparece num **aviso no topo do modal** (e em "Avisos da coleta" no Markdown exportado) em vez de ser inventado.

## Como ler cada aba

- **Resumo:** achados com severidade (crítico, aviso, informação) e os números principais do pool.
- **Alocação e nodes:** por node, quanto está **reservado** (requests e limits) e quanto está **em uso**. "Gargalo de agendamento" é o recurso que mais reserva; "gargalo real" é o que mais é usado de fato. Quando diferem, há request fora da realidade.
- **DaemonSets:** o custo fixo que **todo node paga** antes de receber qualquer aplicação (CPU, memória e pods). Quanto menor o node, maior a fatia.
- **Workloads:** por pod, request atual contra uso (P95, pico ou ao vivo) e o request **recomendado**.
- **HPAs e throttling:** como cada HPA está configurado e quais workloads sofrem limite de CPU.
- **Simulação de VMs:** quantos nodes de outra VM entregam a mesma capacidade que o pool tem hoje, e quanto isso custaria (resto desta página).

## Como chegamos ao número de nodes de uma VM ("Mesma capacidade de hoje")

A pergunta é simples: **o pool de hoje entrega uma certa capacidade; quantos nodes da outra VM entregam o mesmo?** O que roda dentro dos nodes não entra nessa conta.

1. **Capacidade de hoje** = soma do alocável de todos os nodes atuais. Exemplo: 48 nodes de 3,9 cores e 13,3 GiB = **187,2 cores e 638 GiB**. O alocável é o que o Kubernetes enxerga no node (já descontado o que a AKS reserva para o sistema), **medido** nos nodes do pool.
2. **Alocável de uma VM candidata.** Estimado pela fórmula da AKS: CPU reservada de 60m (1 core), 100m (2) ou 140m (4), mais 10m por core acima de 4; memória reservada de min(20 MB × máx. de pods + 50 MB, 25%) mais 100 Mi de eviction, sobre a memória que o node de fato expõe (fator medido no pool, entre 0,90 e 1,00; 0,98 sem medição). Por isso aparece "estimado".
3. **Nodes por recurso**, arredondados para cima:
   - **CPU** = capacidade de CPU de hoje ÷ alocável de CPU de um node da VM;
   - **Memória** = capacidade de memória de hoje ÷ alocável de memória de um node;
   - **Pods** = pods em execução hoje ÷ limite de pods por node (110 no padrão da AKS; usa o valor dos nodes do pool).
4. **Resultado.** O **maior** dos três define o número de nodes ("← define"). O mínimo é de **3 nodes** (ou o número de zonas, se maior).

Exemplo com a E8s_v5 (7,8 cores e 60,3 GiB por node): CPU = 187,2 ÷ 7,8 = **24**; memória = 638 ÷ 60,3 = 10,6 → **11**; pods, 700 ÷ 110 = 6,4 → 7. O maior é a CPU, então **24 nodes**.

Uma VM de mesma forma que a atual dá os mesmos nodes de hoje. Como o alocável das candidatas é estimado e o da atual é medido, uma diferença de até 1,5% entre os dois não conta como um node a mais.

Quem são as VMs candidatas: as mesmas vCPUs da atual em todas as séries conhecidas e o dobro (até 16 vCPUs). A atual nunca aparece como candidata. VMs que o catálogo diz indisponíveis na região aparecem esmaecidas.

### Coluna opcional "Reduzindo requests"

É um segundo cenário, **opcional**: além de trocar a VM, reduzir os requests dos workloads ao uso real. Aqui, sim, entra o que roda nos nodes: a demanda é a soma dos requests recomendados (CPU = P95 × 1,2, mínimo 50m; memória = o maior entre P95 e uso atual × 1,2) e cada node só oferece o que sobra depois dos DaemonSets. Só faz sentido se você realmente ajustar os requests. O encaixe aqui é o **perfeito** (100%), então tende a ser o mínimo teórico. Se o pool usa hoje mais nodes do que o encaixe perfeito exigiria, a tela mostra, em âmbar, o número no **empacotamento de hoje** (nodes ideais ÷ empacotamento observado).

## Como o preço é obtido

- **Preço de uma VM:** tabela de preços **sob demanda** da Azure (Retail Prices), em US$ por hora, guardada em cache local. Se a API falhar, o servidor usa um preço de referência embutido, e o valor pode divergir da tabela real. Pool **Spot** também usa o preço sob demanda (o custo real do Spot é menor).
- **Câmbio:** cotação USD→BRL de uma API pública, com cache de 1 hora; se estiver indisponível, usa 5,50 de referência (e o aviso aparece na coleta).
- **Custo de um node por mês** = preço por hora × 730 horas × câmbio.
- **Custo do pool por mês** = número de nodes × custo de um node. **Todos os valores em R$ da tela são do pool inteiro, por mês**, a menos que digam "por node".
- **Pool hoje** = nodes atuais × custo de um node da VM atual, a preço de tabela.
- **Contrato e descontos negociados não entram** no preço de tabela. Eles só aparecem pelo custo amortizado, quando existe reserva ou Savings Plan.

## Custo amortizado (reserva e Savings Plan)

O preço de tabela engana num pool coberto por reserva ou Savings Plan: ele mostra o que custaria sob demanda, não o que você paga. Por isso, nesses casos a tela mostra **os dois**, lado a lado: o valor de **tabela (sob demanda)** e o **amortizado**.

- **Custo amortizado do pool:** o custo que o Cost Management atribui ao pool, com o valor da reserva ou do plano **diluído** pelo período, convertido para um mês (custo × 730 ÷ 24 ÷ dias da janela). É dividido em reserva, Savings Plan e sob demanda.
- **Desconto da cobertura:** amortizado contra tabela. Negativo significa que a cobertura baixa o custo.
- **Nodes cobertos pela reserva:** nodes atuais menos o custo sob demanda e do plano dividido pelo custo de tabela de um node. É uma estimativa, e supõe os nodes atuais ligados a janela inteira. O custo de um **node reservado** é o custo da reserva ÷ esses nodes.
- **Pior e melhor caso** (por VM, na mesma capacidade de hoje):
  - *Pior:* a reserva atual **continua paga e ociosa**, e o que passar dela sai a preço de tabela.
  - *Melhor:* a reserva é reaproveitada por outro pool ou cluster no escopo dela, ou trocada.
  - VM da **mesma série** é coberta na proporção dos vCPUs (flexibilidade de tamanho, o padrão da Azure). VM de **outra série** não é coberta pela reserva atual.
- **O que não tem número:** com Savings Plan acima de 20% do custo, moeda diferente de BRL ou pool Spot, não há cenário amortizado por VM. Mostrar um número seria chute, então a tela mostra o amortizado de hoje e avisa.
- **"Já existe reserva para esta SKU":** vem do uso **observado** no Cost Management. Diz que a reserva existe, **não** que há capacidade ociosa nela (isso exigiria o papel Reservations Reader).

## Como ler "Pool hoje × com esta VM"

- **Tabela (sob demanda):** custo de hoje (nodes de hoje × preço de um node), custo com a VM na **mesma capacidade** (nodes equivalentes × preço de um node) e a diferença em R$ e em %. Verde e "−" é mais barato; vermelho e "+" é mais caro.
- **Amortizado:** o mesmo quando há reserva ou Savings Plan: amortizado de hoje contra o pior e o melhor caso com a VM.
- **Reduzindo requests (opcional):** a mesma comparação no cenário em que os requests também são ajustados.

## Como ler a justificativa da troca

Cada VM recebe um veredito, sempre contra a VM atual e na mesma capacidade de hoje:

- **Justificada:** a economia se sustenta, inclusive no pior caso quando há reserva.
- **Com ressalva:** há economia só se a reserva for reaproveitada, ou ela concentra risco (perder 1 node derruba mais de 15% do pool).
- **Ganho técnico:** sem economia, mas com ganho mensurável para os workloads e custo a mais de no máximo 10%. Os ganhos são: nodes a menos (menos DaemonSets duplicados), menor impacto de perder um node, VM sem SMT quando há workloads com throttling, e o maior pod ocupando menos de um node.
- **Não justificada:** indisponível, não comporta, mais cara sem ganho ou sem economia.
- **Sem dados:** falta preço para comparar.

O selo **melhor** vai só para VMs justificadas ou com ressalva, com no máximo 15% de impacto ao perder um node.

## "O que cabe em 1 node desta VM"

Referente ao cenário opcional de requests: para o pod médio do pool, mostra quantos cabem em um node pela CPU, pela memória e pelo limite de pods (o menor manda), quantos do **maior pod** cabem e a comparação **hoje × nesta VM** de pods, CPU e memória reservados por node. Node com 95% ou mais de reserva é sinalizado, porque a simulação usa 100% de ocupação e não deixa folga para picos, HPA ou perda de um node.

## Limites e cuidados

- Tudo parte de **requests** e do uso medido. O encaixe **não considera** afinidade, anti-affinity, taints ou PDBs; ele mostra capacidade, não garante como o scheduler distribuirá.
- Preço de tabela não é o seu contrato. Com reserva ou Savings Plan, confie no custo **amortizado**.
- "Estimado" no alocável significa fórmula, não medição. Confirme antes de migrar.
- O histórico é o do último relatório FinOps: se ele for antigo, atualize o relatório antes de decidir.
- A capacidade equivalente troca **o mesmo total** de CPU e memória, sem folga extra. Se hoje o pool tem sobra, ela continua tendo; se faltar capacidade, a VM nova também faltará.
