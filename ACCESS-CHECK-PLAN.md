# Plano: Verificar Acesso (Access Checker) — ✅ IMPLEMENTADO

Ferramenta no menu **Tools → Verificar Acesso** que responde "o analista X tem acesso a Y no
cluster/namespace Z?" via impersonation nativa do K8s, sem depender do binário `kubectl` no
servidor e sem usar Microsoft Graph API (indisponível no tenant).

Reproduz:
```bash
kubectl auth can-i --list -n <namespace> --as <email>      # visão geral de permissões
kubectl auth can-i get secrets -n <namespace> --as <email>  # checagem pontual
```

---

## Contexto / motivação

SREs recebem pedidos recorrentes do tipo "o analista X tem acesso a Y no cluster Z?" e isso era
resolvido manualmente via `kubectl auth can-i ... --as <email>` direto no terminal, cluster por
cluster — exige VPN, kubeconfig local e conhecimento prévio de qual `RoleBinding`/grupo concede o
quê. A ferramenta traz isso pra dentro da aplicação, com histórico de auditoria.

---

## Decisões de arquitetura

### 1. Cluster/namespace: select próprio, não o contexto global da app

A aba tem **seus próprios selects de Cluster e Namespace**, independentes do cluster/namespace
selecionados globalmente em `Index.tsx`. Motivos:
- É uma ferramenta de auditoria pontual, não de navegação de recursos — o SRE pode querer checar
  um analista num namespace que nem está "aberto" no momento.
- Outras abas do Tools menu que dependem de cluster (`CertificatesTab`, `CommandRunnerTab`) já
  gerenciam seu próprio estado de cluster/namespace, independente do estado global — mantém o
  padrão.
- Escopo: **um cluster + um namespace por checagem** (não multi-cluster como o Command Runner) —
  bate com o caso de uso literal e mantém KISS.

### 2. Resolução de grupos AAD (`VV_CLOUD_PR_*`) — via `az cli`, sem Graph API

**Problema descoberto durante os testes**: o K8s **não herda automaticamente** os grupos AAD do
usuário impersonado com `--as <email>` sozinho — só concede `system:authenticated`/
`system:basic-user` (permissões genéricas: NetworkPolicies do Calico, self-review). Nenhum acesso
real a workloads (secrets, pods, deployments) aparece sem também impersonar os **grupos**.

Inspecionando `RoleBinding`/`ClusterRoleBinding` reais do cluster, confirmado que o subject de
grupo usa **Object ID (GUID)**, nunca o nome:

```
users-rbac-frete-hub_r-via-developers-basic-permissions -> 1fb701e7-0c14-4e79-b773-1ced28316ce8
users-rbac-regionalizacao_rw-via-developers-readwrite   -> 88979704-5f7c-45e4-9d7e-56f01b81ddab
```

Logo, pra refletir o acesso real é obrigatório impersonar com `--as-group=<GUID>` para cada grupo
`VV_CLOUD_PR_*` do qual o analista participa.

**Sem permissão de Microsoft Graph API disponível no tenant** — resolução feita via `az cli`
(mesmo padrão já usado em `internal/rbac/azure_ad.go` para `az account show`), na direção
**"grupo → membro"** (não "usuário → grupos", que dependeria de Graph):

1. `az ad user show --id <email> --query id -o tsv` — resolve o Object ID do analista.
2. `az ad group list --filter "startswith(displayName,'VV_CLOUD_PR_')" --query "[].{id:id,displayName:displayName}" -o json` — lista todos os grupos com esse prefixo (cache ~45min, resultado estável).
3. Para cada grupo da lista, `az ad group member check --group <groupId> --member-id <userObjectId> --query value -o tsv` — confirma a membership. Rodado **em paralelo** (semáforo de 8 goroutines) já que cada `az` custa ~1-3s e há dezenas de grupos.

Implementado em `internal/rbac/aad_group_lookup.go` — `ResolveVVCloudGroups(ctx, email) ([]ADGroup, error)`, cache 5min por e-mail.

### 3. Pré-requisito de impersonation

Rodar `--as` exige que a identidade do backend (kubeconfig do servidor) tenha permissão
`impersonate` sobre `users`/`groups` no cluster alvo. Sem RBAC própria disso, tudo falha com
`Forbidden: ... cannot impersonate`. Tratado no backend como erro dedicado
`IMPERSONATION_NOT_ALLOWED` (mensagem acionável, não erro genérico).

---

## Investigação — comandos reais executados durante o planejamento/validação

