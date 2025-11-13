# Validação de PRs Helm diretamente na plataforma

Este documento descreve, em detalhes, a funcionalidade proposta para validar Pull Requests de Helm Charts usando apenas o link da PR e a tag desejada, sem exigir tokens adicionais ou permissões além das que o usuário já possui no navegador.

---

## 🎯 Objetivo

Permitir que um SRE:

1. Cole **o link da PR** (ex.: `https://github.com/org/repo/pull/123`).
2. Informe **a tag/release alvo** (ex.: `1.4.7`).
3. Veja na própria plataforma:
   - `helm get values --all` do release em produção.
   - `base-values.yaml` proposto na PR.
   - Diff lado a lado (YAML).
   - Comparação das tags (cluster vs PR).

Sem tokens, sem baixar arquivos manualmente, sem sair da interface.

---

## 🧱 Requisitos principais

1. **Frontend (browser)** deve buscar os arquivos da PR diretamente em `raw.githubusercontent.com`, aproveitando a sessão já autenticada do usuário.
2. **Backend** continua responsável por coletar o estado atual do release (via `helm get values --all <release> --namespace <ns>`).
3. **Comparação** (diff + validação de tag) ocorre no backend, garantindo que os dados não sejam manipulados no cliente.
4. **Auditoria**: logar quem executou a validação, tag informada, release comparado e hash resumido dos YAMLs (sem armazenar o conteúdo completo).

---

## 🔐 Considerações de segurança

- Nenhum token ou credencial corporativa precisa ser armazenado.
- O frontend apenas realiza um `fetch` para a URL raw, que já respeita CORS e utiliza os cookies/SSO atuais do usuário.
- Caso a organização bloqueie o domínio `raw.githubusercontent.com`, há fallback para upload manual.
- Todo conteúdo recebido é tratado como texto e validado antes de enviar ao backend.

---

## 🔄 Fluxo detalhado

1. **Usuário abre a página de validação**  
   - Seleciona o release/cluster desejado (ou digita manualmente).
   - Cola o link da PR e informa a tag alvo (opcional, se não extrairmos automaticamente).

2. **Frontend resolve metadados da PR**  
   - Usa a API pública do GitHub (`https://api.github.com/repos/.../pulls/<id>`) para:
     - Confirmar que a PR existe.
     - Identificar a branch (`head.ref`) e o repositório (`head.repo`).
   - Monta a URL raw do arquivo de values (ex.: `https://raw.githubusercontent.com/org/repo/<branch>/charts/app/base-values.yaml`).

3. **Download do `base-values.yaml`**  
   - `fetch` direto do navegador para o endereço raw.
   - Se o download falhar (sem permissão ou bloqueio), o usuário recebe instruções para usar o fallback (upload manual).

4. **Envio para o backend**  
   - O arquivo baixado é enviado ao backend via POST (`/api/v1/helm/validate`), junto com:
     - Release/namespace selecionados.
     - Tag informada.
     - Link da PR (para referência/auditoria).

5. **Backend executa `helm get values --all`**  
   - Com o release/namespace fornecidos, o backend coleta o estado atual.
   - Normaliza os YAMLs (ordenar chaves, remover comentários) e calcula:
     - Tag atual (a partir de annotations/labels ou `values.image.tag`).
     - Diff entre os dois YAMLs.

6. **Resposta para o frontend**  
   - JSON contendo:
     - `currentTag`, `prTag` (quando disponível) e status (match/mismatch).
     - `diffHtml` (string renderizada via `diff2html`) e `diffRaw` para export.
     - Alertas específicos (ex.: `image.tag` alterou, `replicas` reduziu, etc.).

7. **Frontend exibe painel**  
   - Mostra as tags em cards, highlight quando diferem.
   - Renderiza o diff lado a lado.
   - Disponibiliza botões para copiar, baixar o relatório ou abrir a PR original.

8. **Audit trail**  
   - Backend registra: usuário, release, namespace, PR link, timestamp, hash MD5 dos YAMLs e resultado da comparação.
   - Logs ficam disponíveis para auditoria futura.

---

## 🧰 Fallbacks previstos

| Cenário | Ação |
|---------|------|
| `raw.githubusercontent.com` bloqueado | Mostra botão “Upload manual” para selecionar o `base-values.yaml`. |
| PR sem arquivo esperado | Mensagem clara sugerindo o caminho correto (`charts/<app>/base-values.yaml`) e campo para digitar o path. |
| `helm get values` falha | Exibir erro do comando + guia para checar release/nome. |
| Diff enorme/falha de renderização | Oferecer download do diff como `.patch` para análise offline. |

---

## 🖥️ UI sugerida

```
[ Release ] [ Namespace ]        [ Validar ]
[ Link da PR ]  [ Tag alvo ]

┌─────────────────────────── Cards ───────────────────────────┐
│ Tag (cluster): 1.4.5    |   Tag (PR): 1.4.7 (⚠️ Diferente)  │
│ Image: repo/app:1.4.5   |   Image: repo/app:1.4.7          │
└─────────────────────────────────────────────────────────────┘

Diff YAML (side-by-side)   [Copiar diff] [Baixar relatório] [Abrir PR]
```

- Se a sidebar do Monitoring estiver recolhida, o seletor de release/namespace aparece no topo.
- Quando a validação acontece a partir de um card de HPA/Deployment, os campos já vêm preenchidos.

---

## 🧪 Testes necessários

1. PR pública vs PR privada.
2. Release inexistente (erro de `helm get`).
3. Diff com campos críticos (image.tag, replicas, env).
4. Upload manual (sem acesso ao raw).
5. Vários navegadores (Chrome/Edge corporativo).

---

## 🚧 Próximos passos

1. Implementar backend `/api/v1/helm/validate`.
2. Criar componente React “PRValidatorPanel”.
3. Adicionar log/audit.
4. Conectar painel aos cards de Monitoring/ConfigMaps para atalhos contextuais.

Com esse fluxo, o SRE continua aprovando via GitHub (mesmo link de sempre), mas ganha uma validação confiável e rápida dentro da própria plataforma – sem guardar tokens e mantendo o compliance atual.

