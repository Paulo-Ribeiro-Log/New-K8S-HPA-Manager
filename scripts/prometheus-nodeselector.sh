#!/usr/bin/env bash
# Troca o nodeSelector do recurso Prometheus (prometheus-operator, CRD monitoring.coreos.com) para que o pod do
# Prometheus rode em outros nodes. Interativo: lista os nodes, diz em quais o pod serve e pergunta o que não vier por opção.
#
# Por que no recurso Prometheus: o StatefulSet do Prometheus pertence a ele, e o operator o reescreve a partir do
# spec.nodeSelector do recurso. Editar o StatefulSet à mão (ou por política do Kyverno) não permanece/não resolve; trocar
# o valor na fonte faz o operator regerar o StatefulSet e recriar o pod já no node certo.
#
# O que NÃO resolve: o `helm upgrade` da release (kube-prometheus-stack) reescreve o recurso com o nodeSelector dos values
# (prometheus.prometheusSpec.nodeSelector). Para ficar definitivo, ajuste também os values do Helm.
#
# O patch é um JSON merge patch que SUBSTITUI o mapa inteiro (as chaves antigas viram null), então o nodeSelector final é
# exatamente {chave: valor}. Só o nodeSelector é alterado; as tolerations não são tocadas (o script avisa se o pod não
# tolera os taints dos nodes escolhidos, usando as tolerations do recurso Prometheus). Antes de alterar, grava um backup.
#
# Uso:
#   scripts/prometheus-nodeselector.sh
#   scripts/prometheus-nodeselector.sh -c <context> [-n <namespace>] [-p <recurso Prometheus>] [-l chave=valor] [-y] [--dry-run]
#   scripts/prometheus-nodeselector.sh --desfazer [-c <context>] [-n <namespace>] [-p <recurso Prometheus>]
#
#   -c  context do kubeconfig (sem -c: mostra o atual e deixa escolher; o padrão é ESCOLHER, não aceitar o atual)
#   -n  namespace do recurso Prometheus (sem -n: pergunta; sem terminal ou com -y, usa monitoring)
#   -p  nome do recurso Prometheus (sem -p: lista os do namespace). O StatefulSet é prometheus-<nome>
#   -l  label dos nodes de destino, chave=valor (sem -l: lista os nodes e oferece os labels que existem neles)
#   --desfazer      volta ao nodeSelector de um backup deste script (escolhe o backup se houver mais de um)
#   --dry-run       mostra o patch e valida no servidor (--dry-run=server); não altera nada
#   --force         segue mesmo sem node com o label ou com taint não tolerado (no modo interativo, é perguntado)
#   -y              não pede confirmação (exige -c, -p e -l)
#   WAIT_TRIES      quantas esperas de 2 s pelo operator atualizar o StatefulSet (padrão: 30)
#
# Requer: kubectl e jq. Só usa a API do Kubernetes (nenhuma CLI de nuvem). Funciona com o bash 3.2 do macOS. Os comandos
# usam sempre --context: o contexto atual do seu kubeconfig nunca é alterado.

set -euo pipefail

CTX=""
NS=""
PROM=""
LABEL=""
DRY_RUN=0
YES=0
FORCE=0
MODE=""
CRD="prometheus.monitoring.coreos.com"
BACKUP_ROOT="${HOME}/.k8s-hpa-manager/prometheus-nodeselector-backups"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    -c) CTX="${2:-}"; shift 2 ;;
    -n) NS="${2:-}"; shift 2 ;;
    -p) PROM="${2:-}"; shift 2 ;;
    -l) LABEL="${2:-}"; shift 2 ;;
    --desfazer) MODE="desfazer"; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --force) FORCE=1; shift ;;
    -y) YES=1; shift ;;
    -h|--help) sed -n '2,32p' "$0"; exit 0 ;;
    *) die "opção desconhecida: $1 (veja --help)" ;;
  esac
done

command -v kubectl >/dev/null || die "kubectl não encontrado"
command -v jq >/dev/null || die "jq não encontrado"

interativo() { [[ -t 0 ]]; }
k() { kubectl --context "$CTX" "$@"; }

# ---- interação ---------------------------------------------------------------------------------------------------

