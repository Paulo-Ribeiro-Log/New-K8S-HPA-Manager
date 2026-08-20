// pemUtils — helpers puramente client-side pra inspecionar conteúdo PEM colado/gerado nos
// campos de texto de tls.crt (Secrets → "Atualizar Certificado", Certificados TLS → Upload/
// Atualização em Massa). Motivação: essas caixas de texto são `h-32` (~128px, poucas linhas
// visíveis) sem nenhum indicador de quantos certificados estão no campo — um bundle
// leaf+intermediária+raiz tem ~60-90 linhas de PEM, então só a 1ª certidão aparece sem rolar,
// indistinguível visualmente de "a chain não veio" mesmo quando ela está lá. Ver CLAUDE.md,
// seção "Extração de Certificado .pfx (PKCS#12)".

/** Conta quantos blocos "-----BEGIN CERTIFICATE-----" existem no texto colado/gerado. */
export function countPemCertificates(pem: string): number {
  if (!pem) return 0;
  const matches = pem.match(/-----BEGIN CERTIFICATE-----/g);
  return matches ? matches.length : 0;
}
