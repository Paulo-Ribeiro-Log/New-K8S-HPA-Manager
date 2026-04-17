# AWX Cert Integration — Certificados TLS

## Objetivo
Adicionar switch no modal "Upload de Certificado" da aba Certificados TLS para escolher entre upload manual ou renovação via AWX.

## Checklist

### Fase 1 — Extrair AWXCertForm
- [x] Criar `AWXCertForm.tsx` com o conteúdo do modal AWX (sem wrapper Dialog)
  - Props: `cluster`, `namespace`, `onSuccess?: () => void`
  - Lógica interna: auto-fill subs_env/resource_group, survey cert_tls, SSE stream, botões Instalar/Renovar
- [x] Refatorar `AWXCertModal.tsx` para usar `AWXCertForm` internamente (sem quebrar NamespacesTab)

### Fase 2 — Integrar em CertificatesTab
- [x] Importar `AWXCertForm` e `apiClient.getAWXStatus`
- [x] Adicionar estado `awxConfigured` + check no mount
- [x] Adicionar estado `uploadMode: "manual" | "awx"`
- [x] Adicionar Switch no header do modal de upload (visível só quando awxConfigured)
- [x] Renderizar form manual quando `uploadMode === "manual"` (inalterado)
- [x] Renderizar `AWXCertForm` quando `uploadMode === "awx"`
  - Pré-preencher `cluster` e `namespace` do certificado selecionado quando disponível

### Fase 3 — Build e validação
- [x] `./rebuild-web.sh -b` sem erros TypeScript
- [ ] Hard refresh + testar switch manual↔AWX
- [ ] Commit
