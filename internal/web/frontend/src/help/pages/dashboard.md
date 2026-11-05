## Para que serve

A **visão geral do cluster** escolhido: quantos clusters, namespaces, HPAs e node pools existem, os **alertas críticos**, os dados do cluster (versão, nodes, pods), o **uso de CPU e memória** e os **5 namespaces** que mais consomem.

## Quando usar

- Ter um panorama rápido ao começar o dia ou ao trocar de cluster.
- Ver se há algo crítico antes de mexer em qualquer coisa.
- Descobrir **qual namespace** pesa mais no cluster.
- **Cadastrar clusters novos** automaticamente (Auto-Descobrir Clusters).

## O que aparece na tela

**Cartões do topo:**

| Cartão | Conteúdo |
|---|---|
| **Clusters** | total de clusters cadastrados |
| **Namespaces** | namespaces do cluster escolhido |
| **HPAs** | HPAs do cluster escolhido |
| **Node Pools** (ou **Node Groups** no EKS) | pools do cluster escolhido |
| **Alertas Críticos** | soma dos alertas críticos (veja abaixo) |

**Informações do Cluster:** nome, **contexto**, **versão do Kubernetes**, número de **nodes** e de **pods**.

**CPU e Memória:** dois medidores com a porcentagem de uso. Mudam de cor a partir de **70%** (atenção) e **90%** (perigo). Cada um mostra também a fração **Allocatable**, o que o scheduler pode usar, e o que fica como **System Reserved**.

**Top 5 Namespaces por Consumo:** os cinco maiores namespaces em CPU e memória.

Os dados **se atualizam sozinhos a cada 30 segundos**, sem recarregar a tela.

## O cartão de Alertas Críticos

O número soma três fontes: os alertas críticos de **HPAs**, de **Node Pools** e do **Alertmanager**, mais os críticos da **integridade do Prometheus** (Prometheus fora do ar ou desligado por uma limpeza interrompida). O subtítulo diz de onde vêm (`HPAs`, `Nodes`, `Prometheus`). **Clique no cartão** quando ele estiver vermelho: se houver crítico do Alertmanager, abre a página de alertas do cluster em uma nova aba; se o único crítico for o do Prometheus, leva à aba [Prometheus](ajuda:prometheus).

## Auto-Descobrir Clusters

Esse botão procura configurações de clusters **AKS** (Azure), **EKS** (AWS) e **GKE** (GCP) **em paralelo**, a partir do kubeconfig local do servidor, e as cadastra na ferramenta, com progresso na tela. Para o **GCP**, se o servidor não estiver autenticado, a janela oferece **Autenticar GCP**: ela abre um link de login web e espera você concluir. Ao terminar, o Dashboard recarrega os dados.

## Permissões

O Dashboard é de **consulta**. A Auto-Descoberta usa os logins de nuvem que o servidor tem (`az`, `aws`, `gcloud`) e mexe no cadastro de clusters da ferramenta.

## Impacto

**Somente leitura**, com uma exceção: a **Auto-Descoberta** grava ou atualiza o cadastro de clusters da ferramenta (os arquivos de configuração de AKS, EKS e GKE). Nada muda nos clusters em si.

## Cuidados e erros comuns

- **O uso de CPU e memória vem do Metrics Server.** Se o cluster não tem Metrics Server ou ele não responde, os medidores **caem para uma estimativa**: a soma dos **requests** dos pods contra o allocatable. Nesse caso o número mostra **reserva**, não consumo real, e pode ser bem diferente.
- Os **Top 5 Namespaces** usam os **requests** dos pods, não o consumo medido.
- **Cartões zerados ou "..."** costumam ser rede ou VPN fora; o cartão de erro mostra a mensagem e o botão **Tentar Novamente**.
- **Um cluster novo só aparece depois de cadastrado**: use a Auto-Descoberta ou peça o cadastro.
- A Auto-Descoberta **não remove** clusters que sumiram da nuvem; ela só descobre e atualiza.

## Veja também

[Prometheus](ajuda:prometheus), [Nodes](ajuda:nodes), [HPAs](ajuda:hpas), [Node Pools](ajuda:nodepools).
