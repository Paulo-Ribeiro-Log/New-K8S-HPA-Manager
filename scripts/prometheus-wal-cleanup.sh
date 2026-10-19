#!/usr/bin/env bash
# Apaga wal/ e chunks_head/ de um Prometheus gerenciado pelo prometheus-operator que não sobe
# (OOMKilled no replay do WAL). Os blocos compactados (pastas 01…) são preservados; perde-se
# tudo o que ainda não virou bloco — o script mostra até quando vai o último bloco antes de apagar.
#
# Passos: lê o StatefulSet → desliga o Prometheus (replicas 0 no recurso Prometheus) → sobe um pod
# temporário com o PVC → mostra o que será perdido e pede confirmação → apaga → remove o pod →
# devolve as réplicas originais. Se algo falhar no meio, o trap remove o pod e religa o Prometheus.
#
# Uso:
#   scripts/prometheus-wal-cleanup.sh -c <context> [-n monitoring] [-p prometheus-prometheus] [--dry-run] [-y]
#
#   -c  context do kubeconfig (obrigatório)
#   -n  namespace do Prometheus (padrão: monitoring)
#   -p  nome do recurso Prometheus (padrão: o único do namespace)
#   --dry-run  só mostra o que faria (não desliga nada)
#   -y  não pede confirmação (cuidado)
#
# Requer: kubectl, jq. Se o recurso Prometheus é gerenciado por Helm/GitOps, pause o sync antes.

set -euo pipefail

CTX=""
NS="monitoring"
PROM=""
DRY_RUN=0
YES=0
IMAGE="busybox:1.36"
POD="prometheus-wal-cleanup"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -c) CTX="$2"; shift 2 ;;
    -n) NS="$2"; shift 2 ;;
    -p) PROM="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -y) YES=1; shift ;;
    -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
    *) echo "opção desconhecida: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$CTX" ]] || { echo "informe o context com -c" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq não encontrado" >&2; exit 2; }

k() { kubectl --context "$CTX" -n "$NS" "$@"; }
log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }

# --- Descoberta -------------------------------------------------------------------------------
if [[ -z "$PROM" ]]; then
  mapfile -t proms < <(k get prometheus -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
  [[ ${#proms[@]} -eq 1 ]] || { echo "há ${#proms[@]} recursos Prometheus em $NS; escolha com -p: ${proms[*]}" >&2; exit 2; }
  PROM="${proms[0]}"
fi

STS="prometheus-$PROM"
STS_JSON=$(k get statefulset "$STS" -o json)
ORIG_REPLICAS=$(k get prometheus "$PROM" -o jsonpath='{.spec.replicas}')
ORIG_REPLICAS=${ORIG_REPLICAS:-1}

VCT=$(jq -r '.spec.volumeClaimTemplates[0].metadata.name // empty' <<<"$STS_JSON")
[[ -n "$VCT" ]] || { echo "o StatefulSet $STS não usa volumeClaimTemplates (sem PVC, não há o que limpar)" >&2; exit 1; }
SUBPATH=$(jq -r --arg v "$VCT" '.spec.template.spec.containers[] | select(.name=="prometheus") | .volumeMounts[] | select(.name==$v) | .subPath // ""' <<<"$STS_JSON")
SECCTX=$(jq -c '.spec.template.spec.securityContext // {}' <<<"$STS_JSON")
TOLS=$(jq -c '.spec.template.spec.tolerations // []' <<<"$STS_JSON")
NODESEL=$(jq -c '.spec.template.spec.nodeSelector // {}' <<<"$STS_JSON")

log "context=$CTX  namespace=$NS  Prometheus=$PROM (réplicas: $ORIG_REPLICAS)"

# Uma limpeza por réplica (PVC <vct>-<sts>-<i>).
PVCS=()
for ((i = 0; i < ORIG_REPLICAS; i++)); do PVCS+=("$VCT-$STS-$i"); done
log "PVCs: ${PVCS[*]}  (subPath: ${SUBPATH:-nenhum})"

if [[ $DRY_RUN -eq 1 ]]; then
  log "dry-run: desligaria $PROM, montaria cada PVC em $POD e apagaria wal/, chunks_head/ e lock."
  exit 0
fi

# --- Restauração garantida ----------------------------------------------------------------------
SCALED_DOWN=0
cleanup() {
  local rc=$?
  k delete pod "$POD" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  if [[ $SCALED_DOWN -eq 1 ]]; then
    log "religando $PROM com $ORIG_REPLICAS réplica(s)"
    k patch prometheus "$PROM" --type merge -p "{\"spec\":{\"replicas\":$ORIG_REPLICAS}}" >/dev/null || \
      warn "falha ao religar — rode: kubectl --context $CTX -n $NS patch prometheus $PROM --type merge -p '{\"spec\":{\"replicas\":$ORIG_REPLICAS}}'"
  fi
  exit $rc
}
trap cleanup EXIT INT TERM

# --- Desligar -----------------------------------------------------------------------------------
log "desligando $PROM (replicas: 0)"
k patch prometheus "$PROM" --type merge -p '{"spec":{"replicas":0}}' >/dev/null
SCALED_DOWN=1
for ((i = 0; i < ORIG_REPLICAS; i++)); do
  k wait --for=delete "pod/$STS-$i" --timeout=5m >/dev/null 2>&1 || true
done

# --- Limpeza por PVC ----------------------------------------------------------------------------
for PVC in "${PVCS[@]}"; do
  log "montando $PVC em $POD"
  MOUNT=$(jq -nc --arg s "$SUBPATH" '{name:"data",mountPath:"/prometheus"} + (if $s != "" then {subPath:$s} else {} end)')
  jq -n --arg pod "$POD" --arg img "$IMAGE" --arg pvc "$PVC" \
     --argjson sec "$SECCTX" --argjson tols "$TOLS" --argjson ns "$NODESEL" --argjson mount "$MOUNT" '{
    apiVersion: "v1", kind: "Pod",
    metadata: {name: $pod, labels: {app: "prometheus-wal-cleanup"}},
    spec: {
      restartPolicy: "Never", securityContext: $sec, tolerations: $tols, nodeSelector: $ns,
      containers: [{name: "sh", image: $img, command: ["sleep", "3600"],
        resources: {requests: {cpu: "50m", memory: "32Mi"}, limits: {cpu: "200m", memory: "64Mi"}},
        volumeMounts: [$mount]}],
      volumes: [{name: "data", persistentVolumeClaim: {claimName: $pvc}}]
    }}' | k apply -f - >/dev/null
  k wait --for=condition=Ready "pod/$POD" --timeout=5m >/dev/null

  # Último bloco compactado (maxTime em ms no meta.json de cada bloco).
  LAST_MS=$(k exec "$POD" -- sh -c 'cat /prometheus/*/meta.json 2>/dev/null' | grep -o '"maxTime": *[0-9]*' | grep -o '[0-9]*$' | sort -n | tail -1 || true)
  BLOCKS=$(k exec "$POD" -- sh -c 'ls -d /prometheus/01* 2>/dev/null | wc -l')
  SIZES=$(k exec "$POD" -- sh -c 'cd /prometheus && du -sh wal chunks_head 2>/dev/null' | tr '\n' ' ')
  echo
  echo "  PVC:               $PVC"
  echo "  blocos preservados: $BLOCKS"
  echo "  a apagar:           $SIZES"
  if [[ -n "$LAST_MS" ]]; then
    echo "  último bloco termina em: $(date -d "@$((LAST_MS / 1000))" '+%d/%m/%Y %H:%M %Z')"
    warn "as métricas DEPOIS dessa data só existem no WAL e serão PERDIDAS."
  else
    warn "nenhum bloco compactado encontrado — TODO o histórico está no WAL e será perdido."
  fi
  echo

  if [[ $YES -ne 1 ]]; then
    read -r -p "Digite APAGAR para continuar (qualquer outra coisa cancela): " ans
    [[ "$ans" == "APAGAR" ]] || { warn "cancelado — nada foi apagado"; exit 1; }
  fi

  k exec "$POD" -- sh -c 'rm -rf /prometheus/wal/* /prometheus/chunks_head/* /prometheus/lock && ls /prometheus'
  log "$PVC limpo"
  k delete pod "$POD" --wait=true >/dev/null
done

# --- Religar e acompanhar ------------------------------------------------------------------------
trap - EXIT INT TERM
log "religando $PROM com $ORIG_REPLICAS réplica(s)"
k patch prometheus "$PROM" --type merge -p "{\"spec\":{\"replicas\":$ORIG_REPLICAS}}" >/dev/null
k wait --for=condition=Ready "pod/$STS-0" --timeout=10m && log "Prometheus pronto" || \
  warn "ainda não ficou pronto — acompanhe: kubectl --context $CTX -n $NS get pod $STS-0 -w"
