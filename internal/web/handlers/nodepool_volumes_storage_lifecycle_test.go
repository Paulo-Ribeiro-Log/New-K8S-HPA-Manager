package handlers

import (
	"fmt"
	"strings"
	"testing"
)

const lifecyclePolicyJSON = `{"properties":{"policy":{"rules":[
 {"name":"arquivar-logs","enabled":true,"type":"Lifecycle","definition":{
   "filters":{"blobTypes":["blockBlob"],"prefixMatch":["logs/","audit/"]},
   "actions":{"baseBlob":{
      "tierToCool":{"daysAfterModificationGreaterThan":30},
      "tierToArchive":{"daysAfterModificationGreaterThan":90},
      "delete":{"daysAfterModificationGreaterThan":365}},
    "snapshot":{"delete":{"daysAfterCreationGreaterThan":30}}}}},
 {"name":"antiga","enabled":false,"type":"Lifecycle","definition":{
   "filters":{"blobTypes":["blockBlob"]},
   "actions":{"baseBlob":{"tierToCool":{"daysAfterLastAccessTimeGreaterThan":10}}}}}
]}}}`

func TestParseLifecycleRules(t *testing.T) {
	rules := parseLifecycleRules(decode([]byte(lifecyclePolicyJSON)))
	if len(rules) != 2 || rules[0].Name != "antiga" || rules[1].Name != "arquivar-logs" {
		t.Fatalf("regras (ordenadas por nome): %+v", rules)
	}
	r := rules[1]
	if !r.Enabled || !r.Deletes || len(r.Prefixes) != 2 || len(r.BlobTypes) != 1 {
		t.Errorf("regra arquivar-logs: %+v", r)
	}
	want := []string{
		"blob: mover para Cool após 30 dia(s) da última modificação",
		"blob: mover para Archive após 90 dia(s) da última modificação",
		"blob: excluir após 365 dia(s) da última modificação",
		"snapshot: excluir após 30 dia(s) da criação",
	}
	if strings.Join(r.Actions, "|") != strings.Join(want, "|") {
		t.Errorf("ações:\n got %v\nwant %v", r.Actions, want)
	}
	if rules[0].Enabled || rules[0].Actions[0] != "blob: mover para Cool após 10 dia(s) do último acesso" {
		t.Errorf("regra desativada: %+v", rules[0])
	}
	if txt := lifecycleText(r); !strings.Contains(txt, "prefixos: logs/, audit/") || !strings.Contains(txt, "excluir") {
		t.Errorf("texto: %s", txt)
	}
	if txt := lifecycleText(rules[0]); !strings.Contains(txt, "todos os blobs") {
		t.Errorf("sem prefixo deveria dizer 'todos os blobs': %s", txt)
	}
}

func TestLifecycleSection(t *testing.T) {
	d := &AzStorageDetails{}
	lifecycleSection(decode([]byte(lifecyclePolicyJSON)), nil, d)
	if len(d.Sections) != 1 || len(d.Sections[0].Fields) != 2 {
		t.Fatalf("seção: %+v", d.Sections)
	}
	if f := d.Sections[0].Fields[0]; f.Label != "antiga (desativada)" || !f.Warn {
		t.Errorf("regra desativada deve sinalizar: %+v", f)
	}
	if len(d.Notes) != 1 || !strings.Contains(d.Notes[0], "exclui dados automaticamente") || strings.Contains(d.Notes[0], "snapshot: excluir") == false {
		t.Errorf("nota de exclusão: %v", d.Notes)
	}

	// sem política: não é erro, é ausência de regras (e vira aviso na tela)
	d = &AzStorageDetails{}
	lifecycleSection(nil, fmt.Errorf(`ERROR: Not Found({"error":{"code":"ManagementPolicyNotFound"}})`), d)
	if len(d.Warnings) != 0 || len(d.Sections) != 1 || !d.Sections[0].Fields[0].Warn {
		t.Errorf("sem política: %+v %+v", d.Sections, d.Warnings)
	}
	// outro erro (permissão) vira aviso da seção, sem seção
	d = &AzStorageDetails{}
	lifecycleSection(nil, fmt.Errorf("AuthorizationFailed"), d)
	if len(d.Warnings) != 1 || len(d.Sections) != 0 {
		t.Errorf("erro de permissão: %+v %+v", d.Sections, d.Warnings)
	}
}
