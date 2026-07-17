# Plano: Verificação de Espaços em Branco em Valores Base64 (aba Secrets)

← ✅ implementado (ver "Status da implementação" no fim do documento)

## Problema

Secrets do K8s guardam valores em base64. É comum um valor decodificado ter espaço/tab/newline
sobrando no início ou no fim — o clássico `echo "senha" | base64` (sem `-n`) deixa um `\n` no fim,
ou um copy-paste de um editor/gerenciador de senha traz um espaço solto na borda. Isso é **invisível**
tanto na string base64 quanto, muitas vezes, no valor decodificado (a olho nu não dá pra ver um `\n`
ou um espaço extra no fim). O resultado é autenticação falhando silenciosamente, comparação de string
quebrando, etc. — um bug de diagnóstico caro porque "o valor parece certo".

## Decisões (confirmadas com o usuário)

1. **Gatilho**: menu de contexto **dentro do editor Monaco** (botão direito numa linha `chave: valor`
   do bloco `data:`) — não na lista lateral de secrets. Reaproveita o mecanismo de `editor.addAction()`
   já usado pelos atalhos Ctrl+Shift+D/E (decode/encode) documentados no `CLAUDE.md`.
2. **Escopo da detecção**: só espaço/tab/newline/CR **na borda** (início ou fim) do valor decodificado.
   Não sinaliza espaço duplo interno nem CRLF no meio do valor — risco de falso-positivo em valores que
   legitimamente têm espaços internos (connection strings, JSON, certificados PEM multi-linha).

## Onde isso entra no código existente

- `internal/web/frontend/src/components/SecretsTab.tsx` — já tem toda a base necessária:
  - `isValidBase64()` (validação via regex + round-trip `atob`/`btoa`)
  - `base64DecodeUtf8()` / `base64EncodeUtf8()` (decode/encode seguro para UTF-8)
  - `handleToggleDecode()` — já percorre linha a linha o bloco `data:` do YAML (`key: value`),
    decodificando cada valor. Essa é a lógica de parsing a ser **reaproveitada**, não duplicada.
- `internal/web/frontend/src/components/MonacoYamlEditor.tsx` — dono do editor Monaco e dos
  `editor.addAction()` existentes (Ctrl+Shift+D/E, Ctrl+Shift+Z/X). A nova ação de verificação entra
  aqui, ao lado das outras.
- **Nenhuma mudança de backend é necessária.** `internal/web/handlers/secrets.go` (`Get`) já devolve o
  YAML completo do Secret com os valores base64 sem redigir (`SecretManifest.YAML`) — a decodificação e
  a checagem de espaços acontecem inteiramente no browser, sobre o texto já carregado no editor.

## Design

### 1. Utilitário de detecção (novo, extraído — evita duplicar a lógica de parsing do `handleToggleDecode`)

Novo arquivo `internal/web/frontend/src/lib/secretWhitespaceCheck.ts`:

```ts
export type WhitespaceIssue = "leading" | "trailing";

export interface SecretKeyWhitespaceResult {
  key: string;
  lineNumber: number;    // linha no texto do editor (1-based), para a decoration do Monaco
  issues: WhitespaceIssue[];
  decodedPreview: string; // valor decodificado com whitespace visível (\n, \t, espaço) para o hover/toast
}

// Reaproveita o mesmo parsing linha-a-linha de `data:` que handleToggleDecode já faz em SecretsTab.tsx
// (extrair essa função de lá para cá, chamada pelos dois lugares).
export function parseSecretDataLines(yaml: string): { key: string; rawValue: string; lineNumber: number }[];

export function detectWhitespaceIssues(decoded: string): WhitespaceIssue[] {
  const issues: WhitespaceIssue[] = [];
  if (/^\s/.test(decoded)) issues.push("leading");
  if (/\s$/.test(decoded)) issues.push("trailing");
  return issues;
}

export function scanSecretForWhitespaceIssues(yaml: string): SecretKeyWhitespaceResult[];
```

`SecretsTab.tsx` passa a importar `parseSecretDataLines` desse módulo em vez de ter a lógica inline —
`handleToggleDecode` e a nova ação de verificação compartilham o mesmo parser.

### 2. Ação no Monaco (`MonacoYamlEditor.tsx`)

- `editor.addAction({ id: "check-base64-whitespace", label: "Verificar espaços em branco (base64)", contextMenuGroupId: "modification", run: (ed) => {...} })`
- **Escopo automático via context key**: a ação só deve aparecer no menu quando o YAML aberto é um
  `kind: Secret` — usar `editor.createContextKey("isSecretYaml", ...)`, atualizado a cada mudança de
  conteúdo (mesmo padrão que já detecta linguagem/kind em outros pontos do editor), e `precondition:
  "isSecretYaml"` na ação. Isso evita o item aparecer (sem função) no editor de ConfigMaps/outros YAMLs.
- Ao rodar: chama `scanSecretForWhitespaceIssues(editor.getValue())`, aplica as decorations (passo 3),
  e dispara um toast-resumo (reaproveitando o padrão de toast já usado por `handleToggleDecode`):
  `"3 de 12 chaves com espaço em branco suspeito: DB_PASSWORD (fim), API_TOKEN (início), ..."`.
  Zero resultados → toast neutro `"Nenhum espaço em branco suspeito encontrado"`.