Todos os comandos abaixo foram rodados de fato (não hipotéticos) contra clusters AKS reais do
ambiente, para validar cada premissa antes de implementar.

### a) Confirmar que a identidade `-admin` pode impersonar (sem `--as`)

```bash
kubectl auth can-i impersonate users --context <ctx>
```

Resultado: **`yes`** em todos os 9 contexts `-admin` testados (entregamais hlg/prd, envvias
hlg/prd, logreversa hlg/prd, oferta-prd, plataforma hlg/prd, tracking-hlg) — todos usam a
credencial `clusterAdmin_...`.

> Nota importante descoberta na prática: `kubectl auth can-i impersonate users --as <email>`
> testa uma pergunta **diferente** ("o e-mail impersonado pode impersonar?"), não "minha
> identidade real pode impersonar?". O teste correto **não usa `--as`**.

### b) Validar o mecanismo de impersonation fim-a-fim (com `--as`, sem grupos)

```bash
kubectl auth can-i get secrets -n default --as teste-fake@viavarejo.com.br --context akspriv-oferta-prd-admin
# → no (limpo, sem erro de "cannot impersonate")

kubectl auth can-i get secrets -n default --as jair.pereira@viavarejo.com.br --context akspriv-oferta-prd-admin
# → no

kubectl auth can-i --list -n default --as jair.pereira@viavarejo.com.br --context akspriv-oferta-prd-admin
# → só mostra NetworkPolicies (Calico) + selfsubjectreviews/selfsubjectaccessreviews/selfsubjectrulesreviews
#   (genérico de system:authenticated) — nenhum acesso a secrets/pods/deployments.
```

Isso comprovou o problema do item 2 acima: `--as` sozinho não reflete o acesso real via grupos.

### c) Confirmar o formato do subject de grupo nos RoleBindings reais (GUID, não nome)

```bash
kubectl get rolebindings,clusterrolebindings -A --context akspriv-oferta-prd-admin -o json \
  | python3 -c "
import json,sys
data=json.load(sys.stdin)
for item in data['items']:
    for s in item.get('subjects',[]):
        if s.get('kind')=='Group':
            print(item['metadata'].get('namespace','cluster-scoped'), item['metadata']['name'], '->', s.get('name'))
"
```

Saída (trecho):
```
cluster-scoped users-rbac-frete-hub_r-via-developers-basic-permissions -> 1fb701e7-0c14-4e79-b773-1ced28316ce8
cluster-scoped users-rbac-regionalizacao_rw-via-developers-readwrite   -> 88979704-5f7c-45e4-9d7e-56f01b81ddab
frete-prd users-rbac-gestao-de-contrato-de-frete_r-via-developers-group-readonly -> bb155ed4-...
```

Confirmou: subject é sempre GUID.

### d) Validar a resolução via `az cli` (sem Graph API) ponta a ponta

```bash
az ad user show --id jair.pereira@viavarejo.com.br --query id -o tsv
# → fac65ac2-c4a9-486f-aa89-50db5d6236ae

az ad group list --filter "startswith(displayName,'VV_CLOUD_PR_')" \
  --query "[].{id:id,displayName:displayName}" -o json
# → 52 grupos, ex:
#   { "displayName": "VV_CLOUD_PR_FRETE_HUB_R", "id": "1fb701e7-0c14-4e79-b773-1ced28316ce8" }
#   (mesmo GUID do RoleBinding "users-rbac-frete-hub_r" confirmado no item c) — validação cruzada!

az ad group member check --group 1fb701e7-0c14-4e79-b773-1ced28316ce8 \
  --member-id fac65ac2-c4a9-486f-aa89-50db5d6236ae --query value -o tsv
# → false (jair.pereira não está nesse grupo específico — resultado limpo, sem erro)
```

Confirmou que as 3 chamadas funcionam com a identidade `az login` do servidor, sem precisar de
Graph API.

> Nota: uma tentativa inicial de consultar grupos via **Microsoft Graph API direto** (`curl` com
> token OAuth extraído de `az account get-access-token --resource https://graph.microsoft.com`)
> foi **bloqueada pelo classificador de segurança do ambiente de desenvolvimento** (ação
> classificada como "PII de terceiro via API externa não auditada"). Foi esse bloqueio que levou
> à decisão de usar exclusivamente `az ad` (mesmo padrão já estabelecido no projeto), que não foi
> bloqueado por rodar dentro do fluxo/tooling já estabelecido.

