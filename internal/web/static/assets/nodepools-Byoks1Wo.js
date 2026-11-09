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
- **Atributos do storage:** abaixo da origem aparecem o **SKU** (ex.: \`Premium_LRS\`, \`Standard_GZRS\`, \`gp3\`, \`pd-ssd\`), o **tier de acesso** (Hot/Cool/TransactionOptimized, quando a StorageClass define), o **protocolo** (smb/nfs/fuse) e a **storage account**. A **redundância** é extraída do SKU — LRS (uma zona), ZRS (3 zonas), GRS/GZRS (com região secundária), RA-* (leitura na secundária) — e tem filtro próprio; o tooltip explica cada uma. O menu **⋮ → Detalhes do storage** mostra tudo isso mais os parâmetros brutos da StorageClass e do PV (credenciais e referências a Secrets são omitidas). Para volumes Azure, o botão **Consultar na Azure** (só quando você clica) faz leituras \`GET\` no Azure Resource Manager com o \`az login\` do servidor e traz: da **conta** o kind (StorageV2, FileStorage, BlobStorage…), SKU, tier de acesso, região e região secundária, status e atraso da replicação geo, TLS mínimo, acesso público a blobs, firewall e private endpoints, criptografia, HNS/NFSv3/SFTP; do **file share** tier, protocolo, cota e uso; do **container** acesso público e imutabilidade; da **proteção de dados** soft delete e versionamento; e do **Managed Disk** SKU, tier P/E, IOPS, throughput, estado e a VM anexada. Os achados que merecem atenção (acesso público, sem firewall, TLS antigo, soft delete desligado, share quase cheio, replicação atrasada, disco Unattached) vêm em **Pontos de atenção**. A conta precisa estar na subscription do cluster (ou o PV trazer o ID do disco); se faltar permissão de leitura, a parte afetada vira um aviso e o resto continua. Nada é alterado, e há também o comando \`az\` para copiar. Volumes provisionados dinamicamente numa conta gerenciada pelo AKS podem não informar o nome da conta; sem ele não há o que consultar. Volume sem SKU declarado fica sem a linha (o driver usa o padrão dele).
- **Ciclo de vida (blobs):** em **Consultar na Azure**, o detalhe de uma conta de blob traz a **política de ciclo de vida** e **cada regra**: o filtro (tipos de blob e prefixos), as ações (mover para **Cool**, **Cold** ou **Archive**, ou **excluir**, e depois de quantos dias da modificação, criação ou último acesso) e se a regra está **ativa**. Regras que **excluem dados** aparecem como ponto de atenção, e conta **sem política** é avisada (nada move nem expira os blobs sozinho). **Só volumes de blob têm ciclo de vida**: para **disco gerenciado** (Azure Disk, o caso da maioria dos PVCs, como os de dados do Prometheus) e para **Azure Files**, o detalhe mostra "Aplicável: não", porque essa política existe só em contas de blob.
- **Busca fora do cluster:** se o texto da busca (por exemplo o nome de uma storage account) **não aparece em nenhum volume do cluster**, a tela oferece **Buscar na subscription**. A ferramenta lista, só em leitura, as **storage accounts** e os **discos** da subscription do cluster cujo nome contém o texto, e mostra o aviso **"não pertence ao cluster, mas está na mesma subscription"**, com resource group, região, SKU, containers, ciclo de vida e se o resource group é de infraestrutura do AKS (\`MC_…\`). **Buscar também em containers e file shares** varre as contas da subscription (uma chamada por conta, até 30 por subscription), então pode levar cerca de um minuto. Se nada for encontrado, **Buscar em todas as subscriptions do az login** inclui também as subscriptions que **nenhum cluster usa**, mas que o \`az login\` do servidor enxerga (caso típico de um storage de uma Function ou de outro projeto), e a lista de **subscriptions consultadas** mostra quantas contas e discos o Azure listou em cada uma. Precisa do \`az login\` válido no servidor e de permissão de leitura na subscription.
- **Zona do disco:** discos zonais (Azure Disk, EBS, GCE PD) mostram a zona abaixo da origem, há filtro por zona e dois contadores no topo. Entra em **Situação** o disco numa **zona sem nenhum node** (não monta até existir um) e o volume em uso numa **zona com um só node** (se cair, o pod não reagenda em outra zona). Disco regional, sem zona ou cluster sem label de zona nos nodes ficam de fora.
- **Expansão e snapshots:** cada PVC mostra se a StorageClass é **expansível** (\`allowVolumeExpansion\`). Uma expansão em andamento ou esperando o pod reiniciar entra em Situação, e, com **Incluir uso**, o volume crítico/em atenção numa StorageClass **sem expansão** é apontado (a saída é migrar os dados). Quando o cluster tem o snapshot-controller, aparece a contagem de **VolumeSnapshots prontos** (com o último no tooltip) e o filtro **Sem snapshot**; sem a API ou sem permissão, esse filtro nem aparece — não dá para afirmar que não há snapshot. Snapshots feitos fora do Kubernetes (backup do provedor) não são vistos.
- **Gerenciado por GitOps/Helm:** o volume (ou o workload dono) criado por **Flux** (Kustomization/HelmRelease), **Argo CD** ou **Helm** mostra \`⎈ nome\` na coluna PVC, detectado pelas labels/annotations padrão de cada ferramenta. Nos diálogos de **Expandir** e **Apagar** aparece um aviso: o controlador pode reverter a mudança (e, ao tentar reduzir, o reconcile falha) ou recriar o PVC vazio; atualize o manifesto no Git junto. Para PVC de **StatefulSet**, o aviso mostra o tamanho do \`volumeClaimTemplate\`, que é **imutável**: réplicas novas nascem com o tamanho antigo, e igualar exige recriar o StatefulSet com \`--cascade=orphan\`. É só aviso — nada é bloqueado.
- **Idade:** a coluna **Idade** mostra há quanto tempo o PVC (ou o PV, se não há PVC) foi criado, ordenável, com a data no tooltip. Volume **ocioso** (PVC sem pod ou PV sem PVC) criado há mais de 30 dias entra em Situação como candidato a limpeza e conta em "ociosos antigos". O Kubernetes não registra desde quando o volume está ocioso, então o servidor passa a **registrar** (em \`volume_idle.db\`) a primeira vez que o viu ocioso e mostra **"ocioso há N d*"** abaixo da idade; o registro de um volume some quando ele volta a ser usado ou é apagado. O \`*\` avisa que a contagem começa na primeira vez que este servidor listou o volume (abrir o modal), então antes disso só vale a idade, que é o teto. Confira antes de apagar (pode ser um volume de job sazonal).
- **Ações (escrita):** a coluna **Ações** é um menu **⋮** com **Tirar snapshot**, **Expandir** e **Apagar** (só aparecem os que o seu RBAC permite). **Tirar snapshot** (PVC Bound de um driver CSI com snapshot, como Azure Disk ou EBS): o diálogo confere a **versão da API** \`snapshot.storage.k8s.io\` que o cluster serve (\`v1\` ou \`v1beta1\`, mostrada também no tooltip do contador "com snapshot"), lista só as **VolumeSnapshotClasses do mesmo driver** do volume (sugere a padrão) e cria o VolumeSnapshot no namespace do PVC; ele leva instantes até ficar pronto e **cobra armazenamento até ser apagado**. NFS, HostPath e Local não têm snapshot pelo Kubernetes, e sem os CRDs/snapshot-controller (ou sem permissão) a opção nem aparece. Nos diálogos de Expandir e Apagar, um PVC sem snapshot ganha um aviso com o atalho **Tirar snapshot antes**. **Expandir** (PVC Bound numa StorageClass expansível): informe o novo tamanho em GiB inteiros; só cresce, nunca reduz (o custo mensal sobe de forma permanente). **Não apaga dados:** aumenta o disco e o filesystem. Os riscos são outros: dependendo do driver, o disco precisa ser desanexado (interrupção breve), uma falha deixa o PVC em *Resizing* ou *FileSystemResizePending* até o pod reiniciar (aparece em Situação como expansão em andamento/pendente) e o provedor pode recusar por cota ou tamanho máximo. Em volume importante, tire um **snapshot antes**. **Apagar** (só volume ocioso: PVC sem pod ou PV Released/Available): é preciso **copiar a frase de confirmação** mostrada no diálogo (\`apagar pvc <ns>/<nome>\` ou \`apagar pv <nome>\`) e **colá-la** no campo — digitar não vale, e o servidor exige a mesma frase; o aviso destaca quando o reclaim é **Delete**, que apaga o disco e os dados junto, sem desfazer. Os itens do menu só aparecem se o seu RBAC no namespace do volume permite a ação (create de \`volumesnapshots\` para o snapshot; patch de PVC para expandir; delete de PVC ou PV para apagar — o PV precisa de regra de cluster). O servidor relê o objeto e revalida tudo antes de agir (PVC com pod é recusado), respeita o RBAC do cluster e cada ação vai para o Change History (só identificação, sem conteúdo). A lista recarrega ao terminar.
- **Filtros e ordem:** busca por aplicação, PVC, PV, pod, node ou servidor NFS; filtro por namespace, pool e tipo; **Só com problema**; clique no cabeçalho da coluna para ordenar.
- **Incluir uso (Prometheus):** mostra o uso real de cada volume (barra e %), o **crescimento médio dos últimos 7 dias** (regressão linear, que ignora picos pontuais) e a **previsão de quando enche** no ritmo atual. Classifica em **Crítico** (uso a partir de 90% ou enche em até 7 dias), **Atenção** (a partir de 80% ou até 30 dias) e **Superdimensionado** (volume de 10 GiB ou mais usando menos de 20%), com filtro próprio e contadores no topo; os casos viram também itens da coluna Situação. Só volumes **montados por um pod** têm métrica (vem do kubelet); sem histórico suficiente ou sem crescimento, não há previsão. Disco não reduz no lugar: reduzir um superdimensionado exige recriar o volume. Se o Prometheus do cluster não estiver acessível ou não coletar o kubelet, o modal avisa e segue sem uso. Não se aplica a NFS direto no pod.
- **Incluir custo (FinOps):** mostra o custo mensal (R$) por volume e uma visão **Por aplicação** (quanto cada aplicação gasta em volumes, em ordem de custo, com a % do total). O custo é **efetivo** (Cost Management, com reserva/desconto) quando existe para o disco — só Azure — e, no resto, uma **estimativa por preço de tabela on-demand** pelo tipo e tamanho do disco (Azure, AWS e GCP), sem reserva. O topo soma o total e quanto está em **volumes ociosos** (PVC sem pod, PV sem PVC). Volume compartilhado entra **dividido** entre as aplicações. **NFS, Azure Files, EFS, HostPath e Local não têm preço por volume** (o custo é do servidor/share) e aparecem como "sem preço"; disco cujo tipo não dá para identificar também. Por isso o total pode **subestimar** o gasto real. Depende da cotação USD→BRL e dos preços do FinOps.
- **CSV:** exporta a lista como está filtrada, com as aplicações, a origem do vínculo, zona, idade, expansível, snapshots e (se ligados) custo e uso; na visão Por aplicação e na de Servidores NFS, exporta o que está na tela.

### Varredura periódica de volumes

O botão **🔔** ao lado de **Volumes** abre a varredura: você marca os clusters e, **a cada 30 minutos, enquanto o servidor estiver ligado**, eles são lidos (com o uso do Prometheus) e o resultado fica guardado. O botão ganha um **selo** com a soma de volumes **enchendo** (crítico) e **ociosos antigos** (mais de 30 dias) — vermelho se surgiu algo **novo** desde a varredura anterior, âmbar se já era conhecido. **Sem snapshot** aparece só como informação, sem entrar no selo.

- **Escolha dos clusters:** marque e clique em **Salvar lista** (guardada localmente; no máximo 50). A primeira varredura roda cerca de 1 minuto depois; **Varrer agora** não espera o ciclo.
- **Por cluster:** hora da última leitura, os três contadores, os achados (clique no nome para abrir a lista; o • marca os novos) e **Abrir Volumes** para ir ao modal daquele cluster. O filtro **Só novos** mostra o que apareceu desde a varredura anterior.
- **Falhas:** se a leitura de um cluster falha (VPN, credencial), o painel avisa e **mantém o último estado conhecido**; se só o Prometheus falha, avisa que "enchendo" pode estar incompleto.
- **Limites:** **não é um serviço 24x7** — o servidor se desliga sozinho depois de uns 40 minutos sem o navegador aberto, e a varredura para junto. Ela usa a rede e as credenciais do processo do servidor (se ele subiu sem VPN, os clusters privados falharão). Cada ciclo também alimenta o registro de **"ocioso há N d*"**, o que torna essa contagem mais confiável. Somente leitura.

A listagem é somente leitura e usa o acesso de leitura do cluster (PVCs, PVs, pods, nodes, StorageClasses, snapshots e os workloads); só os botões da coluna **Ações** escrevem no cluster. Sem permissão de listar nodes, a coluna de pool fica vazia; sem permissão de listar workloads, o dono só vem dos pods e das labels. Um volume que nenhum pod monta no momento aparece sem pool.

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
