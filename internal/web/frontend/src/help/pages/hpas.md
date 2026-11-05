## Para que serve

Lista os **HPAs** (HorizontalPodAutoscalers) do cluster e deixa você ajustar réplicas mínima e máxima, metas de CPU e memória e os **requests e limits** do Deployment, antes de aplicar.

## Quando usar

- Mudar o limite de réplicas ou a meta de CPU/memória de um HPA.
- Ajustar requests e limits do Deployment que o HPA escala.
- Preparar várias mudanças (em vários HPAs) para aplicar de uma vez.
- Acompanhar um HPA no Monitoramento.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace (ou **Todos**). A lista da esquerda traz **réplicas atuais, mínimo e máximo** de cada HPA; a busca filtra por nome ou namespace. O botão **Desmarcar** limpa a seleção.
2. Sem HPA selecionado, a direita mostra uma tabela com **nome, versão, Min e Max Replicas, Replicas, CPU Target (%) e Memory Target (%)**. **Clique em um HPA** (de novo para desmarcar) para abrir o editor.
3. O editor tem **Minimum Replicas**, **Maximum Replicas**, **Target CPU (%)**, **Target Memory (%)**, as réplicas atuais e a seção **Deployment Resources** (CPU Request, CPU Limit, Memory Request, Memory Limit).
4. Em **Rollout Options** você pode marcar **Deployment Rollout**, **DaemonSet Rollout** e **StatefulSet Rollout**, para reiniciar esses workloads depois da aplicação.
5. Escolha entre dois caminhos:
   - **Salvar (Staging):** guarda a mudança para aplicar depois, junto com as outras, pela aba Staging.
   - **Aplicar Agora:** abre **Confirmar Alterações** com o que vai mudar, e aplica no cluster ao confirmar.
6. **Cancelar** descarta o que você mudou no formulário.

## Validação

O editor confere os valores antes de salvar ou aplicar: o mínimo não pode ser maior que o máximo, as metas de CPU e memória ficam entre **1 e 100%**, e requests e limits precisam estar no formato do Kubernetes (CPU como `100m`, `0.5` ou `1`; memória como `128Mi` ou `1Gi`). Valores absurdamente altos também são barrados.

## O ícone de monitoramento

Cada HPA da lista tem um botão de **monitoramento**. Na primeira vez ele adiciona o HPA ao monitoramento (e liga o motor de monitoramento, se estiver desligado); se o HPA já está monitorado, leva você direto a ele na aba de Monitoramento.

## Permissões

Salvar e Aplicar só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes). Antes de aplicar, a ferramenta confere a conexão com a **VPN**.

## Impacto

**Altera o cluster.** Aplicar um HPA fica registrado no Change History, com o antes e o depois (réplicas, metas e requests/limits).

## Cuidados e erros comuns

- **Mudar requests e limits reinicia os pods.** Eles ficam no template do Deployment, e uma mudança ali dispara um rollout. A ferramenta altera o **primeiro container** do Deployment, que ela assume ser o principal: se o pod tem sidecars, confira.
- **Os rollouts marcados reiniciam os pods de verdade**, em produção. Marque só quando precisar.
- Uma meta de CPU muito baixa faz o HPA escalar cedo e gastar mais; muito alta, escalar tarde. A aba [FinOps](ajuda:finops) ajuda a calibrar.
- **Salvar (Staging) não muda o cluster:** o HPA só é alterado quando você aplica pela aba Staging.

## Veja também

[Deployments](ajuda:deployments), [FinOps](ajuda:finops).
