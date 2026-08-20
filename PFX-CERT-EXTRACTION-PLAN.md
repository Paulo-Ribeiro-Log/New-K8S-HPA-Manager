# Plano: Extração de Certificados .pfx

**Status:** 📝 planejamento — nenhuma fase iniciada, nenhum código escrito.

## Motivação

Usuário pediu ajuda pra instalar manualmente um certificado cuja fonte era um `.pfx` (PKCS#12) —
precisou extrair `tls.crt`/`tls.key` via `openssl` na mão antes de conseguir usar o fluxo Manual
já existente (`CertificateRenewModal.tsx`/Upload de `CertificatesTab.tsx`). Esse passo manual é
repetitivo o suficiente pra valer embutir na aplicação.

## Requisitos confirmados com o usuário

- **Nome do certificado**: campo livre, fornecido pelo usuário no momento da extração (sugestão
  automática = nome do arquivo `.pfx` sem extensão, editável) — não derivado do CN do certificado.
- **Uma pasta por extração**, no mesmo padrão já usado no Backup Manual de certificados
  (`ManualBackupStore`, `internal/certificates/manual_backup.go`) — `<nome>/<timestamp>/`,
  múltiplas extrações sob o mesmo nome empilham (diário), nunca sobrescreve.
- **Extrair TODOS os elementos do certificado**: leaf + chain (intermediárias/raiz, se presentes
  no `.pfx`) e a `key`. **Ajuste confirmado com o usuário**: no uso real desta empresa, a chain já
  fica concatenada DENTRO do `tls.crt` (é assim que o Secret K8s espera mesmo) — não faz sentido
  separar leaf/chain em arquivos diferentes. Saída final é só **dois arquivos**, mesma convenção
  de nome já usada em `ManualBackupStore`: `tls.crt` (leaf + chain já concatenados, na ordem
  leaf-primeiro) e `tls.key`.
- **Campo senha**: confirmado ponta a ponta — formulário recebe a senha do `.pfx` (`type="password"`),
  vai até o backend numa única requisição (multipart), é usada uma única vez pra decodificar via
  `pkcs12.DecodeChain` e **nunca é persistida** em lugar nenhum (não entra em `metadata.json`, não
  fica em disco, não é logada).

## Decisão de arquitetura: processamento no backend (Go)

Decorre diretamente do requisito "uma pasta é criada" — isso é uma operação de disco, só faz
sentido rodar no servidor (mesmo raciocínio de `ManualBackupStore`/`RollbackStore`, que também são
100% backend). Consequência aceita: a senha do `.pfx` transita até o backend numa requisição —
**não é uma exposição nova**: o mesmo trust boundary já existe hoje pro conteúdo puro de
`tls.key` no fluxo Manual/Upload/`ManualBackupStore`/`RollbackStore`, que já recebem e persistem
chave privada em texto claro no disco do servidor. A senha do `.pfx` em si nunca é persistida —
usada uma única vez pra decodificar, descartada em seguida.

## Design

### 1. Backend — extração PKCS#12 (`internal/certificates/pfx_extract.go`, novo)

Nova dependência: `software.sslmate.com/src/go-pkcs12` (fork ativamente mantido). A alternativa já
indiretamente vendorizada, `golang.org/x/crypto/pkcs12`, é legada e **falha em `.pfx` modernos com
criptografia AES** (só suporta RC2/3DES) — mesma classe de problema já vista nesta investigação
com chaves `ENCRYPTED PRIVATE KEY`. Adicionar via `go get software.sslmate.com/src/go-pkcs12 &&
go mod vendor` (conferir `git diff vendor/` depois — CLAUDE.md já documenta que `go mod vendor`
pode mexer em coisas inesperadas, ver nota sobre o patch do go-rod).

```go
// ExtractPFX decodifica um .pfx (PKCS#12) e devolve tlsCrt (leaf + chain já concatenados, ordem
// leaf-primeiro — mesma convenção que um Secret kubernetes.io/tls espera) e tlsKey em PEM, mais o
// leaf já parseado (pra popular metadata sem reparsear).
func ExtractPFX(pfxBytes []byte, password string) (tlsCrt, tlsKey []byte, leaf *x509.Certificate, err error)
```

- `pkcs12.DecodeChain(pfxBytes, password)` → `(privateKey, cert, caCerts, err)`.
- Serializa `cert` (leaf) e concatena com `caCerts` (intermediárias/raiz, se houver) → `tlsCrt`
  único, leaf primeiro. Serializa `privateKey` como PKCS#8 **sem senha**
  (`-----BEGIN PRIVATE KEY-----`, nunca criptografado — resolve de saída o problema de "ENCRYPTED
  PRIVATE KEY" pro resultado desta extração).
- Erros do `go-pkcs12` (senha errada, PFX corrompido) propagados como estão — mensagem já é clara
  o suficiente sem precisar de tradução.

### 2. Backend — storage (`PFXExtractStore`, `internal/certificates/pfx_store.go`, novo)

Espelha `ManualBackupStore` quase 1:1 — mesmos nomes de método, mesma forma, **mesmos nomes de
arquivo** (`tls.crt`/`tls.key`/`metadata.json`), só troca a base dir:

- Base dir: `~/.k8s-hpa-manager/pfx-extracted-certs/`.
- `Save(name, comment, originalFilename string, tlsCrt, tlsKey []byte, leaf *x509.Certificate) (PFXExtractInfo, error)`
  — grava em `<baseDir>/<name>/<timestamp>/{tls.crt, tls.key, metadata.json}`.
- `metadata.json` (`PFXExtractInfo`): nome, `extract_id` (timestamp), subject/issuer/serial/
  `not_after` do leaf (mesmos helpers de `parser.go` — `certSubjectDisplayName`,
  `formatSerialNumber`), `chain_length` (via `parsePEMChain(tlsCrt)`, quantos certs saíram no
  total — útil pro resumo mostrar "leaf + N intermediárias"), nome original do arquivo `.pfx`,
  comentário opcional. **Nunca inclui a senha.**
- `ListNames() ([]string, error)` — nomes com ao menos 1 extração salva.
- `List(name string) ([]PFXExtractInfo, error)` — extrações de um nome, mais recente primeiro.
- `Get(name, extractID string) (tlsCrt, tlsKey []byte, meta PFXExtractInfo, err error)`.
- `UpdateComment(name, extractID, comment string) error`.
- `Delete(name, extractID string) error` — destrutivo, mesma semântica de confirmação de
  `ManualBackupStore.Delete`.
- Reaproveitar `validPathComponent` (já existe em `manual_backup.go`/`rollback.go`) pra validar
  `name`/`extractID` antes de tocar o filesystem.

Dado o formato final ser idêntico ao `ManualBackupStore` (mesmos 3 arquivos, mesma estrutura de
pasta), dá pra considerar unificar os dois numa única store parametrizada por base dir no futuro
— não faz agora pra manter o escopo desta feature pequeno, mas vale revisitar se os dois começarem
a divergir por duplicação de código real.

### 3. Backend — handler + rotas (`internal/web/handlers/certificates_pfx.go`, novo)

- `POST /api/v1/certificates/pfx/extract` — multipart (`file`, `password`, `name`, `comment`
  opcional). `ExtractPFX` → `PFXExtractStore.Save` → devolve `PFXExtractInfo` **junto com
  `tls_crt`/`tls_key`** na mesma resposta, pra já poder popular os campos na hora sem um segundo
  round-trip.
- `GET /api/v1/certificates/pfx/names` — `ListNames`.
- `GET /api/v1/certificates/pfx/:name` — `List`.
- `GET /api/v1/certificates/pfx/:name/:extractId` — PEMs + metadata de uma extração (`Get`) — usado
  pelo picker de origem.
- `PUT /api/v1/certificates/pfx/:name/:extractId` — editar comentário.
- `DELETE /api/v1/certificates/pfx/:name/:extractId` — remover.
- Todas atrás de `rbacMiddleware.RequireSREGroup()` — mesma proteção do resto do fluxo manual de
  certificados, já que recebe/gera material de chave privada.
- **Cuidado já documentado no CLAUDE.md a não repetir**: `useCertificates.ts` monta URLs
  interpolando parâmetros direto na string sem `encodeURIComponent`, o que já causou um bug real
  (404 silencioso em nomes com caracteres especiais, caso dos clusters EKS/ARN). `name` aqui é
  texto livre digitado pelo usuário — usar `encodeURIComponent` em todas as chamadas do frontend
  desde o início.

### 4. Frontend

- **Novo modal** `PFXExtractModal.tsx`: input de arquivo `.pfx`, campo texto "Nome do certificado"
  (sugestão automática = nome do arquivo sem extensão, editável), campo senha (`type="password"`),
  comentário opcional. Botão "Extrair" → `POST /pfx/extract` → resumo (Subject/Issuer/Expira do
  leaf, `chain_length`) + confirmação salva.
  - Entry point: botão novo em `CertificatesTab.tsx`, ao lado de "Upload"/"Atualização em Massa"
    (ex: "Extrair de .pfx").
- **Extensão do `CertificateSourcePickerModal.tsx`** (já existe, 2 abas hoje — Rollback e Backup
  Manual, `type Tab = "rollback" | "manual"`, `internal/web/frontend/src/components/
  CertificateSourcePickerModal.tsx`): vira `type Tab = "rollback" | "manual" | "pfx"` — 3ª aba
  "Extraído de PFX", mesmo padrão master-detail (lista de nomes → extrações por nome → aplicar) e
  mesma busca client-side já usada nas outras duas. "Aplicar" preenche `tls.crt`/`tls.key` com o
  resultado da extração escolhida — igual às outras duas fontes, sem nenhum campo a mais.
  - Esse picker já está integrado nos 2 modais manuais que importam (Secrets e Certificados TLS) —
    a 3ª aba fica disponível nos dois automaticamente, sem trabalho extra de integração.

### 5. Fluxo completo (ponta a ponta)

1. Usuário abre "Extrair de .pfx" em Certificados TLS.
2. Seleciona o arquivo, dá um nome (ex: `via-tls-prod-2026`), digita a senha, extrai.
3. App decodifica, monta `tls.crt` (leaf+chain) e `tls.key`, salva em disco, mostra resumo.
4. Na hora de instalar (Secrets → "Atualizar Certificado", ou Certificados TLS → Upload), abre o
   picker de origem, aba "Extraído de PFX", escolhe pelo nome — `tls.crt`/`tls.key` já vêm
   preenchidos.
5. Segue o fluxo de instalação já existente, sem nenhuma mudança nele.

## Fora de escopo (por ora)

- `.pfx` com múltiplos pares cert+chave no mesmo arquivo (PKCS#12 permite mais de 1 alias) —
  `go-pkcs12.DecodeChain` assume 1 par; se aparecer um caso real com múltiplos, tratar depois.
- Instalação direta no Secret a partir do resultado da extração, pulando o picker — atalho
  possível no futuro, mas o picker já cobre o caso de uso principal reaproveitando 100% da UI
  existente.

## Riscos / pontos a validar antes de fechar como concluído

- `go mod vendor` depois de adicionar `go-pkcs12` — conferir `git diff vendor/` inteiro antes de
  commitar, não só o pacote novo (CLAUDE.md já documenta um precedente de vendor mexendo em coisa
  inesperada).
- Nenhum teste ao vivo possível sem um `.pfx` real — gerar um de teste antes de considerar pronto:
  `openssl pkcs12 -export -in tls.crt -inkey tls.key -out teste.pfx` (a partir de um par já usado
  nos testes existentes de `internal/certificates`, ex: os helpers `genCert`/`concatPEM` de
  `validate_test.go`).
- Confirmar que `pkcs12.DecodeChain` do `go-pkcs12` aceita `.pfx` gerado tanto por OpenSSL quanto
  por exportação do Windows/IIS (formatos historicamente um pouco diferentes na prática) — não
  assumir sem testar pelo menos um exemplar real de cada origem, se disponível.
