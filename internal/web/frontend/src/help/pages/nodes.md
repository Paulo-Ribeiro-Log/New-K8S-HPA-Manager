## Para que serve

Lista os **nodes** do cluster (as VMs que rodam os pods) com status, node pool, taints, versão e uso de CPU e memória, e permite **cordon, uncordon, drain, editar, abrir shell e remover** um node.

## Quando usar

- Ver quais nodes estão `NotReady` ou cordonados e o quanto cada um está ocupado.
- Tirar um node de serviço para manutenção (cordon e drain).
- Ver tudo o que roda em um node, de todos os namespaces.
- Ajustar labels ou taints de um node pelo YAML.

## Como usar (passo a passo)

1. Escolha o cluster. A lista mostra, por node: **NAME, STATUS, POOL, TAINTS, VERSION, PODS, CPU, MEM, %CPU, %MEM, CPU/A, MEM/A e AGE**. CPU e memória vêm do **Metrics Server**; **CPU/A** e **MEM/A** são o que o scheduler pode distribuir (allocatable).
2. Filtre pelos botões **Todos, Ready, NotReady e Cordoned**, escolha um **node pool**, ordene e use a busca (nome, pool, zona, IP, versão). A **visão geral** resume cada pool (nodes, ready, cordoned, CPU, memória, pods); clicar em um pool filtra a lista por ele.
3. **Clique em um node** para ver o que roda nele (pods de todos os namespaces), o YAML editável e o Describe.
4. O menu **⋮** de cada node reúne as ações. Para vários nodes, marque as caixas e use a barra de ações em lote.

## Ações

| Ação | O que faz |
|---|---|
| **Cordon** | marca o node como não agendável: pods novos não vão para ele, os atuais continuam |
| **Uncordon** | desfaz o cordon |
| **Drain…** | cordon e remoção dos pods do node, como `kubectl drain`. Em lote, **um node de cada vez**, e se um falhar os seguintes não são drenados |
| **Describe** | o equivalente a `kubectl describe node` |
| **Shell no node (--rm)** | abre um pod **privilegiado** no node, com o `/` do host em `/host` (como `kubectl debug node`), apagado ao sair |
| **Deletar node** | remove o objeto Node da API, depois de você **digitar o nome do node** |

### Opções do Drain

A confirmação mostra quantos pods serão afetados. As opções são:

| Opção | Efeito |
|---|---|
| **Ignorar DaemonSets** | pods de DaemonSet continuam no node |
| **Apagar dados emptyDir** | permite remover pods com `emptyDir`; os dados desses volumes se perdem |
| **Force** | remove também pods **sem controller**, que **não serão recriados** |
| **Ignorar PodDisruptionBudgets** | usa DELETE direto em vez da Eviction API e pode derrubar todas as réplicas de uma vez |
| **Grace period** e **Timeout** | tempo de desligamento dos pods e prazo total (padrão do campo: `5m`) |

Por padrão a remoção usa a Eviction API, que **respeita os PodDisruptionBudgets** (tenta de novo a cada 5 segundos até o timeout). Mirror e static pods são ignorados.

## Permissões

As ações aparecem desabilitadas, com **(sem permissão)**, quando o RBAC do cluster não permite: Cordon e Uncordon pedem *patch* em nodes, Drain pede também *evict* em pods, e Deletar pede *delete*. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster e pode derrubar aplicações.** Aplicar o YAML, Cordon, Uncordon, Drain e Deletar node ficam registrados no Change History.

## Cuidados e erros comuns

- **Drain derruba pods de verdade.** Pods com controller voltam em outros nodes, mas **só se houver capacidade**: confira o espaço nos demais antes.
- **Force e Ignorar PDB** são as duas opções perigosas (aparecem destacadas): a primeira apaga pods que não voltam, a segunda ignora a proteção de disponibilidade.
- **Deletar node não apaga a VM.** Ela continua existindo e **sendo cobrada** na cloud, e o kubelet pode registrar o node de novo. Os pods saem da API **sem respeitar PDBs**: faça Drain antes. Para remover a VM, reduza o node pool ou apague a instância pela cloud.
- **Cordon sozinho não tira pods.** Ele só impede novos; use Drain para esvaziar.
- O **Shell no node** é um pod privilegiado com acesso ao disco do host: use só quando necessário.
- Se **CPU e MEM** aparecem vazios, o Metrics Server não está respondendo no cluster.

## Veja também

[Pods](ajuda:pods), [Events](ajuda:events), [Namespaces](ajuda:namespaces).
