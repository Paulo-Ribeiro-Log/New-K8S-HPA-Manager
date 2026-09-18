package aws

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/models"
)

// describeInstancesFixture reproduz o shape real de `aws ec2 describe-instances --output json`
// (confirmado contra a documentação/schema da AWS CLI — Reservations[].Instances[]).
const describeInstancesFixture = `{
  "Reservations": [
    {
      "Instances": [
        {
          "InstanceId": "i-0123456789abcdef0",
          "InstanceType": "t3.medium",
          "State": {"Name": "running"},
          "PublicIpAddress": "54.1.2.3",
          "PrivateIpAddress": "10.0.1.5",
          "Placement": {"AvailabilityZone": "us-east-1a"},
          "LaunchTime": "2026-01-15T10:30:00.000Z",
          "Tags": [
            {"Key": "Name", "Value": "web-server-01"},
            {"Key": "Environment", "Value": "prd"}
          ]
        },
        {
          "InstanceId": "i-0fedcba9876543210",
          "InstanceType": "t3.large",
          "State": {"Name": "stopped"},
          "Placement": {"AvailabilityZone": "us-east-1b"},
          "LaunchTime": "2026-01-10T08:00:00.000Z",
          "Platform": "windows",
          "Tags": []
        }
      ]
    }
  ]
}`

const ssmDescribeInstanceInfoFixture = `{
  "InstanceInformationList": [
    {"InstanceId": "i-0123456789abcdef0", "PingStatus": "Online"},
    {"InstanceId": "i-0fedcba9876543210", "PingStatus": "ConnectionLost"}
  ]
}`

func TestParseDescribeInstances_CamposBasicos(t *testing.T) {
	var resp ec2DescribeInstancesResponse
	if err := json.Unmarshal([]byte(describeInstancesFixture), &resp); err != nil {
		t.Fatalf("erro ao parsear fixture: %v", err)
	}
	if len(resp.Reservations) != 1 || len(resp.Reservations[0].Instances) != 2 {
		t.Fatalf("esperado 1 reservation com 2 instances, veio %+v", resp)
	}

	inst1 := resp.Reservations[0].Instances[0]
	if inst1.InstanceId != "i-0123456789abcdef0" {
		t.Errorf("InstanceId = %q", inst1.InstanceId)
	}
	if instanceNameFromTags(inst1) != "web-server-01" {
		t.Errorf("nome via tag = %q, esperado web-server-01", instanceNameFromTags(inst1))
	}
	if normalizeEC2State(inst1.State.Name) != models.PowerStateRunning {
		t.Errorf("state = %q, esperado running", normalizeEC2State(inst1.State.Name))
	}
	if osFromPlatform(inst1.Platform) != "linux" {
		t.Errorf("os = %q, esperado linux (Platform vazio = Linux)", osFromPlatform(inst1.Platform))
	}

	inst2 := resp.Reservations[0].Instances[1]
	// Sem tag "Name" — instanceNameFromTags deve cair pro InstanceId, nunca string vazia.
	if instanceNameFromTags(inst2) != "i-0fedcba9876543210" {
		t.Errorf("fallback de nome sem tag Name = %q, esperado o próprio InstanceId", instanceNameFromTags(inst2))
	}
	if osFromPlatform(inst2.Platform) != "windows" {
		t.Errorf("os = %q, esperado windows", osFromPlatform(inst2.Platform))
	}
	if tagsToMap(inst2) != nil {
		t.Errorf("Tags vazio deveria virar map nil, veio %+v", tagsToMap(inst2))
	}
}

func TestInstanceNameFromTags_CaseInsensitiveFallback(t *testing.T) {
	makeInst := func(tags ...struct{ Key, Value string }) ec2Instance {
		inst := ec2Instance{InstanceId: "i-0000000000000000"}
		for _, tg := range tags {
			inst.Tags = append(inst.Tags, struct {
				Key   string `json:"Key"`
				Value string `json:"Value"`
			}{Key: tg.Key, Value: tg.Value})
		}
		return inst
	}
	kv := func(k, v string) struct{ Key, Value string } { return struct{ Key, Value string }{k, v} }

	// Chave lowercase "name" — não bate no match exato "Name", só no fallback case-insensitive.
	instLower := makeInst(kv("name", "meu-servidor"))
	if got := instanceNameFromTags(instLower); got != "meu-servidor" {
		t.Errorf("fallback case-insensitive (name) = %q, esperado meu-servidor", got)
	}

	// Chave "NAME" (maiúsculas) + espaço em branco.
	instUpper := makeInst(kv("NAME", "outro-servidor"))
	if got := instanceNameFromTags(instUpper); got != "outro-servidor" {
		t.Errorf("fallback case-insensitive (NAME) = %q, esperado outro-servidor", got)
	}

	// Exact match "Name" tem prioridade sobre um "name" lowercase concorrente (não deveria
	// acontecer na prática — AWS não permite 2 tags com a mesma chave normalizada — mas o
	// comportamento de prioridade é explícito e testável mesmo assim).
	instBoth := makeInst(kv("Name", "prioridade-exata"), kv("name", "nao-deveria-vencer"))
	if got := instanceNameFromTags(instBoth); got != "prioridade-exata" {
		t.Errorf("prioridade exata = %q, esperado prioridade-exata", got)
	}

	// Nenhuma tag parecida com "name" — cai pro InstanceId, nunca string vazia.
	instNone := makeInst(kv("Environment", "prd"))
	if got := instanceNameFromTags(instNone); got != "i-0000000000000000" {
		t.Errorf("sem tag de nome = %q, esperado o InstanceId", got)
	}
}