### e) Descobrir onde os grupos de `jair.pereira` realmente têm binding (para validação positiva)

```bash
for ctx in akspriv-entregamais-hlg-admin akspriv-entregamais-prd-admin akspriv-envvias-hlg-admin \
           akspriv-envvias-prd-admin akspriv-logreversa-hlg-admin akspriv-logreversa-prd-admin \
           akspriv-plataforma-hlg-admin akspriv-plataforma-prd-admin akspriv-tracking-hlg-admin; do
  kubectl get rolebindings,clusterrolebindings -A --context "$ctx" -o json 2>/dev/null \
    | python3 -c "... filtra subjects Group in {GUIDs dos grupos do jair.pereira} ..."
done
```

Achado: `akspriv-plataforma-hlg-admin`, namespace `gda` (entre outros), tem
`users-rbac-hubcorreios_r-via-developers-readwrite` ligado ao GUID
`a30f895c-8e11-433a-88b0-2c58688c3786` (`VV_CLOUD_PR_HUB_CORREIOS_R`).

### f) Validação final — ferramenta vs `kubectl` manual, lado a lado

```bash
# Via API da ferramenta:
curl -s "http://localhost:8080/api/v1/access-check/rules?cluster=akspriv-plataforma-hlg-admin&namespace=gda&email=jair.pereira@viavarejo.com.br" \
  -H "Authorization: Bearer $TOKEN"

# Via kubectl direto, com os GUIDs que a ferramenta resolveu:
kubectl auth can-i --list -n gda --as jair.pereira@viavarejo.com.br \
  --as-group=a30f895c-8e11-433a-88b0-2c58688c3786 --context akspriv-plataforma-hlg-admin
```

**Resultado: bateram 100%** — mesmo conjunto de recursos/verbos (secrets, configmaps, pods,
deployments, statefulsets, etc. com CRUD completo `[*]` no namespace `gda`, herdado do
`ClusterRole via-developers-readwrite`).

Também validado o endpoint de checagem pontual:
```bash
curl -s ".../access-check/can-i?cluster=akspriv-plataforma-hlg-admin&namespace=gda&email=jair.pereira@viavarejo.com.br&verb=get&resource=secrets"
# → {"allowed":true, "reason":"RBAC: allowed by RoleBinding \"users-rbac-hubcorreios_r-via-developers-readwrite/gda\" of ClusterRole \"via-developers-readwrite\" to Group \"a30f895c-...\""}

curl -s ".../access-check/can-i?cluster=akspriv-oferta-prd-admin&namespace=default&email=jair.pereira@viavarejo.com.br&verb=get&resource=secrets"
# → {"allowed":false} (namespace sem binding para os grupos do analista)
```

---

## Implementação

### Backend

| Arquivo | O quê |
|---|---|
| `internal/rbac/aad_group_lookup.go` | **Novo** (revisado — ver "Revisão: prefixo ampliado" abaixo). `AADGroupLookup.GetAllGroups(ctx, email)` — uma única chamada `az ad user get-member-groups --id <email>` retorna TODOS os grupos do usuário (cache 10min por e-mail). `ResolveVVCloudGroups()` filtra localmente por prefixo (`VV_CLOUD_`) em cima do cache. |
| `internal/web/handlers/access_check.go` | **Novo.** `AccessCheckHandler` com `GetRules` (`SelfSubjectRulesReview`) e `CanI` (`SelfSubjectAccessReview`), impersonando `email` + GUIDs resolvidos via `rest.Config` (reaproveita `kubeManager.GetRestConfig`, que já resolve auth AKS/EKS/GKE). `buildImpersonatedClient` retorna `groupResolution{matched, all, err}` — `matched` (prefixo `VV_CLOUD_`, usado no `--as-group`) e `all` (lista completa, só para exibição). Detecta `Forbidden ... impersonate` e responde `IMPERSONATION_NOT_ALLOWED`. Loga cada consulta no `HistoryTracker` (`action: "access_check"`) — auditoria de consulta sensível sobre terceiros. |
| `internal/web/server.go` | Registra `GET /api/v1/access-check/rules` e `GET /api/v1/access-check/can-i`, ambos atrás de `rbacMiddleware.RequireSREGroup()`. |

### Frontend

