## Para que serve

Responde "o analista **X** tem acesso a **Y** no cluster **Z**?" sem rodar `kubectl auth can-i --as` à mão. A ferramenta **impersona** o e-mail do analista no Kubernetes (com os grupos dele no Entra ID) e mostra o que ele consegue fazer, no cluster e namespace escolhidos ou em toda a frota.

## Quando usar

- Atender um pedido de acesso ("o João consegue ver os pods do namespace `frete`?").
- Descobrir **por que** alguém não consegue acessar algo (falta de grupo, de RoleBinding).
- Saber se um analista tem **acesso elevado em algum cluster** da frota.
- Auditar **quais grupos** do Entra ID existem em um cluster e quem participa deles.

## Como usar (passo a passo)

1. Escolha o **cluster** e o **namespace**. Esta aba tem **os seus próprios seletores**, independentes do cluster global do aplicativo.
2. Digite o **e-mail do analista** e clique em **Verificar**. A ferramenta resolve os **grupos do Entra ID** dele (pelo `az`), impersona o e-mail junto com **todos** esses grupos e consulta o cluster.
3. Use as abas:

| Aba | O que mostra |
|---|---|
| **Visão Geral** | as regras que o analista tem no namespace, resumidas em **Tem acesso a / Sem acesso a** por categoria (Secrets, ConfigMaps, Pods, Logs, **Exec em Pods**, Services, Deployments, StatefulSets, DaemonSets, CronJobs, Ingress, HPA, Events), cada uma com leitura e escrita |
| **Verificação Pontual** | a pergunta direta: um **verbo** em um **recurso** (e API Group, opcional), por exemplo `get` em `secrets`, e o veredito permitido ou negado |
| **Todos os Grupos AAD** | todos os grupos do Entra ID do e-mail, com nome e Object ID |
| **Grupos do Cluster** | os grupos presentes no cluster: administradores do AKS e os grupos citados nas RoleBindings e ClusterRoleBindings, com os membros sob demanda. Também aponta bindings que dão permissão extra a **grupos abertos** (anônimo, `system:authenticated`) |
| **Todos os Clusters** | varre **toda a frota AKS** cadastrada e mostra, por cluster, o acesso de admin pela IAM e o acesso real por RBAC; sem namespace, lista os namespaces (fora de sistema) onde ele consegue manipular algo |
| **Histórico** | as consultas já feitas, com data, **quem consultou**, cluster, namespace, **e-mail analisado** e verbo e recurso |

## Permissões

Todas as rotas desta aba exigem **perfil SRE**. Além disso, a **identidade do servidor** precisa ter permissão de **impersonar** usuários e grupos no cluster alvo: sem ela a consulta falha com "cannot impersonate".

## Impacto

**Somente leitura** no cluster. Mas cada consulta **fica registrada no Change History** (`access_check`) com quem consultou e o e-mail analisado: é uma trilha de auditoria de quem pesquisou o acesso de quem.

## Cuidados e erros comuns

- **Acesso de administrador pela IAM é invisível ao RBAC.** Quem tem uma role como *Azure Kubernetes Service Cluster Admin* pode buscar o kubeconfig **admin** (certificado `system:masters`), que **ignora o RBAC do Kubernetes**. A Visão Geral e a Verificação Pontual **não mostram** isso; um banner avisa, e a aba **Todos os Clusters** mostra o acesso de admin pela IAM.
- **Sem os grupos, quase tudo dá "sem acesso".** O Kubernetes não herda os grupos do Entra ID de quem é impersonado só por e-mail. Se a resolução dos grupos falhar (por exemplo, permissão insuficiente), a consulta segue **sem grupos** e o resultado parece "sem acesso em lugar nenhum": **leia o aviso** de erro de grupos antes de concluir.
- **"Alcançável" não é "tem acesso".** Em Todos os Clusters, alcançar o cluster só quer dizer que o servidor consegue falar com ele; o que importa é o acesso real do analista.
- **A visão geral pode ser incompleta** em políticas RBAC complexas: a API de revisão de regras do Kubernetes pode devolver só um subconjunto. Para uma resposta definitiva, use a **Verificação Pontual**.
- Os grupos vêm do `az` do servidor, com um cache curto: uma mudança de grupo muito recente pode demorar a aparecer.
- Esta é uma ferramenta de **auditoria sensível**: consulte só quando houver motivo, porque fica no histórico.

## Veja também

[Permissões](ajuda:geral-abas-de-recurso#permissoes), [Namespaces](ajuda:namespaces).
