package handlers

import "testing"

// githubRepo via annotation devops.k8s.io/repository foi desativado propositalmente — a
// annotation provou ser não confiável (CHG real com "Projeto"/"Aplicação(ões)" com sufixo
// extra, ex: "-b2c", que não corresponde ao repositório GitHub real). A fonte de verdade
// agora é o campo "URL do Repositório" extraído da CHG do ServiceNow (internal/servicenow),
// não o scan de deployments do cluster. Ver extractLabelsMetadata em github_releases.go.
func TestExtractLabelsMetadata_GithubRepoAlwaysEmpty(t *testing.T) {
	podTemplateAnnotations := map[string]string{"devops.k8s.io/repository": "org/my-app"}
	annotations := map[string]string{"devops.k8s.io/repository": "org/legacy-app"}

	_, _, _, githubRepo := extractLabelsMetadata(map[string]string{}, annotations, podTemplateAnnotations, "fallback")

	if githubRepo != "" {
		t.Errorf("githubRepo = %q, want vazio (annotation devops.k8s.io/repository ignorada de propósito)", githubRepo)
	}
}

func TestExtractLabelsMetadata_AppNameStillResolved(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "my-app"}

	appName, _, _, _ := extractLabelsMetadata(labels, map[string]string{}, map[string]string{}, "fallback")

	if appName != "my-app" {
		t.Errorf("appName = %q, want my-app", appName)
	}
}

func TestExtractLabelsMetadata_GithubRepoEmptyWhenMissingEverywhere(t *testing.T) {
	_, _, _, githubRepo := extractLabelsMetadata(map[string]string{}, map[string]string{}, map[string]string{}, "fallback")
	if githubRepo != "" {
		t.Errorf("githubRepo = %q, want vazio", githubRepo)
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"numérica com 3 hífens (formato principal)", "2-5-5-2", "2.5.5-2"},
		{"numérica com 2 hífens", "2-5-2", "2.5.2"},
		{"remove prefixo v", "v2-5-5-2", "2.5.5-2"},
		{"já em formato semver, sem alteração", "4.0.4-3", "4.0.4-3"},
		{"alfanumérica com 2 hífens — não deve virar semver corrompido", "choic-4437_cnpj_v6-1", "choic-4437_cnpj_v6-1"},
		{"alfanumérica com 3 hífens — não deve virar semver corrompido", "time-b2c-hotfix-v2", "time-b2c-hotfix-v2"},
		{"vazia", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeVersion(tc.input)
			if got != tc.want {
				t.Errorf("normalizeVersion(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
