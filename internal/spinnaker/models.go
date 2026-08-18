// Package spinnaker é o cliente Go para o Gate API do Spinnaker, usado para detectar se a
// versão vigente de um Deployment chegou lá por rollback. Ver SPINNAKER-INTEGRATION-PLAN.md
// (Fase 1) para o desenho completo — este pacote implementa só o cliente/detecção, sem
// handler HTTP nem persistência de configuração (isso é Fase 2/2.5).
package spinnaker

import (
	"encoding/json"
	"fmt"
)

// Execution é uma execução de pipeline do Spinnaker, campos confirmados ao vivo contra uma
// instância real (ver seção 2/0.2/0.3 do plano) — não é o schema completo do Gate, só os
// campos que esta integração usa.
type Execution struct {
	ID          string  `json:"id"`
	Application string  `json:"application"`
	Name        string  `json:"name"` // nome do pipeline, ex: "deploy-aks-global", "rollback-aks-global"
	BuildTime   int64   `json:"buildTime"`
	StartTime   int64   `json:"startTime"`
	EndTime     int64   `json:"endTime"`
	Status      string  `json:"status"` // SUCCEEDED, TERMINAL, FAILED_CONTINUE, CANCELED, RUNNING...
	Stages      []Stage `json:"stages"`
	Trigger     Trigger `json:"trigger"`
}

