package spinnaker

import "testing"

func TestConfig_LoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()

	got, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig em diretório sem arquivo não deveria dar erro: %v", err)
	}
	if got != (Config{}) {
		t.Fatalf("esperava Config zero value quando arquivo não existe, veio %+v", got)
	}

	want := Config{
		LoginIdentifier: "matricula",
		HLGBaseURL:      "https://spinnaker-hlg.viavarejo.com.br/",
		PRDBaseURL:      "https://spinnaker-prd.viavarejo.com.br/",
		SelectedProject: "SRE Logistica",
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig após salvar: %v", err)
	}
	if got != want {
		t.Errorf("LoadConfig() = %+v, esperava %+v", got, want)
	}
}

func TestConfig_DeckURLForEnv(t *testing.T) {
	cfg := Config{
		HLGBaseURL: "https://spinnaker-hlg.viavarejo.com.br/",
		PRDBaseURL: "https://spinnaker-prd.viavarejo.com.br/",
	}

	if got, err := cfg.DeckURLForEnv(EnvHLG); err != nil || got != cfg.HLGBaseURL {
		t.Errorf("DeckURLForEnv(hlg) = %q, %v", got, err)
	}
	if got, err := cfg.DeckURLForEnv(EnvPRD); err != nil || got != cfg.PRDBaseURL {
		t.Errorf("DeckURLForEnv(prd) = %q, %v", got, err)
	}
	if _, err := cfg.DeckURLForEnv("sit"); err == nil {
		t.Error("esperava erro pra ambiente desconhecido (\"sit\" não tem Gate próprio confirmado)")
	}

	empty := Config{}
	if _, err := empty.DeckURLForEnv(EnvHLG); err == nil {
		t.Error("esperava erro quando a URL do ambiente não está configurada")
	}
}

func TestDeriveEnv(t *testing.T) {
	cases := map[string]string{
		"akspriv-logreversa-hlg":       EnvHLG,
		"akspriv-ofertalogistica-prd":  EnvPRD,
		"akspriv-logreversa-hlg-admin": EnvHLG, // sufixo "-admin" removido antes de checar
		"akspriv-abastecimento-sit":    "",     // "sit" roda dentro do Gate de hlg, mas não dá pra saber só pelo nome
		"akspriv-abastecimento-stg":    "",
		"":                             "",
	}
	for cluster, want := range cases {
		if got := DeriveEnv(cluster); got != want {
			t.Errorf("DeriveEnv(%q) = %q, esperava %q", cluster, got, want)
		}
	}
}

func TestConfig_EffectiveLoginIdentifier(t *testing.T) {
	if got := (Config{}).EffectiveLoginIdentifier(); got != "email" {
		t.Errorf("default deveria ser \"email\" (mesmo default de BrowserConfig), veio %q", got)
	}
	if got := (Config{LoginIdentifier: "matricula"}).EffectiveLoginIdentifier(); got != "matricula" {
		t.Errorf("EffectiveLoginIdentifier() = %q, esperava \"matricula\"", got)
	}
}
