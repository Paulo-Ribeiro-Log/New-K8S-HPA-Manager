# Configuração de Repositório Helm

## Problema: "repo helmrepo not found" ou "chart not found"

Quando você tenta fazer upgrade de um Helm release sem especificar o `chartRef`, o sistema tenta usar o nome do chart atual. Se o repositório não estiver configurado, o Helm falhará.

## Solução: Configurar o Repositório Helm

### 1. Descobrir a URL do Repositório

O repositório usado pelo Spinnaker é `helmrepo`. Você precisa descobrir a URL completa com o time de infraestrutura ou DevOps.

**Possíveis locais:**
- Harbor: `https://harbor.viavarejo.com.br/chartrepo/<projeto>`
- Artifactory: `https://artifactory.viavarejo.com.br/helm/<projeto>`
- GitHub Packages: `https://github.com/<org>/charts`

### 2. Configurar no Ambiente

Execute no pod ou servidor onde a aplicação roda:

```bash
# Adicionar o repositório (substitua a URL correta)
helm repo add helmrepo https://harbor.viavarejo.com.br/chartrepo/convair

# Atualizar índice
helm repo update

# Verificar
helm repo list
```

### 3. Testar

```bash
# Buscar o chart
helm search repo helmrepo/convair-helm

# Verificar se encontra
helm show chart helmrepo/convair-helm
```

## Alternativas Temporárias

### Opção 1: Especificar chartRef Manualmente

Na interface web, ao fazer upgrade, preencha o campo "Chart Reference":

```
helmrepo/convair-helm
```

### Opção 2: Usar URL Direta (se disponível)

Se souber a URL exata do chart:

```
https://harbor.viavarejo.com.br/chartrepo/convair/convair-helm-v0.9.0.tgz
```

### Opção 3: Exportar Chart do Release Atual

```bash
# Exportar chart do secret do Kubernetes
kubectl get secret -n <namespace> sh.helm.release.v1.<release-name>.v<revision> \
  -o jsonpath='{.data.release}' | \
  base64 -d | base64 -d | gunzip | \
  jq -r '.chart' | base64 -d > /tmp/chart.tgz

# Usar o chart exportado
helm upgrade <release> /tmp/chart.tgz --values values.yaml
```

## Configuração Permanente

Para evitar esse problema no futuro, configure o repositório `helmrepo` como parte do setup da aplicação:

**docker-compose.yml** ou **script de inicialização**:
```bash
#!/bin/bash
helm repo add helmrepo https://URL_DO_REPOSITORIO
helm repo update
```

## Descobrir o Repositório via Spinnaker

1. Acesse o pipeline no Spinnaker
2. Encontre o stage de "Bake (Manifest)" ou "Deploy (Helm)"
3. Verifique o campo "Chart Repository URL"
4. Use essa URL no comando `helm repo add`

## Contatos

- Time DevOps: Para obter URL do repositório
- Time Infraestrutura: Para configuração no cluster
- Documentação Spinnaker: Para detalhes do pipeline
