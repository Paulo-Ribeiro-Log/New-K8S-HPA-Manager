# Limpeza do WAL do Prometheus

Ferramenta para o caso em que o Prometheus gerenciado pelo prometheus-operator **não sobe** (tipicamente
`OOMKilled` durante o replay do WAL). Ela analisa antes de sugerir qualquer ação destrutiva e só então
oferece a limpeza de `wal/` e `chunks_head/`.

## Status

| Parte | Situação |
|---|---|
| `scripts/prometheus-wal-cleanup.sh` (fallback em bash) | **Existe** e funciona (ver [Fallback](#fallback-script-bash)) |
| Análise em Go (`internal/promwal`) + rota `GET /prometheus/wal/analyze` | **Pronto** (fase 1, com testes; ainda não validado em cluster real) |
| Painel "Diagnóstico do WAL" na aba Prometheus (`PrometheusWalPanel.tsx`) | **Pronto** (fase 2; type-check e lint limpos, ainda não testado no browser) |
| Execução da limpeza em Go, com progresso (SSE), cancelamento e restauração | **Pronto no backend** (fase 3, testado com clientset/kubectl fakes; **não** validado em cluster real). Sem tela ainda: a UI é a fase 4 |
| Diálogo de limpeza (`PrometheusWalCleanupDialog.tsx`), botão de restauração, Change History, CHANGELOG | **Pronto** (fase 4; type-check e lint limpos, **não testado no browser nem em cluster real**) |
| Contador de reincidência (anotação persistente + Change History) e aviso de problema crônico | **Pronto** (fase 5; testado com fakes e `history.db` real em SQLite temporário; não validado em cluster real nem no browser) |
| Verificação de integridade do Prometheus (rota `GET /prometheus/wal/health`) | **Pronto** (fase 6; testado com fakes; não validado em cluster real) |
| Alertas de integridade e reincidência no app (`usePrometheusHealth`, `PrometheusHealthAlerts`, contagem no `CriticalAlertsBanner`) | **Pronto** (fase 7; type-check, lint e build limpos; **não testado no browser nem em cluster real**) |

Este documento descreve o desenho e o uso pretendidos. Atualize a tabela acima conforme as fases entrarem.

## Por que analisar antes

Apagar o WAL **perde todas as métricas que ainda não viraram bloco compactado** (os blocos `01…` são
preservados). Antes disso há alternativas menos destrutivas, e nem todo Prometheus fora do ar tem problema de WAL.
A análise devolve um veredito em escada, do menos para o mais destrutivo:

| Veredito | Quando | O que a ferramenta faz |
|---|---|---|
| `saudavel` | Pod Ready, sem restarts relevantes | Nada |
| `aumentar_memoria` | OOM com limit baixo em relação ao WAL | **Apenas informa** o valor sugerido e aponta para o editor de recursos da própria aba (rota `PUT /prometheus/:cluster/:namespace/:type/:name`, já existente). Não altera nada |
| `limpar_wal` | OOM mesmo com memória razoável, WAL corrompido, ou limit já no teto | Habilita o botão "Limpar WAL…" |
| `investigar` | A causa não é o WAL (imagem, PVC cheio, permissão…) | Mostra só o diagnóstico |

## Como a análise funciona

Somente leitura. Nada é desligado ou apagado.

### Análise básica (sempre)

Não monta volume e não cria pod. Coleta:

- **Recurso Prometheus (CRD)** via `kubectl get -o json` (o projeto não usa dynamic client): réplicas, retenção,
  limits/requests de memória, tamanho do PVC.
- **Pods `prometheus-<nome>-N`**: `restartCount`, `lastState.terminated` (`OOMKilled`, exit 137), nó, tempo em CrashLoop.
- **Logs do container anterior** (`--previous`, com timeout e limite de linhas):
  - `replaying WAL` / `WAL segment loaded` → progresso do replay antes de morrer;
  - `corruption` / `unexpected end of segment` → **corrupção** (causa diferente de OOM).
- **Eventos** do pod e do PVC.
- **Quem gerencia o recurso**: labels de Helm e Argo CD. Com sync automático, o `replicas: 0` seria revertido — vira aviso.
- **Réplicas**: com mais de uma (par HA), a outra réplica pode estar saudável e ter os dados — vira aviso.

### Análise profunda (sob demanda)

Só roda quando o usuário marca explicitamente a opção (`deep=true`), porque **cria um pod no cluster**:

- Monta o PVC em um pod temporário **read-only**, fixado no `nodeName` do pod atual (evita falha de RWO em nó
  diferente e dispensa desligar o Prometheus).
- Lê os `meta.json` dos blocos, o tamanho de `wal/` e de `chunks_head/`.
- Calcula o **intervalo de dados que seria perdido**: do `maxTime` do último bloco até agora.
- O pod é removido ao final, mesmo em erro.

Sem a análise profunda, o veredito `limpar_wal` ainda pode aparecer, mas a janela de dados perdidos fica como
"desconhecida" e o diálogo de limpeza pede para rodá-la antes de continuar.

## Como usar (interface)

1. Abra a aba **Prometheus**, selecione o cluster e o **StatefulSet** do Prometheus (o painel só aparece para StatefulSets).
2. Clique em **Analisar WAL**. Marque **Análise profunda** para ver a janela de dados perdidos — ela é **obrigatória** para liberar a limpeza.
3. Leia o veredito, as evidências e os avisos.
   - `aumentar_memoria`: ajuste o limit no editor de recursos da própria aba (a ferramenta só informa o valor).
   - `limpar_wal`: o botão **Limpar WAL…** só é habilitado quando todas as réplicas afetadas têm análise profunda.
   - Se uma limpeza anterior foi interrompida, o painel mostra **Restaurar** (religa o Prometheus com as réplicas originais).
4. No diálogo de limpeza:
   - confira o intervalo que será perdido e escolha as réplicas a limpar (dá para limpar só a com problema, mas **todas** ficam desligadas durante a operação);
   - digite `APAGAR` (e o nome do recurso) para confirmar;
   - acompanhe o progresso: anotação de restauração → desligar → aguardar pods → pod temporário → apagar →
     remover pod → religar → aguardar Ready.
5. O diálogo não fecha enquanto a limpeza roda. **Cancelar** remove o pod temporário e religa o Prometheus. Ao terminar, a análise é descartada: rode-a de novo para conferir.

> Se o recurso é gerenciado por Helm/GitOps, **pause o sync antes** de limpar.

## Reincidência: alerta de problema crônico (fase 5)

Limpar o WAL trata o sintoma. Se precisa ser repetido, a causa é recorrente (cardinalidade crescendo, memory
limit baixo, disco cheio, retenção alta). A ferramenta passa a contar as limpezas e a avisar.

**O que conta:** só limpezas em que algo foi de fato apagado (`cleaned` não vazio) — inclusive uma limpeza
parcial (apagou o primeiro PVC e falhou no segundo). Limpezas que falharam antes de apagar, foram canceladas antes
disso ou recusadas pelos guard-rails não entram.

**Duas fontes, combinadas:**

| Fonte | O que guarda | Vê limpezas de outras pessoas? | Limitação |
|---|---|---|---|
| Anotações **persistentes** no recurso Prometheus, gravadas no **mesmo patch** que religa o Prometheus ao fim de uma limpeza bem-sucedida: `hpa-manager/wal-cleanup-count` (total acumulado), `hpa-manager/wal-cleanup-last-at` (RFC3339) e `hpa-manager/wal-cleanup-history` (as últimas ~10 datas) | Fato do cluster | Sim | Zera se o recurso Prometheus for recriado (redeploy completo). Se o religar falhar, a anotação não é gravada |
| Change History (`prometheus_wal_cleanup` com `cleaned` não vazio, mesmo cluster e `namespace/nome` exato, últimos 30 dias) | Auditoria, já gravada hoje | Não: `history.db` é local de cada máquina | Conta só o que foi feito a partir desta instalação |

A contagem de 30 dias é a **união das duas fontes, deduplicada por proximidade de horário** (a mesma limpeza
aparece nas duas). A anotação `…-original-replicas` descrita em "Segurança" é outra coisa: existe só durante a
limpeza e é removida ao religar; não serve de contador.

**Limiar (constante ajustável):** **2 ou mais limpezas em 30 dias** = reincidente.

**Onde aparece:**
- relatório da análise: campo `recurrence` (`count_30d`, `total`, `last_at`, `dates`, `recurrent`) e um aviso do tipo
  "Este Prometheus já teve o WAL limpo N vezes em 30 dias (última há X): limpar de novo trata o sintoma; investigue
  cardinalidade, memory limit e disco";
- painel e diálogo de limpeza: o aviso aparece em destaque. **Não bloqueia** a limpeza;
- sistema de alertas (fase 7), como `warning`.

**Implementação:** `internal/promwal/recurrence.go` (`BuildRecurrence`, anotações, aviso) e
`PrometheusWALHandler.cleanupHistoryDates` (consulta o Change History; o pacote `promwal` não depende do
`history`). A anotação só é gravada se o religar der certo (mesmo patch). O histórico guarda as últimas 10 datas
na anotação; o contador acumulado não tem limite. A análise sempre devolve `recurrence` no relatório.

## Integridade ao ativar o cluster e alertas (fases 6 e 7)

**Motivo:** o sistema de alertas atual é alimentado pelo Prometheus do cluster (integração com o Alertmanager).
Com o Prometheus fora do ar, ele não consegue avisar que está fora do ar. A verificação abaixo é independente:
usa só a API do Kubernetes.

**Quando roda:** assim que o contexto do cluster é **selecionado e ativo** no frontend, e de novo a cada troca de
cluster. Enquanto o cluster continua ativo, repete em intervalo longo (cerca de 60 s). **Clusters inativos nunca
são consultados.** Sem alcance ao cluster (VPN fora, por exemplo), o resultado é "não verificável", **nunca** um
alerta falso.

**Rota (fase 6, pronta):** `GET /api/v1/prometheus/wal/health?cluster=X[&refresh=true]`, leitura, com cache de 45 s por
cluster (como o `prometheus-status` do FinOps) e timeout de 20 s. Não cria pod, não faz análise profunda, não consulta `az`/`aws`/`gcloud`.
Descobre os recursos Prometheus do cluster e devolve, por recurso:

| Estado | Condição | Severidade |
|---|---|---|
| `desligado_por_limpeza` | anotação `…-original-replicas` presente (limpeza interrompida) | `critical` |
| `fora_do_ar` | algum pod sem Ready com restarts, OOMKilled, CrashLoop/ImagePull, ou Pending/não Ready há mais de 5 min; ou **nenhum pod** com `replicas` > 0 | `critical` |
| `reincidente` | 2 ou mais limpezas em 30 dias (contador da fase 5) | `warning` |
| `iniciando` | pod novo (menos de 5 min), sem restarts e sem erro: ainda subindo (o replay do WAL demora) | nenhuma |
| `desligado` | `replicas` 0 sem anotação de limpeza (alguém desligou) | nenhuma |
| `saudavel` | todos os pods Ready e sem reincidência | nenhuma |
| `nao_verificavel` | sem alcance ao cluster ou sem permissão | nenhuma |

Prioridade quando mais de uma condição vale: limpeza interrompida > fora do ar > reincidente > iniciando >
desligado > saudável. Cada item traz `recurrence` e, na limpeza interrompida, `pending_restore_replicas`.

**Resposta:** `{cluster, checked_at, verifiable, reason?, operator_present, items[]}`. Falha de alcance ou de
permissão **não é erro HTTP**: volta 200 com `verifiable: false` e o motivo, e nenhum item (nunca vira alerta).
Cluster sem o CRD do prometheus-operator volta `verifiable: true`, `operator_present: false` e nenhum item.

**Custo:** uma chamada `kubectl get prometheus --all-namespaces -o json` e uma listagem de pods por recurso
Prometheus encontrado; sem logs, sem eventos, sem pod temporário.

**Itens no sistema de alertas (fase 7, pronta):**

- **Disparo:** o hook `usePrometheusHealth` (`hooks/usePrometheusHealth.ts`) consulta a rota assim que o cluster é
  selecionado (`selectedCluster` do `Index.tsx`), na troca de cluster e a cada 60 s enquanto ele continua ativo. A
  consulta de um cluster que deixou de ser o ativo para de se repetir. Vale em qualquer aba (não só no Dashboard) e
  pausa com a aba do navegador em segundo plano (padrão do React Query).
- **Lista de alertas** (`PrometheusHealthAlerts.tsx`, logo abaixo dos cards, em qualquer aba): um item por recurso
  Prometheus com `critical` ou `warning`, com o resumo e o botão **Abrir diagnóstico do WAL** (leva à aba
  Prometheus). Críticos (`fora_do_ar`, `desligado_por_limpeza`) não podem ser dispensados; o aviso de reincidência
  pode ser dispensado **na sessão**. Cada problema novo também gera um toast, uma vez por sessão (não a cada consulta).
- **Card "Alertas Críticos"** (`CriticalAlertsBanner`, Dashboard): o número passa a somar os críticos do Prometheus
  e o subtítulo ganha "• Prometheus". Se o único crítico for o do Prometheus, o clique leva à aba Prometheus; se
  houver crítico do Alertmanager, o clique continua abrindo a página de alertas como antes.
- Estados `nao_verificavel`, `iniciando`, `desligado` e `saudavel`, e cluster sem prometheus-operator, **nunca**
  geram alerta. Os alertas existentes (HPA/Node Pool, Alertmanager) não foram alterados: o Prometheus é uma fonte
  à parte, lida pelo próprio hook.

## Segurança

- **Permissão:** só executa quem passa por `RequireSREGroup()` **e** tem permissão real no cluster. Antes de
  desligar qualquer coisa, a limpeza verifica via `SelfSubjectAccessReview` (patch em `prometheuses`, list/create/
  delete de `pods` e `pods/exec`) e recusa se faltar alguma, para não desligar o Prometheus e só então descobrir
  que não dá para criar o pod. A análise básica é leitura e não exige o grupo SRE. A análise profunda cria um
  pod temporário: depende do RBAC do cluster (sem permissão, a falha vira aviso no relatório) e é registrada
  no Change History (`prometheus_wal_analyze_deep`).
- **Guard-rails no backend** (a UI não é a única barreira) — `POST /cleanup` recusa quando:
  - o `analysis_id` não existe ou passou de 10 min (410), ou já foi usado: **cada análise autoriza uma única limpeza**;
  - a confirmação não é `APAGAR` **e** o nome do recurso Prometheus (400);
  - o veredito não é `limpar_wal` (409);
  - a réplica pedida não tem indicação de limpeza na análise, ou **não passou pela análise profunda** — é ela que
    mostra os dados que serão perdidos (409);
  - há uma limpeza anterior interrompida pendente (409), ou outra limpeza em andamento no mesmo Prometheus (409);
  - `force: true` dispensa o veredito e a análise profunda (decisão explícita), **nunca a confirmação digitada**.
- **Religar garantido:** antes de zerar as réplicas, a ferramenta grava no recurso Prometheus a anotação
  `hpa-manager/wal-cleanup-original-replicas`. Um `defer` religa **sempre** (erro, cancelamento, timeout),
  usando um contexto próprio, e remove a anotação. Se o processo do servidor morrer no meio, a anotação
  permanece: a próxima análise detecta (veredito `investigar`, campo `pending_restore_replicas`) e
  `POST /prometheus/wal/restore` religa. *Desvio do plano original:* não há varredura de todos os clusters na
  inicialização do servidor (exigiria acessar dezenas de clusters, com VPN), a detecção é sob demanda.
- **Todas as réplicas ficam desligadas durante a limpeza**, mesmo que só uma seja limpa: o StatefulSet é do
  operator, que só aceita `replicas` no recurso Prometheus. A réplica saudável de um par HA fica fora do ar por
  alguns minutos.
- **Change History:** registra recurso, PVC, tamanhos e intervalo perdido. Nunca conteúdo dos dados.
- Chamadas a `kubectl` sempre com `exec.CommandContext` + timeout.

## API

Arquivo próprio de handler (`handlers/prometheus_wal.go`), vizinho de `/prometheus`:

| Rota | Descrição |
|---|---|
| `GET /api/v1/prometheus/wal/analyze?cluster&namespace[&name][&deep=true]` | Análise; devolve veredito, evidências, avisos e `analysis_id` (válido por 10 min, guardado em memória). `name` vazio = único recurso Prometheus do namespace |
| `GET /api/v1/prometheus/wal/health?cluster[&refresh=true]` | Integridade leve dos recursos Prometheus do cluster (ver "Integridade ao ativar o cluster e alertas"); cache de 45 s |
| `POST /api/v1/prometheus/wal/cleanup` (SRE) | Corpo: `{analysis_id, replicas?, confirm, confirm_name, force?}`. Valida (ver guard-rails), responde **202** com `operation_id` e executa em segundo plano |
| `GET /api/v1/prometheus/wal/progress/:id` | Progresso via SSE (`WebSocketJWTAuthMiddleware`, token em `?token=`). Eventos `wal-cleanup` e, no fim, `complete` ou `error`, com o resultado (`cleaned`, `restored`, `ready`, `warnings`) em `result`. Quem conecta depois recebe os eventos anteriores (replay) |
| `POST /api/v1/prometheus/wal/cancel/:id` (SRE) | Cancela; a limpeza em curso religa o Prometheus antes de terminar |
| `POST /api/v1/prometheus/wal/restore` (SRE) | Corpo: `{cluster, namespace, name}`. Religa um Prometheus deixado desligado por limpeza interrompida |

Passos da execução (`internal/promwal/cleanup.go`): permissões → anotação de restauração → `replicas: 0` →
aguardar os pods saírem → por PVC: pod temporário (herda `subPath`, `securityContext`, tolerations e
`nodeSelector` do StatefulSet) → ler blocos/tamanhos → `rm -rf wal/* chunks_head/* lock` → conferir que ficou vazio →
remover o pod → religar com as réplicas originais → aguardar Ready. A imagem do pod temporário é `busybox:1.36`: o
nó precisa conseguir baixá-la.

## Estrutura de código

- `internal/promwal/`: `Analyze` (função pura sobre dados coletados, em `analyze.go`), coleta (`collect.go`,
  `deep.go`), `Store` dos `analysis_id` e a execução da limpeza (`cleanup.go`).
  - Limites da heurística (constantes em `analyze.go`): OOM com limit **< 8Gi** → `aumentar_memoria`
    (sugere o dobro, limitado a 8Gi); OOM com limit ≥ 8Gi ou log com corrupção → `limpar_wal`; OOM depois de
    "Server is ready" (já serviu) → sempre `aumentar_memoria`, porque apagar o WAL não resolve; OOM sem limit → `investigar`.
    Ajuste conforme os casos reais.
- `internal/web/handlers/prometheus_wal.go`: rotas e guard-rails.
- Frontend: `components/PrometheusWalPanel.tsx`, montado no editor quando o recurso é o StatefulSet do Prometheus;
  HTTP por `lib/api/client.ts`, hooks em `hooks/useAPI.ts`.
- Testes: análise por tabela (fixtures de STS, status de pod, logs de OOM e de corrupção); execução com clientset
  fake verificando que **sempre** religa (erro e cancelamento); guard de `analysis_id` expirado.

Testes contra cluster real devem ser feitos pelo usuário, com a rede/VPN da própria máquina.

## Fallback: script bash

`scripts/prometheus-wal-cleanup.sh` continua no repositório como alternativa de linha de comando (por exemplo,
com a aplicação web indisponível). Ele **não** faz a análise prévia: desliga, mostra o que será perdido e pede
confirmação. Use-o só depois de avaliar manualmente que a limpeza é necessária.

```bash
scripts/prometheus-wal-cleanup.sh -c <context> [-n monitoring] [-p prometheus-prometheus] [--dry-run] [-y]
```

| Flag | Descrição |
|---|---|
| `-c` | context do kubeconfig (obrigatório) |
| `-n` | namespace do Prometheus (padrão `monitoring`) |
| `-p` | nome do recurso Prometheus (padrão: o único do namespace) |
| `--dry-run` | só mostra o que faria, sem desligar nada |
| `-y` | não pede confirmação (cuidado) |

Requer `kubectl` e `jq`. Limpa **todas** as réplicas (a versão em Go permitirá escolher só a com problema).
Se algo falhar no meio, o `trap` remove o pod temporário e religa o Prometheus; se o processo for morto à força,
religue manualmente:

```bash
kubectl --context <ctx> -n <ns> patch prometheus <nome> --type merge -p '{"spec":{"replicas":<original>}}'
```
