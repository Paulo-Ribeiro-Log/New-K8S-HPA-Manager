## Para que serve

Lista os **Pods** do cluster em uma tabela ao vivo e permite inspecionar, editar o YAML, abrir shell, acessar arquivos, abrir um port-forward e **reiniciar, matar ou excluir** pods.

## Quando usar

- Ver o estado dos pods de um namespace e quais estão com problema.
- Ler logs, o `describe` e o YAML de um pod.
- Entrar no container (shell), copiar arquivos ou abrir um túnel para uma porta.
- Reiniciar ou remover pods travados, um a um ou em lote.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace (ou todos). Namespaces de sistema ficam ocultos até você usar o botão do olho.
2. A lista da esquerda traz cada pod com a situação, `ready` e os **restarts** (acima de 3 ganha um selo laranja). A tabela da direita mostra as colunas NAME, KIND, READY, STATUS, REST., CPU, MEM, NODE e AGE, com filtro e ordenação nos cabeçalhos e o filtro **Top N** em CPU e MEM. A coluna **DT** mostra se o pod está monitorado no Dynatrace.
3. **Clique em um pod** para abrir os detalhes: métricas de CPU e memória em relação a requests e limits, o manifesto YAML e os botões de ação.
4. Para ler logs, use **Ver Logs**: escolha o container; a janela já abre com **atualização automática** ligada, para acompanhar.
5. **Describe** mostra o equivalente a `kubectl describe pod`.
6. O menu **⋮** reúne: **Abrir Shell** (no container, num container efêmero de debug ou num pod de troubleshooting próprio, que é apagado ao sair), **Port Forward**, **Arquivos (SFTP)**, **Buscar Config (.NET/Java/...)**, **Restart Pod**, **Kill Pod (Forçar)** e **Deletar Pod**.

## Editar o YAML do pod

O editor segue o fluxo **Validar (dry-run) → Diff → Aplicar**. Atenção: o Kubernetes **não deixa mudar a maioria dos campos de um pod já criado** (basicamente a imagem e poucos outros, como tolerations). Quando o cluster recusa, o aviso explica isso. Para mudar o resto, altere o Deployment/StatefulSet dono do pod.

## Restart, Kill e Deletar: qual usar

| Ação | O que faz |
|---|---|
| **Restart Pod** | apaga o pod **sem esperar o desligamento** e o controller (Deployment, StatefulSet...) cria outro. Se o pod **não tem dono**, ele **não volta**, e a aplicação avisa |
| **Kill Pod (Forçar)** | também sem esperar o desligamento (graça zero); use quando o pod não termina |
| **Deletar Pod** | apaga com o tempo normal de desligamento do pod |

Cada uma tem **versão em lote**: marque pods na lista e use **Reiniciar**, **Kill** ou **Deletar** no topo, com diálogo de confirmação e um resultado por pod.

## Permissões

Restart, Kill, Deletar e Aplicar só aparecem com permissão de escrita no namespace (RBAC do cluster). Sem ela, os botões ficam indisponíveis.

## Impacto

**Altera o cluster e pode derrubar serviço.** Aplicar, Restart, Kill, Deletar (inclusive em lote), download de arquivo, SFTP e port-forward ficam registrados no Change History. Ler logs, describe e YAML não é registrado.

## Cuidados e erros comuns

- **Restart e Kill derrubam o pod na hora:** se ele é a única réplica, há indisponibilidade até o novo ficar pronto.
- **Pod sem dono não é recriado.** Deletar ou reiniciar um pod avulso o apaga de vez.
- **Pod de StatefulSet** volta com o mesmo nome, mas o disco (PVC) permanece; confira o estado antes de matar.
- A lista usa um *watch* em tempo real quando consegue e cai para atualização periódica se ele falhar; se achar a lista parada, use o botão de atualizar.

## Veja também

[Containers](ajuda:containers), [Events](ajuda:events), [Deployments](ajuda:deployments).
