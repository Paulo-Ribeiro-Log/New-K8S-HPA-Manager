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

// TestTrigger_IsRollbackFlag cobre o achado da Fase 4 (generalização entre squads): "Is Rollback"
// (Parameters, bool) confirmado ao vivo em 8 applications de 4 squads diferentes; "isRollback"
// (Payload, bool) confirmado numa execução satélite real de rollback (squad SRE Marketplace).
func TestTrigger_IsRollbackFlag(t *testing.T) {
	cases := []struct {
		name string
		t    Trigger
		want bool
	}{
		{"Parameters true", Trigger{Parameters: map[string]interface{}{"Is Rollback": true}}, true},
		{"Parameters false", Trigger{Parameters: map[string]interface{}{"Is Rollback": false}}, false},
		{"Payload true (Parameters ausente)", Trigger{Payload: map[string]interface{}{"isRollback": true}}, true},
		{"nenhum dos dois presente", Trigger{}, false},
		{"Parameters com tipo errado (string, não bool) é ignorado", Trigger{Parameters: map[string]interface{}{"Is Rollback": "true"}}, false},
		{"chave parecida mas diferente ('Is Automatic Rollback') não conta", Trigger{Parameters: map[string]interface{}{"Is Automatic Rollback": "false"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.t.IsRollbackFlag(); got != c.want {
				t.Errorf("IsRollbackFlag() = %v, want %v", got, c.want)
			}
		})
	}
}