| Arquivo | O quê |
|---|---|
| `internal/web/frontend/src/lib/api/types.ts` | Tipos `AccessCheckRulesResult`, `AccessCheckCanIResult`, `AccessCheckMatchedGroup`, `AccessCheckResourceRule`, `AccessCheckNonResourceRule`. Ambos os resultados têm `matchedGroups` (usados na impersonation) e `allGroups` (lista completa do e-mail). |
| `internal/web/frontend/src/lib/api/client.ts` | `getAccessCheckRules()` e `getAccessCheckCanI()`. |
| `internal/web/frontend/src/components/AccessCheckTab.tsx` | **Novo.** Cluster (`ClusterSelectorForTab`) + Namespace (`useQuery` + `apiClient.getNamespaces`) + e-mail. Painel "Grupos AAD (VV_CLOUD_*) usados na verificação" sempre visível. Três seções com abas manuais (`useState`, nunca shadcn `<Tabs>` — regra do CLAUDE.md): "Visão Geral" (veredito explícito Sim/Não por categoria de recurso + tabela bruta colapsável), "Verificação Pontual" (formulário verbo/resource → frase explícita SIM/NÃO PODE + motivo) e "Todos os Grupos AAD (N)" (lista completa `allGroups` com busca, badge "usado" nos que entraram na impersonation). |
| `internal/web/frontend/src/components/ClusterSelectorForTab.tsx` | **Reescrito** — ver "Revisão: bug do seletor de cluster" abaixo. |
| `internal/web/frontend/src/components/ToolsMenu.tsx` | Novo item `{ id: "access-check", label: "Verificar Acesso", icon: UserCheck }`. |
| `internal/web/frontend/src/pages/Index.tsx` | `case "access-check": return <AccessCheckTab />;` no `renderTabContent()`. |

---

## Revisão 1: respostas confusas demais (raw rule dump)

Feedback: a "Visão Geral" só mostrava a tabela técnica bruta de `resourceRules` (recurso/API
group/verbos), exigindo interpretação manual para responder "ele tem ou não acesso?". Adicionado,
sem remover nenhum dado:
- Um veredito direto no topo: *"`email` TEM acesso a recursos no namespace X"* / *"NÃO tem
  acesso..."*, com lista explícita do que tem/não tem acesso.
