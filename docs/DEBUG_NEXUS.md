# Debug e Teste - Nexus Values Integration

## 🔍 Logs Implementados

### Backend (Go)
Os seguintes logs foram adicionados para debug:

```go
[NexusHandler] Download request: release=X, version=Y, env=Z, type=W
[NexusHandler] Error getting client: ...
[NexusHandler] Download error: ...
[NexusHandler] Response contains error: ...
[NexusHandler] Download successful: X bytes

[Nexus] Downloading from URL: https://...
[Nexus] Using authentication for user: ...
[Nexus] Response status: 200
[Nexus] Error: ...
[Nexus] Downloaded X bytes successfully
```

### Frontend (TypeScript)
Console logs no navegador:

```javascript
[useNexus] Downloading values: {...}
[useNexus] Download response: {...}
[useNexus] Response contains error: ...
[useNexus] Download successful: X bytes
[useNexus] Download failed: ...

[NexusValuesDiffPanel] Starting comparison...
[NexusValuesDiffPanel] File1: {...}
[NexusValuesDiffPanel] File2: {...}
[NexusValuesDiffPanel] Compare result: {...}
[NexusValuesDiffPanel] Setting file contents...
[NexusValuesDiffPanel] Comparison successful!
```

## 🧪 Como Testar

### 1. Iniciar a Aplicação
```bash
./build/new-k8s-hpa web
```

### 2. Acessar a Interface
- URL: http://localhost:8080
- Login com token (se necessário)
- Navegar para aba "Nexus Values"

### 3. Configurar Nexus
1. Clicar em "Configurar Nexus"
2. Preencher:
   - URL Base: `https://nexus.viavarejo.com.br`
   - Repository: `workspace`
   - Usuário: seu_usuario_sso
   - Senha: sua_senha
   - Diretório temporário: `/tmp/k8s-hpa-nexus`
3. Clicar em "Testar Conexão"
4. Verificar resposta
5. Clicar em "Salvar"

### 4. Testar Download
1. Preencher Arquivo 1:
   - Release: nome-do-release
   - Versão: v1.0.0
   - Ambiente: prd
   - Tipo: base
2. Preencher Arquivo 2:
   - Release: nome-do-release
   - Versão: v1.0.0
   - Ambiente: sit
   - Tipo: base
3. Clicar em "Comparar"
4. Verificar logs no console do navegador (F12)
5. Verificar logs no terminal onde está rodando o backend

## 🐛 Troubleshooting

### Erro: "No configuration found"
**Causa:** Configuração não foi salva
**Solução:** 
1. Ir em "Configurar Nexus"
2. Preencher e salvar novamente
3. Verificar arquivo: `~/.k8s-hpa-manager/nexus-config.json`

### Erro: "Authentication failed"
**Causa:** Credenciais inválidas ou expiradas
**Solução:**
1. Verificar usuário e senha SSO
2. Tentar fazer login manual no Nexus via browser
3. Reconfigurar com novas credenciais

### Erro: "File not found"
**Causa:** Arquivo não existe no caminho especificado
**Solução:**
1. Verificar a URL construída nos logs:
   ```
   [Nexus] Downloading from URL: https://nexus.viavarejo.com.br/repository/workspace/RELEASE/VERSION/ENV/helm-values/TYPE-values.yaml
   ```
2. Tentar acessar a URL manualmente no browser
3. Verificar se o caminho está correto
4. Ajustar release/versão/ambiente/tipo conforme necessário

### Erro: "Request failed: dial tcp..."
**Causa:** Problemas de rede/conectividade
**Solução:**
1. Verificar se está na VPN (se necessário)
2. Testar conectividade: `curl -I https://nexus.viavarejo.com.br`
3. Verificar firewall/proxy

### Erro: "Unexpected status code: XXX"
**Causa:** Servidor retornou erro HTTP
**Solução:**
1. Verificar o código de status nos logs
2. Status 401: Problema de autenticação
3. Status 403: Sem permissão
4. Status 404: Arquivo não encontrado
5. Status 500+: Erro no servidor Nexus

## 📝 Checklist de Debug

- [ ] Aplicação iniciada (`./build/new-k8s-hpa web`)
- [ ] Console do navegador aberto (F12)
- [ ] Terminal do backend visível
- [ ] Nexus configurado e testado
- [ ] Formulário preenchido corretamente
- [ ] Logs do frontend verificados
- [ ] Logs do backend verificados
- [ ] URL construída está correta
- [ ] Credenciais são válidas
- [ ] Arquivos existem no Nexus

## 🔧 Verificação Manual da URL

Para testar se o arquivo existe, use curl:

```bash
curl -u "usuario:senha" \
  "https://nexus.viavarejo.com.br/repository/workspace/RELEASE/VERSION/ENV/helm-values/TYPE-values.yaml"
```

Substitua:
- `usuario:senha` - Suas credenciais SSO
- `RELEASE` - Nome do release
- `VERSION` - Versão (ex: v1.0.0)
- `ENV` - Ambiente (dev/sit/uat/hlg/prd)
- `TYPE` - Tipo (base/sit/prd/hlg/dev)

Exemplo:
```bash
curl -u "paulo.ribeiro:minhasenha" \
  "https://nexus.viavarejo.com.br/repository/workspace/meu-app/v1.2.3/prd/helm-values/base-values.yaml"
```

## 📊 Exemplo de Logs Esperados

### Sucesso Completo
```
[NexusHandler] Download request: release=meu-app, version=v1.0.0, env=prd, type=base
[Nexus] Downloading from URL: https://nexus.viavarejo.com.br/repository/workspace/meu-app/v1.0.0/prd/helm-values/base-values.yaml
[Nexus] Using authentication for user: paulo.ribeiro
[Nexus] Response status: 200
[Nexus] Downloaded 1234 bytes successfully
[NexusHandler] Download successful: 1234 bytes
```

### Falha de Autenticação
```
[NexusHandler] Download request: release=meu-app, version=v1.0.0, env=prd, type=base
[Nexus] Downloading from URL: https://nexus.viavarejo.com.br/repository/workspace/meu-app/v1.0.0/prd/helm-values/base-values.yaml
[Nexus] Using authentication for user: paulo.ribeiro
[Nexus] Response status: 401
[Nexus] Error: Authentication failed
[NexusHandler] Response contains error: Authentication failed
```

### Arquivo Não Encontrado
```
[NexusHandler] Download request: release=app-errado, version=v1.0.0, env=prd, type=base
[Nexus] Downloading from URL: https://nexus.viavarejo.com.br/repository/workspace/app-errado/v1.0.0/prd/helm-values/base-values.yaml
[Nexus] Using authentication for user: paulo.ribeiro
[Nexus] Response status: 404
[Nexus] Error: File not found: https://...
[NexusHandler] Response contains error: File not found: https://...
```

## 🎯 Próximos Passos

Se os logs mostram que está tudo correto mas ainda não funciona:

1. **Verificar estrutura real do Nexus**
   - Logar no Nexus via browser
   - Navegar até o repository `workspace`
   - Confirmar a estrutura de pastas

2. **Validar formato da URL**
   - Pode ser que o formato seja diferente
   - Pode ter prefixos/sufixos adicionais
   - Verificar com equipe responsável pelo Nexus

3. **Testar com arquivo conhecido**
   - Usar um arquivo que você sabe que existe
   - Verificar se o download funciona

4. **Ajustar o BuildURL se necessário**
   - Se o formato estiver errado, ajustar em `internal/pkg/nexus/client.go`
   - Função `BuildURL()`
