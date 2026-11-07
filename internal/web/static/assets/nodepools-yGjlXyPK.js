const o=`Na aba aparece como **Node Pools** em clusters AKS e como **Node Groups** em clusters EKS; as ações são as mesmas.

## Para que serve

Lista os **node pools** do cluster e permite ver a configuração de cada um (VM, disco, versão), mudar o **número de nodes** e o **autoscaling**, ver os nodes e as cargas pendentes e fazer a **troca sequencial** entre dois pools com cordon e drain.

## Quando usar

- Aumentar ou reduzir um pool, ou ligar e desligar o autoscaling com mínimo e máximo.
- Ver quanto cada node está ocupado e quais pods mais consomem.
- Saber por que um node foi removido (autoscaler, spot, ação manual).
- Migrar a carga de um pool para outro sem derrubar a aplicação.
- Ver pods que não conseguem ser agendados (pendentes).

## Como usar (passo a passo)

1. Escolha o cluster. A lista da esquerda traz os pools; o botão de atualizar relê do provedor.
2. **Clique em um pool** para abrir o editor, com cinco abas: **Configuration**, **Nodes**, **Disk**, **Conntrack** e **Pendentes**.
3. Em **Configuration**, edite o **Autoscaling**. Com ele desligado você define o **Node Count**; com ele ligado, **Min Nodes** e **Max Nodes**. O painel também mostra a VM (com as especificações), o disco do SO, a versão do Kubernetes, a imagem do node, o máximo de pods por node, a prioridade (**Spot** é destacado como "pode ser removido") e os taints do pool.
4. Escolha como aplicar:
   - **Salvar (Staging):** guarda a mudança para aplicar depois, junto com outras, pela aba [Staging](ajuda:staging).
   - **Aplicar Agora:** abre a confirmação e aplica ao confirmar.
5. **Análise Preditiva** (e seu histórico) avalia o pool com base nos dados coletados.

## O que cada aba mostra

| Aba | Conteúdo |
|---|---|
| **Nodes** | os nodes do pool, o CPU e a memória **solicitados** por node e os pods que mais consomem. O selo **NR-n** conta os nodes **removidos**, com a busca de quem os removeu (autoscaler do AKS, usuário, automação ou plataforma Azure, como spot e manutenção) |
| **Disk** | uso de disco dos nodes e detalhes do disco |
| **Conntrack** | monitoramento da tabela de conexões (conntrack) do pool |
| **Pendentes** | workloads com pods **não prontos**, que podem estar esperando capacidade |

## Volumes (PV/PVC) do cluster

O botão **Volumes**, no cabeçalho do painel direito (junto dos widgets de SNAT e Conntrack), abre a visão de **todos os volumes persistentes do cluster** e de **quais aplicações os usam**. Cada linha cruza o **PVC**, o **PV** que o atende, a **origem** do disco, as **aplicações**, o **node pool/group** onde elas rodam, a storage class e a capacidade.

- **Aplicações:** lista **todas** as aplicações que usam o volume (um PVC pode ser compartilhado), com o tipo (Deployment, StatefulSet, DaemonSet, CronJob, Job…) e **como o vínculo foi descoberto**: por um **pod** rodando; pelo **spec do workload** (acha o dono mesmo com 0 réplicas ou entre execuções de um CronJob); pelo **volumeClaimTemplate** de um StatefulSet (PVC \`<template>-<sts>-<n>\`); ou só pela **label** do PVC (\`app.kubernetes.io/name\`/\`app\`), quando nenhum workload o referencia. Volume sem nenhum desses vínculos aparece como **sem aplicação identificada**. Para um PV Released, o dono vem do PVC que existia.
- **Origem e NFS:** mostra o tipo do backend de cada volume — **NFS** (com \`servidor:/caminho\`), NFS via CSI, EFS, Azure Disk, Azure Files, EBS, GCE PD, HostPath, Local. Também entram os **NFS montados direto no pod** (sem PV nem PVC), com a aplicação que os monta. A busca encontra servidor e caminho NFS, e há filtro por tipo. A visão **Servidores NFS** (botão ao lado de Volumes, só aparece se houver NFS) agrupa por servidor: quantos volumes, **quantas aplicações dependem dele** (destacado a partir de 5), os exports, os namespaces e quantos volumes estão ociosos — útil para saber o que para se um servidor cair ou for migrado. Respeita os filtros de namespace e pool; tem CSV próprio. Capacidade de NFS é só a declarada no PV, não o espaço real do servidor.
- **Situação:** aponta o que costuma ser desperdício ou problema — PVC **sem pod** usando, PVC **Pending** ou **Lost**, PV **sem PVC** (disco ocioso), PV **Released** (o PVC foi apagado mas o disco continua e cobra) e PV com reclaim **Retain** sem uso.
- **Zona do disco:** discos zonais (Azure Disk, EBS, GCE PD) mostram a zona abaixo da origem, há filtro por zona e dois contadores no topo. Entra em **Situação** o disco numa **zona sem nenhum node** (não monta até existir um) e o volume em uso numa **zona com um só node** (se cair, o pod não reagenda em outra zona). Disco regional, sem zona ou cluster sem label de zona nos nodes ficam de fora.
- **Expansão e snapshots:** cada PVC mostra se a StorageClass é **expansível** (\`allowVolumeExpansion\`). Uma expansão em andamento ou esperando o pod reiniciar entra em Situação, e, com **Incluir uso**, o volume crítico/em atenção numa StorageClass **sem expansão** é apontado (a saída é migrar os dados). Quando o cluster tem o snapshot-controller, aparece a contagem de **VolumeSnapshots prontos** (com o último no tooltip) e o filtro **Sem snapshot**; sem a API ou sem permissão, esse filtro nem aparece — não dá para afirmar que não há snapshot. Snapshots feitos fora do Kubernetes (backup do provedor) não são vistos.
- **Idade:** a coluna **Idade** mostra há quanto tempo o PVC (ou o PV, se não há PVC) foi criado, ordenável, com a data no tooltip. Volume **ocioso** (PVC sem pod ou PV sem PVC) criado há mais de 30 dias entra em Situação como candidato a limpeza e conta em "ociosos antigos". O Kubernetes não registra desde quando o volume está ocioso: a idade é só o teto desse tempo, então confira antes de apagar (pode ser um volume de job sazonal).
- **Filtros e ordem:** busca por aplicação, PVC, PV, pod, node ou servidor NFS; filtro por namespace, pool e tipo; **Só com problema**; clique no cabeçalho da coluna para ordenar.
- **Incluir uso (Prometheus):** mostra o uso real de cada volume (barra e %), o **crescimento médio dos últimos 7 dias** (regressão linear, que ignora picos pontuais) e a **previsão de quando enche** no ritmo atual. Classifica em **Crítico** (uso a partir de 90% ou enche em até 7 dias), **Atenção** (a partir de 80% ou até 30 dias) e **Superdimensionado** (volume de 10 GiB ou mais usando menos de 20%), com filtro próprio e contadores no topo; os casos viram também itens da coluna Situação. Só volumes **montados por um pod** têm métrica (vem do kubelet); sem histórico suficiente ou sem crescimento, não há previsão. Disco não reduz no lugar: reduzir um superdimensionado exige recriar o volume. Se o Prometheus do cluster não estiver acessível ou não coletar o kubelet, o modal avisa e segue sem uso. Não se aplica a NFS direto no pod.
- **Incluir custo (FinOps):** mostra o custo mensal (R$) por volume e uma visão **Por aplicação** (quanto cada aplicação gasta em volumes, em ordem de custo, com a % do total). O custo é **efetivo** (Cost Management, com reserva/desconto) quando existe para o disco — só Azure — e, no resto, uma **estimativa por preço de tabela on-demand** pelo tipo e tamanho do disco (Azure, AWS e GCP), sem reserva. O topo soma o total e quanto está em **volumes ociosos** (PVC sem pod, PV sem PVC). Volume compartilhado entra **dividido** entre as aplicações. **NFS, Azure Files, EFS, HostPath e Local não têm preço por volume** (o custo é do servidor/share) e aparecem como "sem preço"; disco cujo tipo não dá para identificar também. Por isso o total pode **subestimar** o gasto real. Depende da cotação USD→BRL e dos preços do FinOps.
- **CSV:** exporta a lista como está filtrada, com as aplicações e a origem do vínculo.

É somente leitura e usa o acesso de leitura do cluster (PVCs, PVs, pods, nodes e os workloads). Sem permissão de listar nodes, a coluna de pool fica vazia; sem permissão de listar workloads, o dono só vem dos pods e das labels. Um volume que nenhum pod monta no momento aparece sem pool.

## Troca sequencial entre dois pools

Em **Execution Order**, marque um pool como \`*1\` e outro como \`*2\`. Com os dois marcados, aparece **Configure Sequencing**, e a ferramenta executa em fases:

1. **PRE-DRAIN:** aplica as mudanças no pool de destino (por exemplo, escalar para cima) e espera 30 segundos para os nodes ficarem prontos.
2. **CORDON:** marca os nodes do pool de origem como não agendáveis.
3. **DRAIN:** esvazia os nodes do pool de origem (grace period, timeout e opções como \`kubectl drain\`).
4. **POST-DRAIN:** aplica as mudanças finais (por exemplo, reduzir a origem).
5. **FINALIZAÇÃO:** encerramento e registro.

O progresso aparece em tempo real, e uma operação em andamento pode ser **abortada**.

## Permissões

Aplicar só fica disponível com permissão de escrita no cluster. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes). Antes de aplicar, a ferramenta confere a conexão com a **VPN**. As operações usam as CLIs da cloud (\`az\` ou \`aws\`) no servidor, com o login que ele tem.

## Impacto

**Altera a infraestrutura e custa dinheiro.** Aplicar um node pool e a sequência completa ficam registrados no Change History, assim como o cordon e o drain.

## Cuidados e erros comuns

- **Reduzir o número de nodes remove VMs de verdade**, e os pods delas são reagendados (ou perdidos, se não houver capacidade). Faça cordon e drain antes de reduzir.
- **Aumentar nodes aumenta o custo** na hora; a cobrança da VM começa assim que ela é criada.
- Desligar o **autoscaling** congela o pool no **Node Count**: ele deixa de crescer sozinho em picos.
- **Pool Spot** pode perder nodes a qualquer momento por decisão da cloud; não é falha da ferramenta.
- A operação do provedor pode **demorar vários minutos**. Se um node pool fica em estado de transição (Updating, Scaling), espere: a lista atualiza sozinha.
- Na troca sequencial, confira antes se o pool de destino tem capacidade para receber os pods; senão eles ficam **pendentes**.

## Veja também

[Staging](ajuda:staging), [Nodes](ajuda:nodes), [Pods](ajuda:pods), [Events](ajuda:events), [FinOps](ajuda:finops).
`;export{o as default};
