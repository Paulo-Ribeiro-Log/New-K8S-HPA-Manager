package handlers

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCaseInsensitiveGlob(t *testing.T) {
	cases := map[string]string{
		"web.config": "[wW][eE][bB].[cC][oO][nN][fF][iI][gG]",
		"*.jar":      "*.[jJ][aA][rR]",
		"app*.yml":   "[aA][pP][pP]*.[yY][mM][lL]",
	}
	for in, want := range cases {
		if got := caseInsensitiveGlob(in); got != want {
			t.Errorf("caseInsensitiveGlob(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShWalkFindScriptUsaTodosOsPadroes(t *testing.T) {
	script := shWalkFindScript(archiveNamePatterns)
	for _, p := range archiveNamePatterns {
		if !strings.Contains(script, caseInsensitiveGlob(p)) {
			t.Errorf("script não contém o padrão %q: %s", p, script)
		}
	}
	if !strings.HasSuffix(script, "exit 0") {
		t.Errorf("script deveria terminar com exit 0: %s", script)
	}
}

func TestIsCommandNotFoundExitENoShell(t *testing.T) {
	exit127 := fmt.Errorf("stream: command terminated with exit code 127 (stderr: )")
	if !isCommandNotFoundExit(exit127) {
		t.Error("exit code 127 deveria ser reconhecido")
	}
	if isCommandNotFoundExit(fmt.Errorf("stream: command terminated with exit code 1 (stderr: )")) {
		t.Error("exit code 1 não é comando não encontrado")
	}
	if isNoShellExecError(exit127) {
		t.Error("127 sozinho não prova falta de sh (pode ser só o find)")
	}
	if !isNoShellExecError(errNoShell) || !isNoShellExecError(fmt.Errorf("x: %w", errNoShell)) {
		t.Error("errNoShell deveria ser reconhecido como falta de sh")
	}
	if !errors.Is(errNoShell, errNoShell) {
		t.Error("errNoShell")
	}
}
