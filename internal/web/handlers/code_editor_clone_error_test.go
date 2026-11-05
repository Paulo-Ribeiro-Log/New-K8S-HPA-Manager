package handlers

import (
	"errors"
	"strings"
	"testing"
)

func TestCloneFailureMessage(t *testing.T) {
	exit := errors.New("exit status 128")

	sso := []string{
		"Cloning into 'x'...",
		"remote: The 'org' organization has enabled or enforced SAML SSO.",
		"remote: To access this repository, visit https://github.com/enterprises/grupo/sso?authorization_request=ABC123",
		"and try your request again.",
		"fatal: unable to access 'https://github.com/org/r.git/': The requested URL returned error: 403",
	}
	got := cloneFailureMessage(sso, exit)
	if !strings.Contains(got, "SAML SSO") || !strings.Contains(got, "authorization_request=ABC123") || strings.Contains(got, "exit status") {
		t.Errorf("SSO: %q", got)
	}

	got = cloneFailureMessage([]string{"fatal: Authentication failed for 'https://github.com/o/r.git/'"}, exit)
	if !strings.Contains(got, "recusou a credencial") {
		t.Errorf("credencial: %q", got)
	}

	got = cloneFailureMessage([]string{"Cloning into 'x'...", "fatal: repository 'https://github.com/o/r.git/' not found"}, exit)
	if got != "repository 'https://github.com/o/r.git/' not found" {
		t.Errorf("fatal: %q", got)
	}

	if got = cloneFailureMessage(nil, exit); got != "exit status 128" {
		t.Errorf("sem saída: %q", got)
	}
}