### 3. Indicação visual — decorations do Monaco

Para cada chave com problema, `editor.createDecorationsCollection([...])` na linha correspondente:
- `glyphMarginClassName` — ícone de aviso (⚠) na margem esquerda (requer `glyphMargin: true` nas
  options do editor — checar se já está habilitado, senão habilitar).
- `className` — leve destaque de fundo na linha (nova classe CSS, tom âmbar, compatível com os dois
  temas — seguir o padrão `@media (prefers-color-scheme: dark)` / `:root[data-theme]` já usado no
  restante do app).
- `hoverMessage` — texto explicando o problema em pt-BR, ex: *"Espaço em branco no final do valor
  decodificado — comum quando o secret foi criado com `echo` sem `-n`."*

As decorations ficam **até a próxima ação do usuário que invalide o estado**: novo toggle de
decode/encode, edição manual do texto, ou nova rodada do scan. Não há necessidade de persistir nada —
puramente estado do editor em memória, limpo em `onChange`.

### 4. Indicador na lista lateral (opcional, fase 2)

Badge pequeno (mesmo estilo pill dos badges de expiração TLS já existentes na sidebar, ou
`Badge variant="destructive"` do shadcn, seguindo o padrão "orfão" do FinOps) ao lado do nome do
Secret, mas **só depois que o usuário já rodou o scan nessa sessão** para aquele Secret — não há scan
automático em background (custo desnecessário, e a decisão já tomada foi "gatilho manual via menu de
contexto"). Estado: `whitespaceIssueCounts: Record<string, number>` em `SecretsTab.tsx`, populado pelo
callback disparado pela ação do Monaco. Depende de expor um callback `onWhitespaceScan` de
`MonacoYamlEditor` para `SecretsTab`.

## Fora de escopo (v1)

- Correção automática (trim automático + reencode) — só detecção/indicação visual, igual ao pedido.
  Poderia entrar como ação de follow-up ("Remover espaços e reencode") numa fase 2, mas não pedido agora.
- Scan automático ao abrir o Secret (sem clique) — decisão explícita de manter sob demanda via menu de
  contexto, evita custo de decode/scan em Secrets grandes só por terem sido abertos.
- Detecção de espaço duplo interno / CRLF no meio do valor — descartado por risco de falso-positivo
  (ver "Decisões" acima).
- Badge na lista lateral (passo 4) é opcional/fase 2 — o pedido original cobre gatilho + indicação
  visual, que a decoration do Monaco já satisfaz sozinha.

## Mapa de arquivos (previsto)

| Arquivo | O quê |
|---|---|
| `internal/web/frontend/src/lib/secretWhitespaceCheck.ts` (novo) | `parseSecretDataLines`, `detectWhitespaceIssues`, `scanSecretForWhitespaceIssues` |
| `internal/web/frontend/src/components/MonacoYamlEditor.tsx` | Nova `editor.addAction` + context key `isSecretYaml` + decorations + hover message |
| `internal/web/frontend/src/components/SecretsTab.tsx` | `handleToggleDecode` passa a importar `parseSecretDataLines` do novo módulo em vez de duplicar; (fase 2) estado `whitespaceIssueCounts` + badge na sidebar |

## Validação planejada

Sem mudança de backend → sem teste Go novo. Frontend: `tsc --noEmit` + teste manual via
`./rebuild-web.sh -b` contra um Secret real com um valor propositalmente sujo (`echo "x" | base64`,
sem `-n`) para confirmar que a ação aparece só em Secrets, a decoration marca a linha certa, e o hover
mostra a mensagem certa nos dois temas (claro/escuro).

## Status da implementação

Implementado como desenhado, com dois desvios conscientes em relação ao design original:

- **`handleToggleDecode` (`SecretsTab.tsx`) não foi refatorado** para importar `parseSecretDataLines` —
  é lógica de produção já testada (toggle Encode/Decode) e a duplicação do regex de parsing é pequena;
  o risco de regressão numa feature existente não valia o ganho de DRY. `parseSecretDataLines` vive só
  em `secretWhitespaceCheck.ts`, usado apenas pela nova feature.
- **Sem variante clara/escura na decoration CSS** — o Monaco nesta aba usa `theme="vs-dark"` fixo
  (hardcoded em `MonacoYamlEditor.tsx`, independente do tema da aplicação), então `.monaco-whitespace-issue-line`/
  `-glyph` em `index.css` não precisam de `@media (prefers-color-scheme)`.

Indicador na lista lateral (seção 4, fase 2) **não implementado** — o pedido original (gatilho +
indicação visual) já fica satisfeito só com a decoration no editor.

Validado nesta rodada: `go build ./...`, `tsc --noEmit`, `eslint` nos arquivos tocados, e
`./rebuild-web.sh -b` com servidor reiniciado com sucesso (processo novo pós-build confirmado). **Não
testado clicando de verdade no browser** — sem ferramenta de automação de browser disponível neste
ambiente na hora da implementação. Teste manual recomendado: abrir um Secret com um valor gerado via
`echo "x" | base64` (sem `-n`), botão direito no editor → "Verificar espaços em branco (base64)" →
confirmar que a linha certa fica marcada e o hover mostra a prévia com `\n` visível.
