# 🔧 Correção do Bug de Seleção de Cluster

**Problema:** Frontend enviava `cluster.name` em vez de `cluster.context`
**Erro:** `Failed to get client for cluster akspriv-faturamento-prd: context "akspriv-faturamento-prd" does not exist`

---

## 🐛 Causa Raiz

O frontend estava usando `cluster.name` (ex: `akspriv-faturamento-prd`) para fazer requisições, mas a API precisa do `cluster.context` (ex: `akspriv-faturamento-prd-admin`).

### Código Antes (Bugado)
```javascript
// Linha 416 - index.html
onclick="selectCluster('${cluster.name}')"  // ❌ Errado
```

### Código Depois (Corrigido)
```javascript
// Linha 416 - index.html  
onclick="selectCluster('${cluster.context}')"  // ✅ Correto
```

---

## 🔧 Mudanças Aplicadas

### Arquivo: `internal/web/static/index.html`

1. **Renderização de clusters** (linha 416):
   - **Antes:** `onclick="selectCluster('${cluster.name}')"`
   - **Depois:** `onclick="selectCluster('${cluster.context}')"`

2. **Comparação de seleção** (linha 415):
   - **Antes:** `${state.selectedCluster === cluster.name ? 'selected' : ''}`
   - **Depois:** `${state.selectedCluster === cluster.context ? 'selected' : ''}`

3. **Parâmetro da função** (linha 436):
   - **Antes:** `function selectCluster(clusterName)`
   - **Depois:** `function selectCluster(clusterContext)`

---

## ✅ Validação

### Teste 1: API com context correto
```bash
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/namespaces?cluster=akspriv-faturamento-prd-admin&showSystem=false"
```
**Resultado:** ✅ 200 OK - 3 namespaces retornados

### Teste 2: Estrutura do cluster
```bash
curl -H "Authorization: Bearer poc-token-123" \
  http://localhost:8080/api/v1/clusters | jq '.data[0]'
```
**Resultado:**
```json
{
  "context": "akspriv-abastecimento-hlg-admin",
  "name": "akspriv-abastecimento-hlg",
  "status": "checking..."
}
```
✅ API retorna ambos `name` e `context`

---

## 🎯 Comportamento Esperado

1. **Usuário clica em cluster** → Frontend envia `context` (ex: `akspriv-faturamento-prd-admin`)
2. **API recebe context** → Usa para conectar ao Kubernetes
3. **Namespaces carregam** → Lista exibida corretamente
4. **HPAs carregam** → Quando namespace selecionado

---

## 📊 Status Final

- ✅ **Bug corrigido** no frontend
- ✅ **Rebuild concluído** (81MB)
- ✅ **Servidor rodando** na porta 8080
- ✅ **API funcionando** com context correto
- ✅ **Testes validados** via curl

---

## 🚀 Como Testar no Browser

1. Acesse: http://localhost:8080
2. Login com token: `poc-token-123`
3. Clique em qualquer cluster
4. **Resultado esperado:** Namespaces devem carregar sem erro

**Se ainda aparecer erro**, use F12 (DevTools) e verifique:
- **Network tab:** Request para `/api/v1/namespaces` deve ter `cluster=...-admin`
- **Console:** Não deve haver erros de JavaScript

---

**Correção aplicada:** 16/10/2025 12:11
**Tempo para fix:** ~10 minutos
**Arquivos modificados:** 1 (index.html)
**Linhas alteradas:** 3
