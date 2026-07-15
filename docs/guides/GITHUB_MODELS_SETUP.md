# Usando GitHub Models como alternativa ao Gemini Vertex AI

## 📋 Problema

O Gemini Vertex AI corporativo depende de permissão IAM (`roles/aiplatform.user` ou equivalente,
incluindo a permissão específica `aiplatform.endpoints.predict`) concedida por um administrador
de Cloud/IT no projeto GCP. Sem essa aprovação, qualquer tentativa de análise retorna:

```
permissão negada (403) — credenciais sem acesso ao projeto '<projeto>'.
"Permission 'aiplatform.endpoints.predict' denied on resource '...'"
```

Isso não é um bug de configuração — é uma permissão que precisa ser solicitada e aprovada fora
da ferramenta. Enquanto isso não acontece (ou se a organização não permite acesso a vendors de
IA externos), o **GitHub Models** é uma alternativa que funciona hoje: é a própria API de IA do
GitHub, acessível com o mesmo Personal Access Token (PAT) que a organização já libera pra uso
com `git`/`gh`/GitHub Releases — sem precisar de aprovação nova, projeto GCP, IAM ou SSO
corporativo separado.

O provider **"OpenAI"** já existente na ferramenta aceita um endpoint customizado (Base URL) e é
100% compatível com o formato de resposta do GitHub Models — não é necessário nenhum provider
novo, só apontar pro endpoint certo.

---

## ✅ Passo a passo

### 1. Confirmar que você está autenticado no `gh` CLI

```bash
gh auth status
```

Se aparecer mais de uma conta, a marcada como `Active account: true` é a que o comando abaixo
vai usar por padrão.

### 2. Obter o token (vai no campo "OpenAI API Key")

```bash
gh auth token
```

Isso imprime um PAT (`gho_...` ou `ghp_...`). Pra pegar o token de uma conta específica (não a
ativa):

```bash
gh auth token --user <nome-da-conta>
```

### 3. Descobrir quais modelos sua organização realmente libera

O catálogo público do GitHub Models lista todos os modelos que **existem**, não os que sua
organização tem **acesso** — o acesso real varia por org/entitlement de Copilot. O jeito
confiável é testar direto:

```bash
GH_TOKEN=$(gh auth token)

# Lista o catálogo completo (só mostra o que existe, não o que você tem acesso)
curl -s https://models.github.ai/catalog/models | python3 -m json.tool | grep '"id"'

# Testa um modelo específico de verdade — só assim você sabe se tem acesso
curl -s https://models.github.ai/inference/chat/completions \
  -H "Authorization: Bearer $GH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"openai/gpt-4.1","messages":[{"role":"user","content":"oi"}]}'
```

- Resposta com `{"choices":[...]}` → o modelo funciona, pode usar.
- Resposta com `{"error":{"code":"no_access",...}}` → tente outro modelo do catálogo.

Exemplo real (validado em julho/2026): `openai/gpt-4.1` e `microsoft/Phi-4` funcionaram,
`openai/gpt-4o-mini` retornou `no_access`. Isso varia por organização — sempre valide antes de
configurar.

### 4. Preencher na ferramenta

Aba **AI Diagnostics → Configurações de IA → seção OpenAI**:

| Campo | Valor |
|---|---|
| OpenAI API Key | resultado de `gh auth token` |
| Base URL | `https://models.github.ai/inference/chat/completions` |
| Modelo OpenAI | um modelo confirmado no passo 3 (ex: `openai/gpt-4.1`) |

Quando a **Base URL** está preenchida, o campo de modelo vira texto livre (a lista fixa de
modelos oficiais da OpenAI some, já que os IDs do GitHub Models são diferentes, ex:
`openai/gpt-4.1`, `microsoft/Phi-4`).

Marque **"OpenAI"** como provider preferido e clique em salvar.

---

## ⚠️ Observações

- O `gh auth token` pode ser revogado/expirar como qualquer PAT. Se a integração parar de
  funcionar do nada, rode `gh auth token` de novo e cole o valor atualizado.
- O botão "Validar Formato" da aba OpenAI **não faz chamada real** à API (só valida tamanho
  mínimo da chave) — isso é intencional (chaves OpenAI/GitHub têm formatos variados). Pra
  confirmar que a integração funciona de verdade, rode uma análise real na ferramenta ou use o
  teste via `curl` do passo 3.
- Esse caminho não substitui pedir a permissão IAM do Vertex AI se sua organização realmente
  precisar do Vertex corporativo no longo prazo — é um caminho prático pra ter IA funcionando
  **hoje**, sem depender de aprovação de Cloud/IT.