// Stage é um estágio de uma execução (deployManifest, runJobManifest, checkPreconditions...).
// Context fica como json.RawMessage porque a estrutura real é um blob com dezenas de chaves
// heterogêneas (kato.*, deploy.*, notification.type...) — só decodificamos o que precisamos
// (manifestArtifact.reference) sob demanda via ManifestReference().
type Stage struct {
	ID        string          `json:"id"`
	RefID     string          `json:"refId"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	StartTime int64           `json:"startTime"`
	EndTime   int64           `json:"endTime"`
	Context   json.RawMessage `json:"context"`
}

// stageContextManifest é o subconjunto de Stage.Context que interessa pra detecção de rollback.
type stageContextManifest struct {
	ManifestArtifact *struct {
		Reference string `json:"reference"`
	} `json:"manifestArtifact"`
}

// ManifestReference devolve a URL do manifesto (Job YAML) usado neste stage, ou "" se o stage
// não tiver manifestArtifact ou o context não decodificar (nunca erro fatal — dado best-effort).
func (s Stage) ManifestReference() string {
	if len(s.Context) == 0 {
		return ""
	}
	var ctx stageContextManifest
	if err := json.Unmarshal(s.Context, &ctx); err != nil {
		return ""
	}
	if ctx.ManifestArtifact == nil {
		return ""
	}
	return ctx.ManifestArtifact.Reference
}

// Trigger é quem/o que disparou a execução. Achado real (seção 0.3 do plano): `Parameters`
// (labels humanos, ex: "Application SN Change Number") é fonte mais estável e limpa que
// `Payload` (dump bruto de env vars de CI, usado só como fallback).
type Trigger struct {
	Type   string `json:"type"`
	User   string `json:"user"`
	Source string `json:"source"` // conta/cluster que disparou o webhook, ex: "akspriv-logreversa-hlg"
	// Parameters é map[string]interface{}, não map[string]string — achado real (bug reproduzido
	// ao vivo contra o Gate de PRD, projeto "SRE Logistica"): pelo menos uma application manda um
	// valor não-string em trigger.parameters (ex: um bool), o que quebrava o unmarshal de TODAS as
	// applications consultadas (json.Unmarshal falha a struct inteira, não só o campo problemático)
	// — o sintoma era "matched: false" silencioso pra qualquer deployment, porque SearchExecutions
	// nunca retornava nenhuma execução decodificada. paramString() converte pra string on-demand.
	Parameters map[string]interface{} `json:"parameters"`
	Payload    map[string]interface{} `json:"payload"`
}

// Chaves conhecidas de Trigger.Parameters — nomes confirmados ao vivo (seção 0.3 do plano).
const (
	paramAppName   = "Application Name"
	paramNamespace = "Application K8S Namespace"
	paramVersion   = "Application Version"
	paramCHGNumber = "Application SN Change Number"
	paramTargetEnv = "Application Environment"
)

// AppName, Namespace, Version e CHGNumber leem de Trigger.Parameters primeiro (fonte primária)
// e caem para Trigger.Payload quando ausente (fallback — payload usa chaves diferentes,
// minúsculas/camelCase, confirmadas na mesma investigação).

func (t Trigger) AppName() string {
	if v := t.paramString(paramAppName); v != "" {
		return v
	}
	return t.payloadString("nameApp")
}

func (t Trigger) Namespace() string {
	if v := t.paramString(paramNamespace); v != "" {
		return v
	}
	return t.payloadString("namespace")
}

func (t Trigger) Version() string {
	if v := t.paramString(paramVersion); v != "" {
		return v
	}
	if v := t.payloadString("version"); v != "" {
		return v
	}
	return t.payloadString("tag")
}

func (t Trigger) CHGNumber() string {
	if v := t.paramString(paramCHGNumber); v != "" {
		return v
	}
	return t.payloadString("serviceNowTaskNumber")
}

func (t Trigger) TargetEnv() string {
	if v := t.paramString(paramTargetEnv); v != "" {
		return v
	}
	return t.payloadString("targetEnv")
}

// IsRollbackFlag lê o sinal explícito "Is Rollback" (Parameters) / "isRollback" (Payload) —
// achado real da Fase 4 (generalização entre squads, ver SPINNAKER-INTEGRATION-PLAN.md seção 6
// item 2): testado ao vivo contra 8 applications de squads DIFERENTES (SRE Logística, SRE
// Compra Self Service, sre cyber iam, SRE Marketplace) — o campo aparece em 100% das execuções
// amostradas (sempre bool, nunca ausente nas squads testadas), sempre `false` em deploys normais
// e `true` na única execução de rollback real encontrada fora de SRE Logística. É um sinal MAIS
// confiável que nome de pipeline (frágil — squads podem nomear diferente) ou manifestArtifact
// (indireto), mas usado aqui como reforço, não substituto: numa execução real observada, esse
// campo veio `true` numa execução SATÉLITE (pipeline "deploy-aks-global" reaplicando o artefato
// antigo) que tinha Namespace/Version/CHG vazios — não corresponderia a nenhum Deployment via
// executionsForTarget sozinha; a execução do pipeline nomeado "rollback-*" (com esses campos
// populados) continua sendo a que a correlação de fato usa. Não confundir com "Is Automatic
// Rollback" (chave diferente, presente na execução do pipeline de rollback em si — indica se
// foi disparado automaticamente ou não, não se É um rollback).
func (t Trigger) IsRollbackFlag() bool {
	if v, ok := t.Parameters["Is Rollback"]; ok {
		if b, isBool := v.(bool); isBool {
			return b
		}
	}
	if v, ok := t.Payload["isRollback"]; ok {
		if b, isBool := v.(bool); isBool {
			return b
		}
	}
	return false
}

// paramString lê Trigger.Parameters tolerando qualquer tipo JSON (string é o caso comum, mas o
// Gate real já mandou bool pra alguma chave observada) — nunca aplica type assertion direta.
func (t Trigger) paramString(key string) string {
	v, ok := t.Parameters[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func (t Trigger) payloadString(key string) string {
	v, ok := t.Payload[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Project é um projeto Spinnaker (agrupamento tipo squad), confirmado ao vivo via GET /projects
// (seção 10.2 do plano) — hierarquia real: Project → Applications (várias) → nameApp (microsserviço).
type Project struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Email  string        `json:"email"`
	Config ProjectConfig `json:"config"`
}

// ProjectConfig traz só o que usamos — Applications é a lista de applications Spinnaker do projeto.
type ProjectConfig struct {
	Applications []string `json:"applications"`
}