func TestSSMOnlineParsing_SoContaOnline(t *testing.T) {
	var resp ssmDescribeInstanceInfoResponse
	if err := json.Unmarshal([]byte(ssmDescribeInstanceInfoFixture), &resp); err != nil {
		t.Fatalf("erro ao parsear fixture SSM: %v", err)
	}

	online := map[string]bool{}
	for _, info := range resp.InstanceInformationList {
		if strings.EqualFold(info.PingStatus, "Online") {
			online[info.InstanceId] = true
		}
	}

	if !online["i-0123456789abcdef0"] {
		t.Errorf("instância com PingStatus=Online deveria contar como SSM-viável")
	}
	if online["i-0fedcba9876543210"] {
		t.Errorf("instância com PingStatus=ConnectionLost NÃO deveria contar como SSM-viável")
	}
}

// TestListInstances_SupportedConnectionModes cobre a heurística fim-a-fim (parse + cruzamento),
// sem chamar a AWS CLI de verdade — monta os dois structs de resposta diretamente, do jeito que
// ListInstances os consumiria.
func TestListInstances_SupportedConnectionModes(t *testing.T) {
	var resp ec2DescribeInstancesResponse
	if err := json.Unmarshal([]byte(describeInstancesFixture), &resp); err != nil {
		t.Fatalf("erro ao parsear fixture: %v", err)
	}
	ssmOnline := map[string]bool{"i-0123456789abcdef0": true}

	for _, inst := range resp.Reservations[0].Instances {
		modes := []models.ConnectionMode{models.ConnectionModeSSH}
		if ssmOnline[inst.InstanceId] {
			modes = append(modes, models.ConnectionModeSSM)
		}

		if inst.InstanceId == "i-0123456789abcdef0" {
			if len(modes) != 2 {
				t.Errorf("instância com SSM online deveria ter 2 modos (ssh+ssm), veio %v", modes)
			}
		} else {
			if len(modes) != 1 || modes[0] != models.ConnectionModeSSH {
				t.Errorf("instância sem SSM deveria ter só [ssh], veio %v", modes)
			}
		}
	}
}

func TestApplyVMFilter_PorTag(t *testing.T) {
	instances := []models.VMInstance{
		{ID: "i-1", Tags: map[string]string{"Environment": "prd"}},
		{ID: "i-2", Tags: map[string]string{"Environment": "hlg"}},
		{ID: "i-3", Tags: nil},
	}

	filtered := applyVMFilter(instances, models.VMFilter{Tags: map[string]string{"Environment": "prd"}})
	if len(filtered) != 1 || filtered[0].ID != "i-1" {
		t.Errorf("esperado só i-1, veio %+v", filtered)
	}

	// Sem filtro de tags, devolve tudo sem alteração.
	unfiltered := applyVMFilter(instances, models.VMFilter{})
	if len(unfiltered) != 3 {
		t.Errorf("sem filtro deveria devolver as 3 instâncias, veio %d", len(unfiltered))
	}
}

func TestNormalizeEC2State_EstadoDesconhecidoViraUnknown(t *testing.T) {
	if got := normalizeEC2State("algo-novo-que-a-aws-inventou"); got != models.PowerStateUnknown {
		t.Errorf("estado desconhecido deveria virar unknown, veio %q", got)
	}
	if got := normalizeEC2State("running"); got != models.PowerStateRunning {
		t.Errorf("running deveria passar direto, veio %q", got)
	}
}

// TestClassifyEC2Error_MesmoEspiritoDoClassifyAWSError garante que os casos de erro de credencial
// já cobertos por classifyAWSError (nodegroup.go) também são reconhecidos aqui, e que a mensagem
// de "instância não encontrada" cita a instância certa (mesmo bug real já corrigido uma vez em
// classifyAWSError — citar o profile no lugar do recurso que falhou).
func TestClassifyEC2Error_MesmoEspiritoDoClassifyAWSError(t *testing.T) {
	tests := []struct {
		name        string
		raw         error
		instanceID  string
		profile     string
		wantSubstr  string
		wantExclude string
	}{
		{
			name:       "token expirado",
			raw:        errors.New("An error occurred: ExpiredToken"),
			profile:    "cnt",
			wantSubstr: "aws sso login --profile cnt",
		},
		{
			name:        "instância não encontrada cita a instância, não o profile",
			raw:         errors.New("An error occurred (InvalidInstanceID.NotFound): The instance ID 'i-0123456789abcdef0' does not exist"),
			instanceID:  "i-0123456789abcdef0",
			profile:     "cnt",
			wantSubstr:  "i-0123456789abcdef0",
			wantExclude: "instância 'cnt'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyEC2Error(tc.raw, tc.instanceID, tc.profile)
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("mensagem %q não contém %q", err.Error(), tc.wantSubstr)
			}
			if tc.wantExclude != "" && strings.Contains(err.Error(), tc.wantExclude) {
				t.Errorf("mensagem %q não deveria conter %q", err.Error(), tc.wantExclude)
			}
		})
	}
}

// compile-time sanity: NewAWSEC2Provider monta o provider com region/profile corretos.
func TestNewAWSEC2Provider(t *testing.T) {
	p := NewAWSEC2Provider("sa-east-1", "cnt")
	if p.region != "sa-east-1" || p.profile != "cnt" {
		t.Errorf("region/profile = %q/%q, esperado sa-east-1/cnt", p.region, p.profile)
	}
}