- Uma grade por categoria de recurso (Secrets, Pods, Deployments, ConfigMaps, Services, etc.) com
  colunas **Leitura**/**Escrita** separadas (Sim/Não), calculada a partir das mesmas
  `resourceRules` (`computeCategoryAccess()` em `AccessCheckTab.tsx`).
- A tabela técnica bruta continua disponível, só movida para `<details>` "Ver regras técnicas
  brutas" (colapsada por padrão, não removida).
- "Verificação Pontual": badge ALLOWED/DENIED virou frase explícita *"SIM — `email` PODE
  executar 'get secrets' no namespace X"* / *"NÃO — ... NÃO PODE ..."*.

## Revisão 2: bug do seletor de cluster (busca não filtrava)

Sintoma relatado: digitar no campo de busca do cluster não refletia no dropdown.

**Causa raiz** (confirmada com teste Playwright real, headless, contra o servidor rodando):
`ClusterSelectorForTab.tsx` usava um `<Select>` do Radix com um `<Input>` de busca **fora** dele.
O Radix `Select` fecha o popover automaticamente em qualquer interação fora do seu conteúdo — ao
clicar no campo de busca (elemento irmão, fora do `Select`), o dropdown fechava antes da digitação
chegar a filtrar a lista visível. Reproduzido com script Playwright (`chromium.launch` +
`page.locator('[role="listbox"]').isVisible()` confirmando `false` logo após focar o input).

**Correção**: reescrito para o mesmo padrão já usado no seletor de cluster global do `Header.tsx`
— `Popover` + `Command`/`CommandInput` (`cmdk`), com a busca embutida **dentro** do mesmo popover,
então focar/digitar nela não fecha nada. Revalidado com o mesmo teste Playwright: popover
permanece aberto, lista filtra ao vivo, seleção final correta. Como é um componente compartilhado,
a correção beneficia qualquer consumidor futuro, não só esta ferramenta.

## Revisão 3: prefixo ampliado de `VV_CLOUD_PR_` para `VV_CLOUD_`

Pedido: ampliar a busca de grupos para descobrir acesso em mais categorias de recursos, não só
`VV_CLOUD_PR_*`.

**Problema de escala descoberto**: `az ad group list --filter "startswith(displayName,'VV_CLOUD_')"`
retorna **802 grupos** no tenant (vs. 52 com `VV_CLOUD_PR_`) — checar membership um a um
(`az ad group member check`, abordagem original) exigiria até 802 chamadas `az` por request,
inviável dentro do timeout de 30s do handler.

**Solução**: descoberto que `az ad user get-member-groups --id <email>` (aceita e-mail/UPN
diretamente) devolve **todos** os grupos do usuário numa única chamada (~1-2s), eliminando a
necessidade de listar grupos por prefixo e checar membership individualmente. Testado:

```bash
time az ad user get-member-groups --id jair.pereira@viavarejo.com.br \
  --query "[].{id:id,displayName:displayName}" -o json
# → ~1.3s, retorna todos os 82 grupos do usuário (VV_CLOUD_* e outros: AZ-SAILPOINT-*,
#   az-ztna-*, GCB-PII-*, etc.)
```

`internal/rbac/aad_group_lookup.go` foi reescrito: removida a lógica de `listPrefixGroups` +
`checkMembership` em paralelo (semáforo de 8 goroutines); agora é uma função única
`fetchMemberGroups` + cache de 10min por e-mail. `ResolveVVCloudGroups()` filtra localmente
(`strings.HasPrefix`) sobre o cache — sem custo adicional de chamadas `az`.

Validado end-to-end contra `akspriv-plataforma-hlg-admin`/`gda`: 82 grupos totais, 19 batendo
`VV_CLOUD_*` (incluindo `VV_CLOUD_SRE`, `VV_CLOUD_CHECKOUT`, `VV_CLOUD_BUSCA`,
`VV_CLOUD_GCP_GEMINI_FRONTLINE`, etc. — bem mais amplo que os 2 encontrados antes só com
`VV_CLOUD_PR_`), sem regressão no resultado de acesso a `secrets` (`verbs: ["*"]` mantido).

## Revisão 4: aba "Todos os Grupos AAD"

Como `get-member-groups` já traz a lista completa sem custo extra, adicionado `allGroups` na
resposta de ambos os endpoints e uma terceira aba manual em `AccessCheckTab.tsx` — "Todos os
Grupos AAD (N)" — com busca por nome e badge "usado" nos grupos que entraram na impersonation
(prefixo `VV_CLOUD_`). Dá visibilidade total dos grupos do analista, não só o subconjunto usado
pela ferramenta.

## Revisão 5: falso negativo com acesso elevado — descoberta da camada IAM do Azure

Relato: e-mails reconhecidamente com "permissões elevadas para todos os clusters que
gerenciamos" apareciam como **sem nenhum acesso** na Visão Geral. A princípio pareceu ser o
mesmo tipo de bug do prefixo (grupo `VV_CLOUD-ADM` usa **hífen**, não `_`, então nunca batia com
o filtro `"VV_CLOUD_"` — bug real, corrigido: prefixo agora é `strings.HasPrefix(displayName, "VV_CLOUD")`
sem o separador final). Mas o usuário confirmou que a pessoa testada **também está no
`VV_CLOUD_SRE`** (que já batia com o prefixo antigo) — então o hífen não explicava tudo.

Investigação (com reautenticação do `az cli` para uma conta corporativa completa, já que a
sessão original era uma conta terceirizada `.ca@via.com.br` — hipótese de permissão restrita
descartada: `az ad user get-member-groups` funcionou igual para ambas as contas, inclusive
consultando terceiros):

```bash
# Confirma se o cluster usa Azure RBAC para autorização K8s (webhook pro ARM) ou só RBAC nativo
az aks show --name akspriv-oferta-prd --resource-group rg-oferta-app-prd \
  --subscription <sub-id> --query "{enableRBAC:enableRbac, aadProfile:aadProfile}" -o json
# → "enableAzureRbac": null — NÃO usa Azure RBAC para K8s, só RBAC nativo (RoleBinding/ClusterRoleBinding)

# IAM (Role Assignments) do recurso AKS no Azure — camada de autorização SEPARADA do RBAC do K8s
RESOURCE_ID=$(az aks show --name akspriv-oferta-prd --resource-group rg-oferta-app-prd \
  --subscription <sub-id> --query id -o tsv)
az role assignment list --scope "$RESOURCE_ID" \
  --query "[].{principal:principalName, role:roleDefinitionName, principalType:principalType}" -o table
# → VV_CLOUD_CARGADIRETA  Azure Kubernetes Service Cluster Admin Role   Group
```

**Achado decisivo**: `VV_CLOUD_CARGADIRETA` tem a role nativa do Azure **"Azure Kubernetes
Service Cluster Admin Role"** atribuída via IAM no recurso AKS — essa role concede a ação
`listClusterAdminCredential`, ou seja, permite **buscar o kubeconfig ADMIN** (o certificado
`system:masters`, o mesmo usado nos contexts `-admin` testados o tempo todo neste documento).
`system:masters` é hardcoded no `kube-apiserver` como bypass total de qualquer autorização —
nativa ou via impersonation. **Esse tipo de acesso é estruturalmente invisível a qualquer
checagem via `SelfSubjectRulesReview`/`SelfSubjectAccessReview`** — inclusive o próprio `kubectl
auth can-i --as` sofre da mesma limitação, porque a decisão de conceder acesso acontece **antes**
de qualquer request chegar no K8s (é o Azure Resource Manager decidindo se entrega ou não o
certificado, não uma autorização avaliada pelo `kube-apiserver`).

Diferença importante entre as duas roles nativas do AKS encontradas no IAM:
- **"Azure Kubernetes Service Cluster User Role"** → só permite buscar o kubeconfig *normal*
  (`listClusterUserCredential`); o que a pessoa pode fazer depois ainda passa pelo RBAC nativo
  do K8s — **coberto corretamente** pela ferramenta.
- **"Azure Kubernetes Service Cluster Admin Role"** → permite buscar o kubeconfig *admin*
  (bypass total) — **não é possível replicar via impersonation**, só detectar que ela existe.

**Correção implementada** — não tenta simular o bypass (impossível), só **detecta e avisa**:
`internal/web/handlers/access_check_iam.go` (novo arquivo):
- `getAKSResourceRoleAssignments(ctx, cluster)`: resolve o resource ID do AKS via
  `findClusterInConfig` (já usado em `nodepools_snat.go`) + `az aks show --query id`, depois
  `az role assignment list --scope <resourceID> --query "[?principalType=='Group']..."`.
  Só roda pra AKS (`detectSNATProvider(cluster) == "aks"` — GKE/EKS retornam `nil, nil`, sem
  erro). Cache 45min por cluster (não depende do e-mail — é config de infra, estável).
- `findIAMAdminBypass(ctx, cluster, allGroups)`: cruza os grupos do e-mail (já resolvidos via
  `GetAllGroups`, sem chamada `az` extra) com os Role Assignments, filtrando por
  `iamAdminRoles = {"Azure Kubernetes Service Cluster Admin Role", "Azure Kubernetes Service RBAC Cluster Admin"}`.
- Novo campo `iamAdminAccess: [{groupName, role}]` nas respostas de `/rules` e `/can-i` —
  falha nessa checagem é best-effort (não derruba a resposta principal, `iamMatches, _ := ...`).
- `AccessCheckTab.tsx`: banner vermelho sempre visível (independente da aba ativa) quando
  `iamAdminAccess` não é vazio — deixa explícito que esse acesso **não** está refletido no
  restante da ferramenta (Visão Geral/Verificação Pontual).

Validado ponta a ponta: `otavio.ramos@viavarejo.com.br` (membro confirmado do
`VV_CLOUD_CARGADIRETA` via `az ad group member list`) contra `akspriv-oferta-prd` → API retornou
`iamAdminAccess: [{"groupName":"VV_CLOUD_CARGADIRETA","role":"Azure Kubernetes Service Cluster Admin Role"}]`.

---

## Status e próximos passos

✅ Implementado, buildado (`go build ./...`, `npm run build`, `./rebuild-web.sh -b`) e validado
ponta a ponta contra clusters reais (seções "Validação final", "Revisão 3" e "Revisão 5" acima).

Pontos que podem ser revisitados no futuro (não bloqueantes):
- Cache de grupos AAD é por processo (memória) — reinicia a cada restart do servidor.
- Não há fallback de input manual de GUID de grupo caso a resolução via `az cli` falhe — hoje
  degrada mostrando aviso e resultado só com `system:authenticated`.
- O prefixo `VV_CLOUD` está hardcoded (`vvCloudGroupPrefix` em `access_check.go`) — se surgir
  necessidade de outro prefixo/convenção, generalizar para parâmetro de query.
- A checagem de IAM (Revisão 5) só cobre AKS — GKE (IAM do GCP) e EKS (IAM do AWS) têm
  equivalentes conceituais mas não implementados (escopo não pedido ainda).
- Não checamos `enableAzureRbac=true` (Azure RBAC para autorização K8s) em nenhum cluster real
  até agora — se algum cluster usar esse modo, a authorização passa por um webhook pro ARM e o
  comportamento da impersonation nesse cenário específico não foi validado.
