Na aba aparece como **Node Pools** em clusters AKS e como **Node Groups** em clusters EKS; as ações são as mesmas.

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
   - **Salvar (Staging):** guarda a mudança para aplicar depois, junto com outras, pela aba Staging.
   - **Aplicar Agora:** abre a confirmação e aplica ao confirmar.
5. **Análise Preditiva** (e seu histórico) avalia o pool com base nos dados coletados.

## O que cada aba mostra

| Aba | Conteúdo |
|---|---|
| **Nodes** | os nodes do pool, o CPU e a memória **solicitados** por node e os pods que mais consomem. O selo **NR-n** conta os nodes **removidos**, com a busca de quem os removeu (autoscaler do AKS, usuário, automação ou plataforma Azure, como spot e manutenção) |
| **Disk** | uso de disco dos nodes e detalhes do disco |
| **Conntrack** | monitoramento da tabela de conexões (conntrack) do pool |
| **Pendentes** | workloads com pods **não prontos**, que podem estar esperando capacidade |

## Troca sequencial entre dois pools

Em **Execution Order**, marque um pool como `*1` e outro como `*2`. Com os dois marcados, aparece **Configure Sequencing**, e a ferramenta executa em fases:

1. **PRE-DRAIN:** aplica as mudanças no pool de destino (por exemplo, escalar para cima) e espera 30 segundos para os nodes ficarem prontos.
2. **CORDON:** marca os nodes do pool de origem como não agendáveis.
3. **DRAIN:** esvazia os nodes do pool de origem (grace period, timeout e opções como `kubectl drain`).
4. **POST-DRAIN:** aplica as mudanças finais (por exemplo, reduzir a origem).
5. **FINALIZAÇÃO:** encerramento e registro.

O progresso aparece em tempo real, e uma operação em andamento pode ser **abortada**.

## Permissões

Aplicar só fica disponível com permissão de escrita no cluster. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes). Antes de aplicar, a ferramenta confere a conexão com a **VPN**. As operações usam as CLIs da cloud (`az` ou `aws`) no servidor, com o login que ele tem.

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

[Pods](ajuda:pods), [Events](ajuda:events), [FinOps](ajuda:finops).
