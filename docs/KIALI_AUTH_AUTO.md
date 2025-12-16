# Autenticação Automática do Kiali

## Visão Geral

O sistema de autenticação automática do Kiali foi implementado para suportar clusters que usam diferentes estratégias de autenticação (`anonymous`, `token`, `openid`, `openshift`).

## Problema Resolvido

Alguns clusters Kiali estão configurados com `auth.strategy: token` ou outros métodos de autenticação que não permitem acesso anônimo. Anteriormente, isso resultava em erro 401/403 ao tentar acessar a API do Kiali.

## Solução Implementada

### 1. Detecção Automática da Estratégia de Autenticação

Antes de fazer qualquer requisição ao Kiali, o sistema verifica a estratégia de autenticação consultando o endpoint `/api/config`:

```go
func getKialiAuthStrategy(kialiURL string) (string, error)
```

Este endpoint retorna a configuração do Kiali, incluindo a estratégia de autenticação:

```json
{
  "auth": {
    "strategy": "token"
  }
}
```

### 2. Criação Automática de Token

Se a estratégia não for `anonymous`, o sistema cria automaticamente um token de service account usando a API do Kubernetes:

```go
func createKialiToken(clientset kubernetes.Interface, namespace string) (string, error)
```

O token é criado através da API `TokenRequest` do Kubernetes:

```bash
# Equivalente ao comando kubectl:
kubectl -n istio-system create token kiali
```

### 3. Cache de Tokens

Para evitar criar tokens repetidamente, implementamos um sistema de cache com expiração:

- **Duração do Token**: 1 hora (3600 segundos)
- **Expiração do Cache**: 55 minutos (margem de segurança de 5 minutos)
- **Thread-Safe**: Usa `sync.RWMutex` para acesso concorrente

```go
type cachedToken struct {
    token     string
    expiresAt time.Time
}

var tokenCache = make(map[string]*cachedToken)
var tokenCacheMu sync.RWMutex
```

### 4. Aplicação do Token

O token é aplicado automaticamente no header `Authorization` das requisições HTTP:

```go
req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))
```

## Fluxo de Autenticação

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Cliente solicita grafo do Service Mesh                   │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Verificar estratégia de autenticação                     │
│    GET {kiali_url}/api/config                               │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  ▼
         ┌────────┴────────┐
         │  anonymous?     │
         └────────┬────────┘
                  │
        ┌─────────┴─────────┐
        │                   │
       SIM                 NÃO
        │                   │
        │                   ▼
        │    ┌─────────────────────────────────────┐
        │    │ 3. Verificar cache de token         │
        │    │    Chave: {cluster}:{namespace}     │
        │    └────────────┬────────────────────────┘
        │                 │
        │      ┌──────────┴──────────┐
        │      │  Token no cache?    │
        │      │  Ainda válido?      │
        │      └──────────┬──────────┘
        │                 │
        │       ┌─────────┴─────────┐
        │       │                   │
        │      SIM                 NÃO
        │       │                   │
        │       │                   ▼
        │       │    ┌──────────────────────────────┐
        │       │    │ 4. Criar novo token          │
        │       │    │    kubectl create token      │
        │       │    │    Duração: 1 hora           │
        │       │    └────────────┬─────────────────┘
        │       │                 │
        │       │                 ▼
        │       │    ┌──────────────────────────────┐
        │       │    │ 5. Armazenar no cache        │
        │       │    │    Expira em: 55 minutos     │
        │       │    └────────────┬─────────────────┘
        │       │                 │
        │       └─────────┬───────┘
        │                 │
        └─────────┬───────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. Fazer requisição ao Kiali                                │
│    Authorization: Bearer {token} (se necessário)            │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────┐
│ 7. Retornar dados do grafo ao cliente                       │
└─────────────────────────────────────────────────────────────┘
```

## Configuração

### Namespace Padrão

Por padrão, o sistema procura o service account `kiali` no namespace `istio-system`. Isso pode ser ajustado se necessário.

### Estratégias Suportadas

- ✅ **anonymous**: Sem autenticação (comportamento original)
- ✅ **token**: Token de service account do Kubernetes
- ✅ **openid**: Usa token de service account como fallback
- ✅ **openshift**: Usa token de service account como fallback

## Vantagens

1. **Zero Configuração**: Funciona automaticamente sem necessidade de alterar ConfigMaps do Kiali
2. **Multi-Cluster**: Cada cluster pode usar sua própria estratégia de autenticação
3. **Performance**: Cache de tokens evita chamadas desnecessárias à API
4. **Segurança**: Tokens com expiração controlada
5. **Compatibilidade**: Mantém suporte para clusters com autenticação anônima

## Logs de Diagnóstico

O sistema fornece logs detalhados para facilitar o troubleshooting:

```
[ServiceMesh] ✅ Usando Kiali via URL externa: https://kiali-prd.example.com/kiali/
[ServiceMesh] 🔐 Kiali requer autenticação (strategy: token), gerando token...
[ServiceMesh] ✅ Token criado com sucesso
[ServiceMesh] Usando autenticação com token
[ServiceMesh] ✅ Dados recebidos via URL externa: 42 nodes, 87 edges
```

## Troubleshooting

### Erro: "Erro ao criar token"

Verifique se:
- O service account `kiali` existe no namespace correto
- O usuário tem permissão para criar tokens (`kubectl auth can-i create serviceaccounts/token -n istio-system`)

### Erro: 401/403 ainda ocorre

Possíveis causas:
- Token expirado (verificar cache)
- Service account sem permissões necessárias no Kiali
- Configuração de RBAC do Kiali precisa incluir o service account

### Token não está sendo usado

Verifique os logs:
- Deve aparecer: `[ServiceMesh] 🔐 Kiali requer autenticação`
- Deve aparecer: `[ServiceMesh] Usando autenticação com token`

Se não aparecer, pode ser que:
- O endpoint `/api/config` não está retornando a estratégia corretamente
- O sistema detectou como `anonymous` incorretamente

## Referências

- [Kiali Authentication](https://kiali.io/docs/configuration/authentication/)
- [Kubernetes TokenRequest API](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-request-v1/)
- [Service Account Tokens](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection)
