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

func TestIsFindPartialExit(t *testing.T) {
	if !isFindPartialExit(fmt.Errorf("stream: command terminated with exit code 1 (stderr: )")) {
		t.Error("exit code 1 do find (diretório ilegível) deveria ser parcial")
	}
	for _, e := range []error{
		nil,
		fmt.Errorf("stream: command terminated with exit code 127 (stderr: )"),
		fmt.Errorf("stream: command terminated with exit code 137 (stderr: )"),
		fmt.Errorf("stream: context deadline exceeded (stderr: )"),
	} {
		if isFindPartialExit(e) {
			t.Errorf("%v não deveria ser tratado como exit 1 parcial", e)
		}
	}
}

func TestConfigFilePatternsCobremGo(t *testing.T) {
	for _, p := range []string{"config*.yaml", "config*.yml", "config*.json", "config*.toml", ".env"} {
		if !strings.Contains(configFileFindScript, "'"+p+"'") || !strings.Contains(configFileFindScriptNoPrintf, "'"+p+"'") {
			t.Errorf("scripts find não cobrem %q", p)
		}
		found := false
		for _, q := range configFileNamePatterns {
			found = found || q == p
		}
		if !found {
			t.Errorf("configFileNamePatterns não cobre %q", p)
		}
	}
}
