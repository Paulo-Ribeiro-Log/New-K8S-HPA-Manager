package teams

import "testing"

func TestIsTeamsURLAceitaOsDoisDominios(t *testing.T) {
	for _, u := range []string{
		"https://teams.microsoft.com/v2/",
		"https://teams.cloud.microsoft/",
		"https://teams.cloud.microsoft/v2/?culture=pt-br",
		"https://teams.microsoft.com.mcas.ms/v2/",
	} {
		if !isTeamsURL(u) {
			t.Errorf("isTeamsURL(%q) = false", u)
		}
	}
	for _, u := range []string{"about:blank", "https://login.microsoftonline.com/x", "https://example.com/teams"} {
		if isTeamsURL(u) {
			t.Errorf("isTeamsURL(%q) = true", u)
		}
	}
	if !isTeamsV2URL("https://teams.cloud.microsoft/v2/") || isTeamsV2URL("https://teams.cloud.microsoft/") {
		t.Error("isTeamsV2URL no domínio novo")
	}
}

func TestLinkDeMensagemNosDoisDominios(t *testing.T) {
	for _, link := range []string{
		"https://teams.microsoft.com/l/message/19:abc@thread.v2/1786982367908?context=x",
		"https://teams.cloud.microsoft/l/message/19:abc@thread.v2/1786982367908?context=x",
	} {
		th, msg, err := ParseTeamsMessageLink(link)
		if err != nil || th != "19:abc@thread.v2" || msg != "1786982367908" {
			t.Errorf("%s -> %q %q %v", link, th, msg, err)
		}
	}
	got := toTeamsHashRoute("https://teams.cloud.microsoft/l/message/19:a/1?x=1")
	if got != "https://teams.cloud.microsoft/_#/l/message/19:a/1?x=1" {
		t.Errorf("toTeamsHashRoute = %s", got)
	}
	if got := toTeamsHashRoute("https://teams.microsoft.com/l/message/19:a/1"); got != "https://teams.microsoft.com/_#/l/message/19:a/1" {
		t.Errorf("domínio antigo = %s", got)
	}
}
