<div align="center">

# RELATORIO DE ALERTAS - HEALTH CHECK

**Gerado em: 11/02/2026, 11:16:34**

*SRE Logistica - K8s HPA Manager*

</div>

---

## SUMÁRIO EXECUTIVO

- **Total de Clusters:** 1
- **Total de Warnings:** 44
- **Total de Criticals:** 29

---

## ANÁLISE DETALHADA POR CLUSTER

### 🖥️ Cluster: **akspriv-entregamais-prd-admin**

- **Warnings:** 44
- **Criticals:** 29

#### Alertas Detectados

| Status | Tipo | Mensagem |
|--------|------|----------|
| **WARNING** | events | UpdateFailed: ExternalSecret calico-apiserver/akv-entregamais-prd-externalsecrets - error processing spec.dataFrom[0].find, err: error getting all secrets: keyvault.BaseClient#GetSecrets: Failure responding to request: StatusCode=403 -- Original Error: autorest/azure: Service returned an error. Status=403 Code="Forbidden" Message="Client address is not authorized and caller is not a trusted service.\r \| Client address: 20.201.4.120\r \| Caller: appid=a1410026-861e-4754-85cf-4be6103cbff7;oid=3a523610-35e6-48ba-b759-a789bb486fb1;iss=https://sts.windows.net/5a86b3fb-4213-49cd-b4d6-be91482ad3c0/;xms_mirid=/subscriptions/819a7d8f-1b0a-4121-b7dc-d001d9f109f1/resourcegroups/MC_rg-entregamais-app-prd_akspriv-entregamais-prd_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-entregamaisn-14165312-vmss;xms_az_rid=/subscriptions/819a7d8f-1b0a-4121-b7dc-d001d9f109f1/resourcegroups/MC_rg-entregamais-app-prd_akspriv-entregamais-prd_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-entregamaisn-14165312-vmss\r \| Vault: akv-entregamais-prd;location=brazilsouth" InnerError={"code":"ForbiddenByFirewall"} |
| **WARNING** | events | PRESSAO DE CPU no Node aks-entregamaisn-14165312-vmss000000 \| Cores: 4 \| Load: 6.59 (threshold: 3.6 (0.9 * 4)) \| PSI CPU: 60.21% \| CPU: user 57.70%!s(MISSING)ystem: 14.36%!i(MISSING)owait: 0.00%!s(MISSING)teal: 0.00%!i(MISSING)dle: 25.85%!, sys 14.36%!i(MISSING)owait: 0.00%!s(MISSING)teal: 0.00%!i(MISSING)dle: 25.85%!, idle 25.85%! \| Acao: escalar nodes ou reduzir workloads |
| **WARNING** | events | PRESSAO DE CPU no Node aks-entregamaisn-14165312-vmss000000 \| Cores: 4 \| Load: 6.61 (threshold: 3.6 (0.9 * 4)) \| PSI CPU: 60.74% \| CPU: user 62.53%!s(MISSING)ystem: 17.05%!i(MISSING)owait: 4.13%!s(MISSING)teal: 0.00%!i(MISSING)dle: 13.95%!, sys 17.05%!i(MISSING)owait: 4.13%!s(MISSING)teal: 0.00%!i(MISSING)dle: 13.95%!, idle 13.95%! \| Acao: escalar nodes ou reduzir workloads |
| **WARNING** | events | UpdateFailed: ExternalSecret entrega-mais-prd/akv-entregamais-prd-externalsecrets - error processing spec.dataFrom[0].find, err: error getting all secrets: keyvault.BaseClient#GetSecrets: Failure responding to request: StatusCode=403 -- Original Error: autorest/azure: Service returned an error. Status=403 Code="Forbidden" Message="Client address is not authorized and caller is not a trusted service.\r \| Client address: 20.201.4.120\r \| Caller: appid=a1410026-861e-4754-85cf-4be6103cbff7;oid=3a523610-35e6-48ba-b759-a789bb486fb1;iss=https://sts.windows.net/5a86b3fb-4213-49cd-b4d6-be91482ad3c0/;xms_mirid=/subscriptions/819a7d8f-1b0a-4121-b7dc-d001d9f109f1/resourcegroups/MC_rg-entregamais-app-prd_akspriv-entregamais-prd_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-entregamaisn-14165312-vmss;xms_az_rid=/subscriptions/819a7d8f-1b0a-4121-b7dc-d001d9f109f1/resourcegroups/MC_rg-entregamais-app-prd_akspriv-entregamais-prd_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-entregamaisn-14165312-vmss\r \| Vault: akv-entregamais-prd;location=brazilsouth" InnerError={"code":"ForbiddenByFirewall"} |
| **WARNING** | events | FailedGetScale: HorizontalPodAutoscaler istio-system/kiali - no matches for kind "Deployment" in group "extensions" |
| **WARNING** | events | UpdateFailed: ExternalSecret migracao-pedido-prd/akv-entregamais-prd-externalsecrets - error processing spec.dataFrom[0].find, err: error getting all secrets: keyvault.BaseClient#GetSecrets: Failure responding to request: StatusCode=403 -- Original Error: autorest/azure: Service returned an error. Status=403 Code="Forbidden" Message="Client address is not authorized and caller is not a trusted service.\r \| Client address: 20.201.4.120\r \| Caller: appid=a1410026-861e-4754-85cf-4be6103cbff7;oid=3a523610-35e6-48ba-b759-a789bb486fb1;iss=https://sts.windows.net/5a86b3fb-4213-49cd-b4d6-be91482ad3c0/;xms_mirid=/subscriptions/819a7d8f-1b0a-4121-b7dc-d001d9f109f1/resourcegroups/MC_rg-entregamais-app-prd_akspriv-entregamais-prd_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-entregamaisn-14165312-vmss;xms_az_rid=/subscriptions/819a7d8f-1b0a-4121-b7dc-d001d9f109f1/resourcegroups/MC_rg-entregamais-app-prd_akspriv-entregamais-prd_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-entregamaisn-14165312-vmss\r \| Vault: akv-entregamais-prd;location=brazilsouth" InnerError={"code":"ForbiddenByFirewall"} |
| **WARNING** | events | 6 evento(s) de aviso encontrado(s) |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-cargas: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-eventos: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-faturamento: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **WARNING** | pvcs | PVC monitoring/prometheus-prometheus-prometheus-db-prometheus-prometheus-prometheus-0: AVISO: PV usa ReclaimPolicy=Delete - dados serao perdidos se PVC for deletado |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-inventario: AVISO: HPA esta no limite maximo de replicas (15/15) - pode precisar de mais capacidade |
| **WARNING** | pvcs | 1 PVC(s) com avisos encontrado(s) |
| **WARNING** | deployments | entrega-mais-prd/entrega-mais-api: LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-api tem 1 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-nf: AVISO: HPA esta no limite maximo de replicas (4/4) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-pedido: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-std: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-std-consumer: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-transporte: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/entrega-mais-web-app: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | HPA entrega-mais-prd/insucesso-pedido-api: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **WARNING** | deployments | entrega-mais-prd/entrega-mais-inventario: LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-inventario tem 11 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **CRITICAL** | hpas | HPA entrega-mais-prd/insucesso-pedido-consumer: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **WARNING** | configs | Secret dynatrace/dynatrace-webhook-certs: SECRET COM PROBLEMAS: dynatrace/dynatrace-webhook-certs tem 1 valor(es) invalido(s) ou vazio(s): ca.crt.old (valor vazio). Credenciais podem estar incorretas. |
| **WARNING** | deployments | entrega-mais-prd/entrega-mais-nf: LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-nf tem 2 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **WARNING** | deployments | entrega-mais-prd/entrega-mais-pedido: LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-pedido tem 1 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-login: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-login tem 2 valor(es) invalido(s) ou vazio(s): MAX_ACTIVE_CONNECTION (connection string inválida), URL_DATASOURCE (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-login-6d8bkb8mmf: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-login-6d8bkb8mmf tem 2 valor(es) invalido(s) ou vazio(s): URL_DATASOURCE (connection string inválida), MAX_ACTIVE_CONNECTION (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-web-app: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app tem 1 valor(es) invalido(s) ou vazio(s): VITE_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-web-app-2hffb5hk65: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-2hffb5hk65 tem 1 valor(es) invalido(s) ou vazio(s): VITE_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-web-app-2hffb5hk65-v000: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-2hffb5hk65-v000 tem 1 valor(es) invalido(s) ou vazio(s): VITE_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-web-app-47t98694f2: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-47t98694f2 tem 1 valor(es) invalido(s) ou vazio(s): REACT_APP_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-web-app-58fm49mg8c: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-58fm49mg8c tem 1 valor(es) invalido(s) ou vazio(s): REACT_APP_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | configs | ConfigMap entrega-mais-prd/entrega-mais-web-app-5dkk85b2cf: CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-5dkk85b2cf tem 1 valor(es) invalido(s) ou vazio(s): REACT_APP_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **CRITICAL** | hpas | HPA istio-system/kiali: AVISO: HPA tem 1 metrica(s) com erro - verificar se metrics-server esta funcionando \| AVISO: Evento de falha de scaling recente: FailedGetScale - no matches for kind "Deployment" in group "extensions" |
| **WARNING** | deployments | entrega-mais-prd/entrega-mais-web-app: 1 avisos na configuração de probes |
| **CRITICAL** | hpas | HPA migracao-pedido-prd/migracao-pedido-app: AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | hpas | 14 HPA(s) com problemas criticos encontrado(s) |
| **WARNING** | configs | Secret istio-system/istio-ca-secret: SECRET COM PROBLEMAS: istio-system/istio-ca-secret tem 4 valor(es) invalido(s) ou vazio(s): cert-chain.pem (valor vazio), key.pem (valor vazio), key.pem (credential muito curta, possível erro), root-cert.pem (valor vazio). Credenciais podem estar incorretas. |
| **WARNING** | configs | Secret kube-system/bootstrap-token-e1drtf: SECRET COM PROBLEMAS: kube-system/bootstrap-token-e1drtf tem 2 valor(es) invalido(s) ou vazio(s): token-id (credential muito curta, possível erro), usage-bootstrap-authentication (credential muito curta, possível erro). Credenciais podem estar incorretas. |
| **WARNING** | configs | Secret kube-system/bootstrap-token-nai8v1: SECRET COM PROBLEMAS: kube-system/bootstrap-token-nai8v1 tem 2 valor(es) invalido(s) ou vazio(s): usage-bootstrap-authentication (credential muito curta, possível erro), token-id (credential muito curta, possível erro). Credenciais podem estar incorretas. |
| **WARNING** | configs | Secret kube-system/bootstrap-token-s1y9r4: SECRET COM PROBLEMAS: kube-system/bootstrap-token-s1y9r4 tem 2 valor(es) invalido(s) ou vazio(s): token-id (credential muito curta, possível erro), usage-bootstrap-authentication (credential muito curta, possível erro). Credenciais podem estar incorretas. |
| **WARNING** | configs | Secret kube-system/bootstrap-token-xnvbhb: SECRET COM PROBLEMAS: kube-system/bootstrap-token-xnvbhb tem 2 valor(es) invalido(s) ou vazio(s): usage-bootstrap-authentication (credential muito curta, possível erro), token-id (credential muito curta, possível erro). Credenciais podem estar incorretas. |
| **WARNING** | configs | Secret monitoring/alertmanager-prometheus-alertmanager-tls-assets-0: SECRET VAZIO: monitoring/alertmanager-prometheus-alertmanager-tls-assets-0 nao contem nenhuma chave. Se estiver em uso, aplicacao pode falhar ao ler credenciais. |
| **WARNING** | configs | Secret monitoring/alertmanager-prometheus-alertmanager-web-config: SECRET COM PROBLEMAS: monitoring/alertmanager-prometheus-alertmanager-web-config tem 1 valor(es) invalido(s) ou vazio(s): web-config.yaml (valor vazio). Credenciais podem estar incorretas. |
| **WARNING** | configs | Secret monitoring/prometheus-prometheus-prometheus-tls-assets-0: SECRET VAZIO: monitoring/prometheus-prometheus-prometheus-tls-assets-0 nao contem nenhuma chave. Se estiver em uso, aplicacao pode falhar ao ler credenciais. |
| **WARNING** | configs | Secret monitoring/prometheus-prometheus-prometheus-web-config: SECRET COM PROBLEMAS: monitoring/prometheus-prometheus-prometheus-web-config tem 1 valor(es) invalido(s) ou vazio(s): web-config.yaml (valor vazio). Credenciais podem estar incorretas. |
| **CRITICAL** | summary | Total: 432 checks realizados |
| **CRITICAL** | summary | Críticos: 14 |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-cargas - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-eventos - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-faturamento - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-inventario - AVISO: HPA esta no limite maximo de replicas (15/15) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-nf - AVISO: HPA esta no limite maximo de replicas (4/4) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-pedido - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-std - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-std-consumer - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-transporte - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | Crítico: HPA entrega-mais-prd/entrega-mais-web-app - AVISO: HPA tem minReplicas (1) igual a maxReplicas (1) - nao ha scaling automatico \| AVISO: HPA esta no limite maximo de replicas (1/1) - pode precisar de mais capacidade |
| **CRITICAL** | summary | ... e mais 4 problema(s) crítico(s). Veja abaixo nos resultados completos. |
| **WARNING** | summary | Avisos: 25 |
| **WARNING** | summary | Aviso: Deployment entrega-mais-prd/entrega-mais-api - LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-api tem 1 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **WARNING** | summary | Aviso: Deployment entrega-mais-prd/entrega-mais-inventario - LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-inventario tem 11 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **WARNING** | summary | Aviso: Deployment entrega-mais-prd/entrega-mais-nf - LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-nf tem 2 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **WARNING** | summary | Aviso: Deployment entrega-mais-prd/entrega-mais-pedido - LIVENESS REINICIANDO: Deployment entrega-mais-prd/entrega-mais-pedido tem 1 container(s) sendo reiniciados por liveness probe. Pode indicar travamento ou timeout muito curto. |
| **WARNING** | summary | Aviso: Deployment entrega-mais-prd/entrega-mais-web-app - 1 avisos na configuração de probes |
| **WARNING** | summary | Aviso: Config entrega-mais-prd/entrega-mais-login - CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-login tem 2 valor(es) invalido(s) ou vazio(s): MAX_ACTIVE_CONNECTION (connection string inválida), URL_DATASOURCE (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | summary | Aviso: Config entrega-mais-prd/entrega-mais-login-6d8bkb8mmf - CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-login-6d8bkb8mmf tem 2 valor(es) invalido(s) ou vazio(s): URL_DATASOURCE (connection string inválida), MAX_ACTIVE_CONNECTION (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | summary | Aviso: Config entrega-mais-prd/entrega-mais-web-app - CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app tem 1 valor(es) invalido(s) ou vazio(s): VITE_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | summary | Aviso: Config entrega-mais-prd/entrega-mais-web-app-2hffb5hk65 - CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-2hffb5hk65 tem 1 valor(es) invalido(s) ou vazio(s): VITE_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | summary | Aviso: Config entrega-mais-prd/entrega-mais-web-app-2hffb5hk65-v000 - CONFIGMAP COM PROBLEMAS: entrega-mais-prd/entrega-mais-web-app-2hffb5hk65-v000 tem 1 valor(es) invalido(s) ou vazio(s): VITE_EVENTS_URL (connection string inválida). Aplicacao pode nao funcionar corretamente. |
| **WARNING** | summary | ... e mais 15 aviso(s). Veja abaixo nos resultados completos. |
| **CRITICAL** | complete | Concluído - Status: critical |

---


---

*Relatório gerado automaticamente pelo K8s HPA Manager*
