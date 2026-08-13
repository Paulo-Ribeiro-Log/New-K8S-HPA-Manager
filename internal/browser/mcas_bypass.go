package browser

import "github.com/go-rod/rod"

// SubmitMCASHiddenForm detecta e submete o form oculto que o MCAS (Microsoft Cloud App Security)
// usa na tela "Monitored access" / "Use Edge Browser for better performance..." (redirect
// intermediário obrigatório de teams.microsoft.com → teams.microsoft.com.mcas.ms em tenants com
// essa política ativa) pra completar o redirect OIDC/SAML.
//
// Bug real corrigido (achado ao vivo testando o modo Docker do Teams, com MFA aprovado com
// sucesso e mesmo assim a extração nunca avançava até estourar o timeout de 4min): esse form
// (id="hiddenform", method POST, action de volta pro próprio host .access.mcas.ms) deveria se
// auto-submeter via JS assim que a página carrega — padrão clássico "auto-post" de redirect
// OIDC/SAML — mas nesse ambiente o auto-submit nunca disparava, travando a página
// indefinidamente numa tela que nem pede interação humana de verdade. Inspecionando a página ao
// vivo via CDP: `document.getElementById('hiddenform').submit()` resolve na hora, avançando pra
// teams.microsoft.com.mcas.ms/v2/ — provável causa raiz é um recurso externo
// (inline.cdn.mcas.ms, usado pro i18n dos textos da página) que falha silenciosamente e impede o
// script original de rodar, mas submeter o form manualmente não depende desse recurso.
//
// Chamado incondicionalmente (não só no modo Docker) nos loops de espera de discover.go — mesmo
// padrão de AttemptSSOAutoLogin: retorna false rápido (via document.getElementById que não acha
// nada) em qualquer página que não seja essa tela específica, então é seguro chamar toda
// iteração. O Chrome do sistema normalmente não expõe esse bug porque a sessão salva já costuma
// vir autenticada (login silencioso, sem passar por esse redirect de novo) — mas um perfil
// novo/frio pode disparar o mesmo problema em qualquer modo.
func SubmitMCASHiddenForm(page *rod.Page) bool {
	res, err := page.Eval(`() => {
		const f = document.getElementById('hiddenform');
		if (f && f.tagName === 'FORM') { f.submit(); return true; }
		return false;
	}`)
	if err != nil || res == nil {
		return false
	}
	return res.Value.Bool()
}
