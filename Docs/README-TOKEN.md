# GitHub Token Storage

## Localização do Token

O token do GitHub para operações de API está armazenado de forma segura em:

```
~/.config/k8s-hpa-manager/.gh-token
```

## Permissões

O arquivo possui permissões `600` (somente leitura/escrita pelo proprietário) e o diretório possui `700`.

## Como usar o token

### Manualmente
```bash
# Ler o token
cat ~/.config/k8s-hpa-manager/.gh-token

# Usar com gh CLI
gh auth login --with-token < ~/.config/k8s-hpa-manager/.gh-token

# Usar em scripts
GITHUB_TOKEN=$(cat ~/.config/k8s-hpa-manager/.gh-token)
curl -H "Authorization: token $GITHUB_TOKEN" https://api.github.com/...
```

### Em Scripts Go
```go
tokenPath := filepath.Join(os.Getenv("HOME"), ".config", "k8s-hpa-manager", ".gh-token")
tokenBytes, err := os.ReadFile(tokenPath)
if err != nil {
    return "", err
}
token := strings.TrimSpace(string(tokenBytes))
```

## Segurança

✅ **O que está protegido:**
- Token salvo em local não versionado (`.gitignore`)
- Permissões restritas ao usuário atual
- Não exposto em commits ou PRs

⚠️ **Importante:**
- NUNCA compartilhe o conteúdo do arquivo `.gh-token`
- NUNCA commite o token no repositório
- Revogue o token no GitHub se houver suspeita de vazamento

## Revogação

Se precisar revogar o token:
1. Acesse: https://github.com/settings/tokens
2. Encontre o token na lista
3. Clique em "Delete" ou "Revoke"
4. Gere um novo token se necessário
5. Atualize o arquivo: `echo "NOVO_TOKEN" > ~/.config/k8s-hpa-manager/.gh-token`
