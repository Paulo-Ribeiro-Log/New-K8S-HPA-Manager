# Estudo: Forçar geração do ExternalSecret via Kyverno + Visualizador de Secret no AKV

**Status**: 🔬 estudo — mecanismo de força já **confirmado ao vivo contra clusters reais**
(`akspriv-entregamais-prd-admin`, `akspriv-abastecimento-hlg-admin`); nenhuma linha de código
da aplicação foi escrita ainda.

## Correção importante em relação à 1ª versão deste estudo

A 1ª versão assumia uma `ClusterPolicy` clássica do Kyverno (`spec.rules[].generate`,
JMESPath) — **errado**. Confirmado ao vivo: `generate-external-secret` é uma
**`GeneratingPolicy`** (`apiVersion: policies.kyverno.io/v1`), a nova API do Kyverno baseada
em **CEL** (Common Expression Language), instalada via Helm (`meta.helm.sh/release-name:
sre-tools`, chart `external-secrets-0.2.0`). O cluster tem as duas famílias de CRD instaladas
(`clusterpolicies.kyverno.io` legada e `*.policies.kyverno.io` nova), mas quem realmente gera
o `ExternalSecret` está na nova API — por isso `kubectl get clusterpolicy` retornava vazio.

## Cadeia completa, confirmada ao vivo, ponta a ponta

Três policies CEL trabalham juntas (todas com `policies.kyverno.io/part-of:
platform-signals`/`external-secrets`, confirmadas via `kubectl get
mutatingpolicies.policies.kyverno.io,generatingpolicies.policies.kyverno.io -o yaml`):

### 1. `detect-gcb-workloads` (MutatingPolicy) — detecção automática
- **Trigger real** (`spec.matchConstraints.resourceRules`, confirmado):
  `apps/v1 deployments|statefulsets|daemonsets` e `batch/v1 jobs|cronjobs`, nas operações
  `CREATE`/`UPDATE`/`DELETE`.
- Toda vez que um workload muda no namespace, recalcula (via `resource.List(...)`, a API CEL
  do Kyverno pra consultar outros recursos do cluster durante a avaliação) quantos workloads
  daquele namespace batem com a **mesma heurística já documentada nesta app como
  `isCompanyManagedDeployment`**: labels começando com `devops.k8s.io/`/`app.via.com.br/`,
  annotations começando com `artifact.spinnaker.io/`, ou imagem de container começando com
  `harbor`/`gcbregistry`.
- Escreve o resultado (a contagem) na annotation `devops.k8s.io/gcb-workloads` do **Namespace**
  (remove a annotation se a contagem cair a zero). `evaluation.mutateExisting.enabled: true` —
  também reconcilia workloads já existentes em background, não só eventos novos.
- Exclui ~30 namespaces de sistema via `namespaceSelector` (`argocd`, `cert-manager`, `dsv`,
  etc.).

### 2. `detect-gcb-workloads-bootstrap` (MutatingPolicy) — **o botão de força, já existe**
- **Trigger real, confirmado**: `v1 namespaces`, só `UPDATE`.
- `matchConditions` exige que o **Namespace** tenha uma label
  `devops.k8s.io/gcb-bootstrap` cujo valor comece com `"request-"` **e** tenha mudado em
  relação ao valor anterior (`oldObject` vs `object`) — ou seja, reaplicar o mesmo valor não
  dispara nada de novo, precisa ser um valor novo a cada chamada (ex.: timestamp).
- Quando dispara, roda a **mesma varredura** de `detect-gcb-workloads` (conta workloads pela
  mesma heurística) e regrava `devops.k8s.io/gcb-workloads` no mesmo Namespace, na mesma
  admissão.
- **Conclusão**: este é literalmente um mecanismo de "peça um recálculo agora" já projetado
  pelo time de plataforma para uso manual/self-service — só nunca documentado nem exposto em
  nenhuma ferramenta. Confirmado que nenhum namespace do cluster testado tem essa label
  setada hoje (nunca foi usado ainda, pelo menos nesse cluster).

### 3. `generate-external-secret` (GeneratingPolicy) — gera o ExternalSecret
- **Trigger real**: `v1 namespaces`, `CREATE`/`UPDATE`.
- **Gate único e exclusivamente binário**: `hasGcbWorkloads = has(annotations) &&
  "devops.k8s.io/gcb-workloads" in annotations` — só verifica se a **chave** existe, não o
  valor. Sem essa annotation (posta pelas duas policies acima), a expressão de geração
  (`hasGcbWorkloads ? generator.Apply(...) : true`) não faz nada.
- `spec.evaluation.generateExisting.enabled: true` + `synchronize.enabled: true` — além do
  evento de admissão, há reconciliação em background (então o problema tende a se
  autocorrigir "eventualmente"), e o `ExternalSecret` gerado é mantido em sync automaticamente
  se for deletado/alterado manualmente.
- Template do `ExternalSecret` gerado (por cluster, valores renderizados no Helm install —
  confirmado em `akspriv-abastecimento-hlg-admin`):
  - Nome: `akv-<vault>-<namespace>` (bate com a convenção já documentada em
    `discoverAkvExternalSecretName`, `secrets.go:756`).
  - `spec.dataFrom[0].find.name.regexp`: `(?i)<namespace-sem-hífen>$` — **esse é o filtro real
    de inclusão** (case-insensitive, só exige terminar com o token do namespace). Um segredo
    do Vault cujo nome não termina assim nunca é buscado.
  - `spec.dataFrom[0].rewrite[0].regexp`: um padrão **hardcoded por cluster** (ex.:
    `^AbastecimentoHlg-([^-]+)-([^-]+)-(.*)-abastecimentohlg$` → `$1-$2-$3`) — isto é só a
    **renomeação da chave** no `Secret` final; se um segredo bate no `find` mas não bate nesse
    `rewrite`, o comportamento padrão do `external-secrets` é manter o nome original do AKV
    como chave (não descartar o campo) — ou seja, esse regex mais estrito normalmente não é
    o motivo de um campo "sumir", só de aparecer com uma chave feia/inesperada. **Confirmar
    isso contra um caso real específico antes de assumir**, já que não testamos o
    comportamento de `rewrite` sem match ao vivo nesta rodada.

