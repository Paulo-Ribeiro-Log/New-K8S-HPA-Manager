package spinnaker

import (
	"encoding/json"
	"testing"
)

// TestTrigger_ParametersDecodesNonStringValues reproduz um bug real (confirmado ao vivo contra
// o Gate de PRD, projeto "SRE Logistica"): pelo menos uma application manda um valor não-string
// em trigger.parameters (ex: bool). Quando Parameters era map[string]string, json.Unmarshal
// falhava a struct Execution INTEIRA — não só o campo problemático — e SearchExecutions
// descartava a resposta completa (erro logado, 0 execuções), fazendo DetectRollback nunca achar
// nada pra NENHUMA application do projeto, silenciosamente ("matched: false" universal).
func TestTrigger_ParametersDecodesNonStringValues(t *testing.T) {
	raw := `{
		"id": "exec-1",
		"application": "logreversa",
		"trigger": {
			"parameters": {
				"Application Name": "dat-documento-vendas-api",
				"Application K8S Namespace": "dat-prd",
				"Application Version": "0.0.2-2",
				"Some Flag": true,
				"Some Number": 42
			}
		}
	}`

	var ex Execution
	if err := json.Unmarshal([]byte(raw), &ex); err != nil {
		t.Fatalf("Unmarshal falhou com valor não-string em trigger.parameters: %v", err)
	}

	if got := ex.Trigger.AppName(); got != "dat-documento-vendas-api" {
		t.Errorf("AppName() = %q, want %q", got, "dat-documento-vendas-api")
	}
	if got := ex.Trigger.Namespace(); got != "dat-prd" {
		t.Errorf("Namespace() = %q, want %q", got, "dat-prd")
	}
	if got := ex.Trigger.Version(); got != "0.0.2-2" {
		t.Errorf("Version() = %q, want %q", got, "0.0.2-2")
	}
	// Não deve dar panic nem retornar erro ao ler uma chave com valor bool/número — só precisa
	// tolerar, não precisamos do valor convertido em lugar nenhum do fluxo real.
	if got := ex.Trigger.paramString("Some Flag"); got != "true" {
		t.Errorf("paramString(bool) = %q, want %q", got, "true")
	}
	if got := ex.Trigger.paramString("Some Number"); got != "42" {
		t.Errorf("paramString(number) = %q, want %q", got, "42")
	}
}