# menu "<título>" "<padrão 1..N ou vazio>" item...   → MENU_IDX (0-based)
menu() {
  local title="$1" def="$2" n i ans
  shift 2
  local items=("$@")
  n=${#items[@]}
  echo
  echo "$title"
  for ((i = 0; i < n; i++)); do printf '  %2d) %s\n' $((i + 1)) "${items[$i]}"; done
  while true; do
    if [[ -n "$def" ]]; then
      read -r -p "Escolha [1-$n] (ENTER = $def): " ans
      ans="${ans:-$def}"
    else
      read -r -p "Escolha [1-$n]: " ans
    fi
    if [[ "$ans" =~ ^[0-9]+$ ]] && ((ans >= 1 && ans <= n)); then
      MENU_IDX=$((ans - 1))
      return 0
    fi
    warn "opção inválida"
  done
}

# sim_nao "<pergunta>" → 0 se o operador respondeu s (o padrão é NÃO); com -y responde sim
sim_nao() {
  local ans
  [[ $YES -eq 1 ]] && return 0
  interativo || die "pergunta sem terminal: $1 (use -y ou rode em um terminal)"
  read -r -p "$1 (s/N): " ans
  [[ "$ans" == "s" ]]
}

escolher_contexto() {
  interativo || die "informe o context com -c (ou rode em um terminal para escolher)"
  local atual ctxs=() l ans
  atual="$(kubectl config current-context 2>/dev/null || true)"
  if [[ -n "$atual" ]]; then
    read -r -p "Context atual: $atual. Usar esse? (s/N): " ans
    if [[ "$ans" == "s" ]]; then CTX="$atual"; return 0; fi
  fi
  while IFS= read -r l; do [[ -n "$l" ]] && ctxs+=("$l"); done < <(kubectl config get-contexts -o name 2>/dev/null)
  [[ ${#ctxs[@]} -gt 0 ]] || die "nenhum context encontrado no kubeconfig"
  menu "Contexts disponíveis:" "" "${ctxs[@]}"
  CTX="${ctxs[$MENU_IDX]}"
}

escolher_namespace() {
  [[ -n "$NS" ]] && return 0
  if interativo && [[ $YES -ne 1 ]]; then
    read -r -p "Namespace do recurso Prometheus [monitoring]: " NS
  fi
  NS="${NS:-monitoring}"
}

escolher_prometheus() {
  [[ -n "$PROM" ]] && return 0
  interativo || die "informe o recurso Prometheus com -p"
  local linhas=() itens=() l nome sel pausado
  while IFS= read -r l; do [[ -n "$l" ]] && linhas+=("$l"); done < <(k -n "$NS" get "$CRD" -o json |
    jq -r '.items[] | [.metadata.name, (.spec.nodeSelector // {} | tojson), (.spec.paused // false | tostring)] | @tsv')
  [[ ${#linhas[@]} -gt 0 ]] || die "nenhum recurso Prometheus em $CTX/$NS"
  for l in "${linhas[@]}"; do
    nome="${l%%$'\t'*}"; l="${l#*$'\t'}"; sel="${l%%$'\t'*}"; pausado="${l#*$'\t'}"
    itens+=("$nome   nodeSelector: $sel$([[ "$pausado" == "true" ]] && echo '   [PAUSADO: o operator não reconcilia]')")
  done
  if [[ ${#linhas[@]} -eq 1 ]]; then
    MENU_IDX=0
    log "recurso Prometheus: ${itens[0]}"
  else
    menu "Recursos Prometheus em $CTX/$NS:" "" "${itens[@]}"
  fi
  PROM="${linhas[$MENU_IDX]%%$'\t'*}"
}

# jq: o taint (NoSchedule/NoExecute) é tolerado por alguma toleration do pod? (mesma regra do Kubernetes)
JQ_TOLERATED='def tolerated($tols): . as $t | any($tols[]?;
  ((.effect // "") == "" or .effect == $t.effect)
  and ((.key // "") == "" or .key == $t.key)
  and ((.operator // "Equal") == "Exists" or ((.operator // "Equal") == "Equal" and (.value // "") == ($t.value // ""))));
def serve($tols): ((.spec.unschedulable // false) | not)
  and ([.spec.taints[]? | select(.effect != "PreferNoSchedule") | select(tolerated($tols) | not)] | length == 0);'

# Tabela dos nodes do cluster, já dizendo se o pod do Prometheus serve em cada um (taints tolerados, não cordonado).
mostrar_nodes() {
  local tols="$1" nodes_json="$2"
  echo
  log "nodes de $CTX (✔ = o pod tolera os taints e o node não está cordonado)"
  {
    printf 'SERVE\tNODE\tPOOL\tAPP\tTAINTS\n'
    jq -r --argjson tols "$tols" "$JQ_TOLERATED"'
      .items[] | [ (if serve($tols) then "✔" else "✖" end), .metadata.name, (.metadata.labels.agentpool // "-"),
        (.metadata.labels.app // "-"),
        (([.spec.taints[]? | "\(.key)=\(.value // ""):\(.effect)"] | join(", ")) | if . == "" then "nenhum" else . end) + (if .spec.unschedulable then "  [CORDONADO]" else "" end) ] | @tsv' <<<"$nodes_json"
  } | if command -v column >/dev/null; then column -t -s $'\t'; else cat; fi
}

# Labels que existem nos nodes (agentpool, app, ...) com quantos nodes têm cada um e quantos servem ao pod.
labels_candidatos() {
  local tols="$1" nodes_json="$2"
  jq -r --argjson tols "$tols" --argjson keys '["agentpool","app","kubernetes.azure.com/agentpool","eks.amazonaws.com/nodegroup","cloud.google.com/gke-nodepool"]' "$JQ_TOLERATED"'
    . as $root | [ $keys[] as $k | $root.items | map(select(.metadata.labels[$k] != null)) | group_by(.metadata.labels[$k])[]
      | {sel: "\($k)=\(.[0].metadata.labels[$k])", total: length, serve: ([.[] | select(serve($tols))] | length)} ]
    | unique_by(.sel) | .[] | [.sel, .total, .serve] | @tsv' <<<"$nodes_json"
}

escolher_label() {
  local tols="$1" nodes_json="$2"
  [[ -n "$LABEL" ]] && return 0
  interativo || die "informe o label de destino com -l chave=valor"
  local itens=() sels=() l sel total serve ok
  while IFS= read -r l; do
    [[ -z "$l" ]] && continue
    sel="${l%%$'\t'*}"; l="${l#*$'\t'}"; total="${l%%$'\t'*}"; serve="${l#*$'\t'}"
    if [[ "$serve" -gt 0 ]]; then ok="✔ $serve de $total node(s) servem"; else ok="✖ nenhum dos $total node(s) serve (taint não tolerado ou cordonado)"; fi
    itens+=("$sel   ($ok)")
    sels+=("$sel")
  done < <(labels_candidatos "$tols" "$nodes_json")
  itens+=("digitar outro label (chave=valor)")
  menu "Para qual label de node o Prometheus deve ir?" "" "${itens[@]}"
  if [[ $MENU_IDX -lt ${#sels[@]} ]]; then
    LABEL="${sels[$MENU_IDX]}"
  else
    read -r -p "Label (chave=valor): " LABEL
  fi
}

# ---- patch -------------------------------------------------------------------------------------------------------

# patch_substituindo "<nodeSelector atual (json)>" "<nodeSelector novo (json)>" → merge patch que deixa EXATAMENTE o novo:
# as chaves atuais viram null (removidas) e as novas entram por cima.
patch_substituindo() {
  jq -nc --argjson cur "$1" --argjson new "$2" '{spec: {nodeSelector: (($cur | with_entries(.value = null)) + $new)}}'
}

# aplicar_patch "<nodeSelector atual>" "<nodeSelector novo>" → dry-run ou patch real, e espera o operator propagar.
aplicar_patch() {
  local cur="$1" new="$2" patch i now
  patch="$(patch_substituindo "$cur" "$new")"
  echo
  log "patch no recurso $CRD $NS/$PROM:"
  echo "   $patch"
  echo

  if [[ $DRY_RUN -eq 1 ]]; then
    log "dry-run: validando no servidor (nada é alterado)"
    k -n "$NS" patch "$CRD" "$PROM" --type merge --dry-run=server -p "$patch"
    return 0
  fi

  if [[ $YES -ne 1 ]]; then
    interativo || die "sem terminal: use -y (aplicar) ou --dry-run"
    sim_nao "Aplicar em $CTX? O operator recria o pod do Prometheus (se estiver rodando, ele reinicia)." || { warn "cancelado — nada foi alterado"; exit 1; }
  fi

  k -n "$NS" patch "$CRD" "$PROM" --type merge -p "$patch"
  log "recurso Prometheus alterado; aguardando o operator atualizar o StatefulSet prometheus-$PROM (até $(( ${WAIT_TRIES:-30} * 2 )) s)"
  for i in $(seq 1 "${WAIT_TRIES:-30}"); do
    now="$(k -n "$NS" get statefulset "prometheus-$PROM" -o json 2>/dev/null | jq -cS '.spec.template.spec.nodeSelector // {}' || true)"
    if [[ "$now" == "$(jq -cS . <<<"$new")" ]]; then
      log "StatefulSet prometheus-$PROM com nodeSelector $now"
      k -n "$NS" get pod "prometheus-$PROM-0" -o wide 2>/dev/null || true
      return 0
    fi
    sleep 2
  done
  warn "o StatefulSet prometheus-$PROM ainda não tem o nodeSelector novo (atual: ${now:-não encontrado}). Confira se o operator está rodando e se o recurso não está pausado (spec.paused)."
  return 1
}

# ---- início ------------------------------------------------------------------------------------------------------

if [[ -z "$MODE" ]]; then
  if interativo && [[ $YES -ne 1 ]]; then
    menu "O que fazer?" "1" "trocar o nodeSelector do Prometheus" "desfazer (voltar ao nodeSelector de um backup)"
    case "$MENU_IDX" in 0) MODE="aplicar" ;; *) MODE="desfazer" ;; esac
  else
    MODE="aplicar"
  fi
fi

[[ -n "$CTX" ]] || escolher_contexto
log "context: $CTX"

# ---- desfazer ----------------------------------------------------------------------------------------------------
if [[ "$MODE" == "desfazer" ]]; then
  arquivos=()
  itens=()
  if [[ -d "$BACKUP_ROOT/$CTX" ]]; then
    while IFS= read -r f; do
      [[ -z "$f" ]] && continue
      b_ns="$(jq -r '.namespace' "$f")"; b_name="$(jq -r '.name' "$f")"
      [[ -n "$NS" && "$b_ns" != "$NS" ]] && continue
      [[ -n "$PROM" && "$b_name" != "$PROM" ]] && continue
      arquivos+=("$f")
      itens+=("$b_ns/$b_name   voltar para $(jq -c '.nodeSelector' "$f")   ($(basename "$f"))")
    done < <(ls -1t "$BACKUP_ROOT/$CTX"/*.json 2>/dev/null || true)
  fi
  [[ ${#arquivos[@]} -gt 0 ]] || die "nenhum backup deste script para $CTX em $BACKUP_ROOT"
  if [[ ${#arquivos[@]} -eq 1 ]]; then
    MENU_IDX=0
    log "backup: ${itens[0]}"
  else
    interativo && [[ $YES -ne 1 ]] || die "mais de um backup para $CTX: informe -n e -p, ou rode em um terminal (o mais recente é o primeiro da lista)"
    menu "Backups em $CTX (do mais recente ao mais antigo):" "1" "${itens[@]}"
  fi
  f="${arquivos[$MENU_IDX]}"
  NS="$(jq -r '.namespace' "$f")"
  PROM="$(jq -r '.name' "$f")"
  RESTORE="$(jq -c '.nodeSelector' "$f")"
  CURRENT_SEL="$(k -n "$NS" get "$CRD" "$PROM" -o json | jq -c '.spec.nodeSelector // {}')" || die "recurso Prometheus $NS/$PROM não encontrado em $CTX"
  log "nodeSelector atual: $CURRENT_SEL   →   backup: $RESTORE"
  aplicar_patch "$CURRENT_SEL" "$RESTORE"
  exit $?
fi

# ---- aplicar -----------------------------------------------------------------------------------------------------
k get crd "$CRD" >/dev/null 2>&1 || die "o CRD $CRD não existe em $CTX (prometheus-operator não instalado)"
escolher_namespace
escolher_prometheus

PROM_JSON="$(k -n "$NS" get "$CRD" "$PROM" -o json)" || die "recurso Prometheus $NS/$PROM não encontrado em $CTX"
CURRENT_SEL="$(jq -c '.spec.nodeSelector // {}' <<<"$PROM_JSON")"
TOLS="$(jq -c '.spec.tolerations // []' <<<"$PROM_JSON")"
log "recurso Prometheus: $NS/$PROM   StatefulSet: prometheus-$PROM   nodeSelector atual: $CURRENT_SEL"
if [[ "$(jq -r '.spec.paused // false' <<<"$PROM_JSON")" == "true" ]]; then
  warn "o recurso está PAUSADO (spec.paused): o operator não vai propagar a mudança para o StatefulSet"
fi

ALL_NODES_JSON="$(k get nodes -o json)"
mostrar_nodes "$TOLS" "$ALL_NODES_JSON"
escolher_label "$TOLS" "$ALL_NODES_JSON"

[[ "$LABEL" =~ ^[^=[:space:]]+=[^=[:space:]]*$ ]] || die "label inválido: use chave=valor (ex.: agentpool=meupool)"
KEY="${LABEL%%=*}"
VAL="${LABEL#*=}"
NEW_SEL="$(jq -nc --arg k "$KEY" --arg v "$VAL" '{($k): $v}')"
log "nodeSelector atual: $CURRENT_SEL   →   novo: $NEW_SEL"
if [[ "$CURRENT_SEL" == "$NEW_SEL" ]]; then
  log "o recurso já tem esse nodeSelector; nada a fazer"
  exit 0
fi

# Os nodes de destino existem? E o pod tolera os taints deles?
NODES_JSON="$(k get nodes -l "$KEY=$VAL" -o json)"
N_NODES="$(jq '.items | length' <<<"$NODES_JSON")"
if [[ "$N_NODES" -eq 0 ]]; then
  warn "nenhum node tem o label $KEY=$VAL: o pod ficaria Pending"
  if [[ $FORCE -ne 1 ]]; then
    interativo && [[ $YES -ne 1 ]] && sim_nao "Seguir mesmo assim?" || die "abortado (use --force para seguir mesmo assim)"
  fi
else
  N_OK="$(jq --argjson tols "$TOLS" "$JQ_TOLERATED"'[.items[] | select(serve($tols))] | length' <<<"$NODES_JSON")"
  log "$N_OK de $N_NODES node(s) com $KEY=$VAL servem ao pod"
  if [[ "$N_OK" -eq 0 ]]; then
    warn "nenhum dos $N_NODES node(s) com $KEY=$VAL serve ao pod (taint não tolerado ou cordonado)"
    if [[ $FORCE -ne 1 ]]; then
      interativo && [[ $YES -ne 1 ]] && sim_nao "Seguir mesmo assim?" || die "abortado (ajuste as tolerations no recurso Prometheus/Helm ou use --force)"
    fi
  fi
fi

# Backup do que vai ser substituído (só em execução real).
if [[ $DRY_RUN -ne 1 ]]; then
  mkdir -p "$BACKUP_ROOT/$CTX"
  BACKUP="$BACKUP_ROOT/$CTX/prometheus-${NS}-${PROM}-$(date +%Y%m%d-%H%M%S).json"
  jq -n --arg ns "$NS" --arg name "$PROM" --argjson sel "$CURRENT_SEL" '{namespace: $ns, name: $name, nodeSelector: $sel}' >"$BACKUP"
  log "backup do nodeSelector atual: $BACKUP"
fi

aplicar_patch "$CURRENT_SEL" "$NEW_SEL" || exit 1

if [[ $DRY_RUN -ne 1 ]]; then
  echo
  warn "ajuste também prometheus.prometheusSpec.nodeSelector nos values do Helm: o próximo helm upgrade reescreve o recurso e desfaz esta troca."
  log "pronto. Para desfazer: $0 -c $CTX --desfazer   (backup em $BACKUP)"
fi