## Como forçar a geração agora, sem tocar na policy nem no ExternalSecret

```bash
kubectl label namespace <namespace> \
  devops.k8s.io/gcb-bootstrap="request-$(date +%s)" \
  --context <cluster> --overwrite
```

- Usa exatamente a convenção que a própria plataforma já expõe (`detect-gcb-workloads-bootstrap`).
- Nunca edita a `GeneratingPolicy`/`MutatingPolicy` nem o `ExternalSecret` — só toca uma label
  do próprio Namespace.
- Precisa de um valor **novo a cada execução** (o `$(date +%s)` resolve isso, mesmo padrão já
  usado pelo Resync AKV com `force-sync=<unix-ts>`).
- Efeito esperado: recalcula `devops.k8s.io/gcb-workloads` na hora; se o namespace de fato tem
  workload(s) batendo na heurística, o `ExternalSecret` deveria ser gerado/atualizado na
  sequência (mutação e geração correm na mesma cadeia de admissão do Kubernetes — mutating
  webhooks processam antes de generating, ordem padrão do admission control — mas isso **ainda
  não foi confirmado ao vivo ponta a ponta**, só a mecânica de cada policy isoladamente).

### O que essa força NÃO resolve
Se o workload do namespace genuinamente **não bate** com a heurística (`isCompanyManagedDeployment`
— sem label/annotation/imagem reconhecida), forçar o recálculo vai confirmar `0` de novo, e a
annotation nem chega a ser criada — o `ExternalSecret` continua não sendo gerado, corretamente.
Nesse caso o problema real está a montante (o Deployment não carrega nenhum dos sinais
esperados), não no gatilho de geração.

## Perguntas em aberto (a confirmar antes de expor isso na aplicação)

1. **Confirmar ao vivo, ponta a ponta**, contra um namespace onde o `ExternalSecret` está
   genuinamente ausente/desatualizado hoje: rodar o `kubectl label` acima e observar se
   `kubectl get externalsecret -n <namespace>` passa a listar o recurso dentro de segundos —
   ainda não testado (evitei mutar um Namespace real sem alinhamento prévio).
2. Confirmar o comportamento de `rewrite` sem match (mantém nome original vs. descarta o
   campo) — muda se vale a pena expor essa camada como diagnóstico na ferramenta.
3. Existe algum outro consumidor das labels `devops.k8s.io/gcb-bootstrap`/`gcb-workloads` além
   dessas 3 policies? (Ex.: FinOps, Health Check, ou qualquer outro lugar desta app que já
   confia nessa annotation para outra coisa.)

## Ferramenta proposta: "Forçar Geração" na aba Secrets

Mesmo padrão de UX do **Resync AKV** já existente (`ResyncAkvModal.tsx`,
`SecretHandler.ResyncAKV` em `secrets.go:815`) — botão novo ao lado, condição de exibição:
quando não existe nenhum `ExternalSecret` esperado no namespace, ou quando o usuário já sabe
que o campo não apareceu. Backend: novo endpoint `POST
/api/v1/secrets/:cluster/:namespace/force-external-secret-generation` fazendo `kubectl label
namespace <namespace> devops.k8s.io/gcb-bootstrap=request-<unix-ts> --context <cluster>
--overwrite`, mesmo formato de resposta (comando + saída + status) e mesmo
`RequireSREGroup()`/`HistoryTracker` do Resync AKV.

**Diferença de risco em relação ao Resync AKV**: aquele só toca uma annotation do
`ExternalSecret` (escopo estreito, um recurso específico); este toca uma **label do
Namespace inteiro** — o mesmo objeto que outras policies (incluindo o mecanismo de bypass do
Kyverno já documentado em `withKyvernoBypass`) também usam como alvo de label. Recomendação:
manter RBAC pelo menos tão restrito quanto Resync AKV, e confirmar a pergunta 3 acima antes de
implementar, para garantir que nenhum outro efeito colateral inesperado existe na label usada.

## Visualizador de Secret no AKV — ainda relevante, sem depender do item acima

Se, mesmo com a geração forçada, o campo específico continuar sem aparecer (porque o segredo
no Vault não bate no `find.name.regexp`), a necessidade original de "ver o valor cru no Vault"
continua de pé. Nada do investigado nesta rodada muda a análise já feita (nenhum client de AKV
existe hoje na aplicação; leitura direta via `az keyvault secret list/show` esbarra em
firewall por IP do Vault — já confirmado via log real — e em RBAC de leitura de Secrets no
Vault, nenhum dos dois garantido hoje). Ver a seção completa dessa análise no histórico deste
arquivo (git blame) ou reabrir sob pedido — mantida fora deste documento para não misturar as
duas frentes agora que a de "forçar geração" tem um caminho concreto e validável.

## Próximos passos

1. Escolher um namespace de teste (idealmente HLG, não PRD) onde o `ExternalSecret` esteja
   comprovadamente ausente ou desatualizado, e validar o comando de força ao vivo.
2. Confirmar a pergunta 3 (outros consumidores da label) antes de expor isso na UI.
3. Só depois, implementar o endpoint + botão "Forçar Geração".

**Nenhuma fase de código iniciada.**
