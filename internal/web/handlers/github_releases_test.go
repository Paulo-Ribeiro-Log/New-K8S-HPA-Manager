package handlers

import "testing"

func TestExtractLabelsMetadata_GithubRepoFromPodTemplate(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "my-app"}
	annotations := map[string]string{}
	podTemplateAnnotations := map[string]string{
		"devops.k8s.io/repository": "org/my-app",
	}

	appName, _, _, githubRepo := extractLabelsMetadata(labels, annotations, podTemplateAnnotations, "fallback")

	if appName != "my-app" {
		t.Errorf("appName = %q, want my-app", appName)
	}
	if githubRepo != "org/my-app" {
		t.Errorf("githubRepo = %q, want org/my-app", githubRepo)
	}
}

func TestExtractLabelsMetadata_GithubRepoFallsBackToTopLevelAnnotations(t *testing.T) {
	labels := map[string]string{}
	annotations := map[string]string{"devops.k8s.io/repository": "org/legacy-app"}
	podTemplateAnnotations := map[string]string{}

	_, _, _, githubRepo := extractLabelsMetadata(labels, annotations, podTemplateAnnotations, "fallback")

	if githubRepo != "org/legacy-app" {
		t.Errorf("githubRepo = %q, want org/legacy-app (fallback pro metadata top-level)", githubRepo)
	}
}

func TestExtractLabelsMetadata_GithubRepoEmptyWhenMissingEverywhere(t *testing.T) {
	_, _, _, githubRepo := extractLabelsMetadata(map[string]string{}, map[string]string{}, map[string]string{}, "fallback")
	if githubRepo != "" {
		t.Errorf("githubRepo = %q, want vazio", githubRepo)
	}
}
