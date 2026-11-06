const e=`## Para que serve

Remove uma aplicação inteira de um namespace: descobre tudo o que pertence a ela (workloads, Services, Ingress, HPA, config, volumes, RBAC), mostra o que será apagado, remove na **ordem certa** e confere depois se sobrou alguma coisa.

## Quando usar

- Desativar uma aplicação e não deixar lixo para trás (Service, Ingress, ConfigMap, PVC...).
- Remover uma aplicação instalada por **Helm** (o release inteiro) ou por **Kustomize/kubectl apply**.
- Conferir o que uma aplicação realmente tem antes de decidir apagá-la: o **scan** só lê o cluster e pode ser usado sozinho.

> **Cuidado:** a remoção não pode ser desfeita. Use o **Simular (dry-run)** antes e leia os selos de aviso.

## Como usar (passo a passo)

1. Escolha o **cluster**, o **namespace** e a **aplicação**. A lista traz os workloads (Deployment, StatefulSet, DaemonSet, CronJob) e os **releases Helm** do namespace, com chart e status.
2. Clique em **Escanear dependências**. Os itens aparecem agrupados em **Etapa N**, na ordem em que serão removidos.
3. Revise a seleção. Ela já vem sugerida (veja abaixo); marque ou desmarque o que quiser.
4. Clique em **Simular (dry-run)** para testar sem apagar nada.
5. Clique em **Remover selecionados** e, no diálogo, **digite o nome da aplicação** para confirmar.
6. Acompanhe a barra de progresso e o resultado de cada item.
7. Ao terminar, a ferramenta **verifica sozinha** o namespace. Se sobrou algo, as sobras viram uma nova lista para uma nova rodada.

## Como o scan decide o que pertence à aplicação

| Origem | Como é detectada |
|---|---|
| **Helm** | annotation \`meta.helm.sh/release-name\`; ou label \`app.kubernetes.io/instance\` igual a um release existente; ou o nome informado ser o de um release |
| **Kustomize** | label \`app.kubernetes.io/managed-by=kustomize\` |
| **Kustomize (provável)** | annotation do \`kubectl apply\` mais labels de aplicação. É um palpite: o Kustomize não deixa marca no cluster |
| **Manual** | nenhuma das anteriores |

Sem Helm, o scan parte do workload e acrescenta: Services pelo \`selector\`, Ingress/HTTPRoute/VirtualService que apontam para eles, HPA, PDB, NetworkPolicy, ServiceMonitor/PodMonitor, ConfigMaps/Secrets/PVCs referenciados pelos pods, ServiceAccount, RoleBinding/Role e os PVCs criados por \`volumeClaimTemplates\`. Se um Secret é gerado por um **ExternalSecret**, ele entra também (senão o Secret voltaria sozinho).

## Seleção sugerida

- **Marcados:** workloads, Services, Ingress, HPA, PDB, NetworkPolicy, monitores, ConfigMaps, ServiceAccount e RBAC que só essa aplicação usa.
- **Desmarcados:** **Secrets, PVCs e ExternalSecrets** (guardam dados) e **qualquer item compartilhado** com outra aplicação.
- **Não selecionáveis:** itens "Removido pelo Helm". Eles saem junto com o release.

## O que significam os selos

| Selo | Significado |
|---|---|
| Compartilhado | outro workload do namespace também usa o item (a linha diz qual) |
| Dados | Secret ou PVC: apagar remove dados |
| Removido pelo Helm | o \`helm uninstall\` já apaga este item |
| Terminating | o item está sendo removido, mas um finalizer o segura (a linha mostra qual) |
| Recriado | existe de novo, com outro UID: alguém o recriou depois da remoção |
| Novo | apareceu depois do scan original |

## Ordem de remoção

A ordem existe porque o que sobra pode ser **recriado** por quem ainda está de pé:

1. Desinstalar o release Helm
2. Cortar o tráfego (Ingress, HTTPRoute, VirtualService)
3. Parar o autoscaling (HPA)
4. Parar os CronJobs
5. Remover os controllers (Deployment, StatefulSet, DaemonSet), **esperando os pods saírem**
6. Políticas e monitoramento (PDB, NetworkPolicy, ServiceMonitor, PodMonitor)
7. Services
8. Fontes externas de segredo (ExternalSecret)
9. ConfigMaps e Secrets
10. Volumes (PVC)
11. RBAC (RoleBinding, Role)
12. ServiceAccounts

## Permissões

- A ferramenta usa o \`kubectl\` e o \`helm\` com o contexto do cluster escolhido. É preciso estar na **VPN** quando o cluster for privado.
- O scan precisa de \`get\`/\`list\` nos recursos do namespace; a remoção precisa de \`delete\` neles, pelo **RBAC do cluster**.
- A rota de remoção é de escrita (perfil SRE no desenho da aplicação). Na prática, quem barra é o RBAC do cluster.

## Impacto

- **Apaga recursos do cluster.** O \`helm uninstall\` também remove **Secrets e PVCs que estiverem no manifesto do release**, mesmo desmarcados: o aviso aparece no diálogo.
- Cada item removido (ou que falhou) é gravado no **Change History**, só com identificação (tipo, nome, UID e origem), nunca com conteúdo.
- Só existe **uma execução por aplicação** ao mesmo tempo.

## Cuidados e erros comuns

- **A remoção para na primeira falha** e marca o resto como "não executado". Corrija a causa e escaneie de novo.
- **"O scan está desatualizado":** um recurso foi recriado depois do scan. Escaneie outra vez.
- **"Scan expirado":** o scan fica guardado por 2 horas e some se o servidor reiniciar. Escaneie de novo.
- **Item em Terminating:** um finalizer segura a remoção. A ferramenta mostra qual, mas não o retira.
- **Kyverno ou outra política de admissão** pode negar o \`delete\`: o erro aparece na linha do item.
- **Algo recria os itens** (operador, pipeline, outro controller): a ferramenta mostra "Recriado", mas não descobre quem recria.
- Pods soltos (sem controller) não são tratados.

## Veja também

[Deployments](ajuda:deployments) (rollback), [abas de recurso](ajuda:geral-abas-de-recurso); e as abas Helm e Events.
`;export{e as default};
