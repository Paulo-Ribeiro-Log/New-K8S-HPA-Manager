package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s-hpa-manager/internal/cloudprovider"
	"k8s-hpa-manager/internal/models"
)

// AWSEC2Provider implementa cloudprovider.VMProvider usando AWS CLI (ec2/ssm).
type AWSEC2Provider struct {
	region  string
	profile string
}

// NewAWSEC2Provider cria um provider de instâncias EC2 pra uma região/perfil AWS.
func NewAWSEC2Provider(region, profile string) *AWSEC2Provider {
	return &AWSEC2Provider{region: region, profile: profile}
}

func (p *AWSEC2Provider) baseArgs(subcmd ...string) []string {
	return buildAWSArgs(p.region, p.profile, subcmd...)
}

func (p *AWSEC2Provider) run(ctx context.Context, args []string) ([]byte, error) {
	return runAWSCLI(ctx, p.profile, args)
}

// --- structs para parse de `aws ec2 describe-instances` ---

type ec2DescribeInstancesResponse struct {
	Reservations []struct {
		Instances []ec2Instance `json:"Instances"`
	} `json:"Reservations"`
}

type ec2Instance struct {
	InstanceId   string `json:"InstanceId"`
	InstanceType string `json:"InstanceType"`
	State        struct {
		Name string `json:"Name"`
	} `json:"State"`
	PublicIpAddress  string `json:"PublicIpAddress"`
	PrivateIpAddress string `json:"PrivateIpAddress"`
	Placement        struct {
		AvailabilityZone string `json:"AvailabilityZone"`
	} `json:"Placement"`
	LaunchTime string `json:"LaunchTime"`
	Platform   string `json:"Platform"` // "windows" quando é Windows; vazio = Linux/outro (AWS não marca Linux explicitamente)
	Tags       []struct {
		Key   string `json:"Key"`
		Value string `json:"Value"`
	} `json:"Tags"`
}

// --- struct para parse de `aws ssm describe-instance-information` ---

type ssmDescribeInstanceInfoResponse struct {
	InstanceInformationList []struct {
		InstanceId string `json:"InstanceId"`
		PingStatus string `json:"PingStatus"` // "Online" | "ConnectionLost" | "Inactive"
	} `json:"InstanceInformationList"`
}

// instanceNameFromTags resolve o nome de exibição a partir da tag "Name" (convenção universal da
// AWS Console) — sem essa tag, cai pro próprio InstanceId (nunca string vazia).
//
// Bug real corrigido — relatado pelo usuário: "não exibe o nome apenas o ID". Chave de tag na AWS
// é case-sensitive (confirmado na API/CLI) — a checagem original exigia a chave EXATA "Name",
// então uma instância tagueada como "name"/"NAME"/"Instance Name" (convenções alternativas reais,
// comuns em contas geridas por Terraform/CloudFormation de times diferentes) sempre caía no
// fallback pro InstanceId, mesmo tendo um nome de exibição de fato cadastrado. Corrigido com 2
// passadas: 1ª exige a chave exata "Name" (prioridade — é a convenção oficial da AWS Console,
// preferida quando ambas existem); 2ª, só se a 1ª não achou nada, aceita qualquer chave que bata
// case-insensitively com "name" (cobre "name"/"NAME"/"Name " com espaço).
func instanceNameFromTags(inst ec2Instance) string {
	for _, t := range inst.Tags {
		if t.Key == "Name" && t.Value != "" {
			return t.Value
		}
	}
	for _, t := range inst.Tags {
		if strings.EqualFold(strings.TrimSpace(t.Key), "name") && t.Value != "" {
			return t.Value
		}
	}
	return inst.InstanceId
}

func tagsToMap(inst ec2Instance) map[string]string {
	if len(inst.Tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(inst.Tags))
	for _, t := range inst.Tags {
		m[t.Key] = t.Value
	}
	return m
}

// osFromPlatform traduz o campo Platform da AWS (só populado pra Windows) num rótulo consistente
// com models.VMInstance.OS — "" (o valor real da AWS pra Linux/outros) vira "linux" como melhor
// palpite, já que essa app hoje só lida com frota Linux/Windows (sem outra opção realista).
func osFromPlatform(platform string) string {
	if strings.EqualFold(platform, "windows") {
		return "windows"
	}
	return "linux"
}

// ListInstances lista as instâncias EC2 visíveis, cruzando com SSM pra popular
// SupportedConnectionModes (heurística — nunca usada pra decidir automaticamente o modo de
// conexão, ver models.ConnectionMode).
func (p *AWSEC2Provider) ListInstances(ctx context.Context, filter models.VMFilter) ([]models.VMInstance, error) {
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := p.baseArgs("ec2", "describe-instances")
	if len(filter.States) > 0 {
		values := make([]string, len(filter.States))
		for i, s := range filter.States {
			values[i] = string(s)
		}
		args = append(args, "--filters", fmt.Sprintf("Name=instance-state-name,Values=%s", strings.Join(values, ",")))
	}

	out, err := p.run(listCtx, args)
	if err != nil {
		return nil, classifyEC2Error(err, "describe-instances", p.profile)
	}

	var resp ec2DescribeInstancesResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse EC2 describe-instances: %w", err)
	}

	// SSM online-check — best-effort: se falhar (perfil sem permissão ssm:DescribeInstanceInformation,
	// SSM indisponível na região, etc.), segue sem marcar nenhuma instância como SSM-viável, nunca
	// aborta a listagem inteira por causa disso.
	ssmOnline := p.ssmOnlineInstanceIDs(listCtx)

	var instances []models.VMInstance
	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			modes := []models.ConnectionMode{models.ConnectionModeSSH} // sempre "potencialmente viável" — não dá pra confirmar porta 22 sem tentar
			if ssmOnline[inst.InstanceId] {
				modes = append(modes, models.ConnectionModeSSM)
			}

			launchTime, _ := time.Parse(time.RFC3339, inst.LaunchTime)

			instances = append(instances, models.VMInstance{
				ID:                       inst.InstanceId,
				Name:                     instanceNameFromTags(inst),
				Provider:                 "aws",
				Region:                   p.region,
				Zone:                     inst.Placement.AvailabilityZone,
				PublicIP:                 inst.PublicIpAddress,
				PrivateIP:                inst.PrivateIpAddress,
				State:                    normalizeEC2State(inst.State.Name),
				OS:                       osFromPlatform(inst.Platform),
				InstanceType:             inst.InstanceType,
				Tags:                     tagsToMap(inst),
				LaunchTime:               launchTime,
				SupportedConnectionModes: modes,
			})
		}
	}

	return applyVMFilter(instances, filter), nil
}

// applyVMFilter aplica os campos de VMFilter que describe-instances não cobre nativamente (tags
// de match exato) — filtro de estado já é feito no lado da AWS via --filters, acima.
func applyVMFilter(instances []models.VMInstance, filter models.VMFilter) []models.VMInstance {
	if len(filter.Tags) == 0 {
		return instances
	}
	filtered := make([]models.VMInstance, 0, len(instances))
	for _, inst := range instances {
		match := true
		for k, v := range filter.Tags {
			if inst.Tags[k] != v {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, inst)
		}
	}
	return filtered
}

// ssmOnlineInstanceIDs devolve o conjunto de InstanceIds com o SSM Agent online — usado só como
// heurística de exibição (SupportedConnectionModes), nunca crítico: falha aqui devolve um mapa
// vazio, nunca propaga erro pro chamador.
func (p *AWSEC2Provider) ssmOnlineInstanceIDs(ctx context.Context) map[string]bool {
	args := p.baseArgs("ssm", "describe-instance-information")
	out, err := p.run(ctx, args)
	if err != nil {
		return map[string]bool{}
	}
	var resp ssmDescribeInstanceInfoResponse
	if json.Unmarshal(out, &resp) != nil {
		return map[string]bool{}
	}
	result := make(map[string]bool, len(resp.InstanceInformationList))
	for _, info := range resp.InstanceInformationList {
		if strings.EqualFold(info.PingStatus, "Online") {
			result[info.InstanceId] = true
		}
	}
	return result
}

// normalizeEC2State traduz o nome de estado da AWS (já em minúsculas, ex: "running") pro
// vocabulário de models.PowerState — a AWS já usa nomes bem próximos, então isso é principalmente
// uma validação/fallback pra "unknown" caso a AWS introduza um estado novo no futuro.
func normalizeEC2State(name string) models.PowerState {
	switch models.PowerState(name) {
	case models.PowerStateRunning, models.PowerStateStopped, models.PowerStatePending,
		models.PowerStateStopping, models.PowerStateShuttingDown, models.PowerStateTerminated:
		return models.PowerState(name)
	default:
		return models.PowerStateUnknown
	}
}

// GetInstance retorna os detalhes de uma única instância — reaproveita ListInstances com um
// --instance-ids explícito em vez de duplicar o parsing.
func (p *AWSEC2Provider) GetInstance(ctx context.Context, instanceID string) (models.VMInstance, error) {
	descCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	args := p.baseArgs("ec2", "describe-instances", "--instance-ids", instanceID)
	out, err := p.run(descCtx, args)
	if err != nil {
		return models.VMInstance{}, classifyEC2Error(err, instanceID, p.profile)
	}

	var resp ec2DescribeInstancesResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return models.VMInstance{}, fmt.Errorf("failed to parse EC2 describe-instances: %w", err)
	}

	ssmOnline := p.ssmOnlineInstanceIDs(descCtx)

	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			modes := []models.ConnectionMode{models.ConnectionModeSSH}
			if ssmOnline[inst.InstanceId] {
				modes = append(modes, models.ConnectionModeSSM)
			}
			launchTime, _ := time.Parse(time.RFC3339, inst.LaunchTime)
			return models.VMInstance{
				ID:                       inst.InstanceId,
				Name:                     instanceNameFromTags(inst),
				Provider:                 "aws",
				Region:                   p.region,
				Zone:                     inst.Placement.AvailabilityZone,
				PublicIP:                 inst.PublicIpAddress,
				PrivateIP:                inst.PrivateIpAddress,
				State:                    normalizeEC2State(inst.State.Name),
				OS:                       osFromPlatform(inst.Platform),
				InstanceType:             inst.InstanceType,
				Tags:                     tagsToMap(inst),
				LaunchTime:               launchTime,
				SupportedConnectionModes: modes,
			}, nil
		}
	}

	return models.VMInstance{}, fmt.Errorf("instância %s não encontrada", instanceID)
}

// StartInstance, StopInstance, RebootInstance disparam a transição de energia — não esperam a
// transição completar (mesmo padrão de ScaleNodeGroup em nodegroup.go: dispara e retorna).

func (p *AWSEC2Provider) StartInstance(ctx context.Context, instanceID string) error {
	return p.powerAction(ctx, "start-instances", instanceID)
}

func (p *AWSEC2Provider) StopInstance(ctx context.Context, instanceID string) error {
	return p.powerAction(ctx, "stop-instances", instanceID)
}

func (p *AWSEC2Provider) RebootInstance(ctx context.Context, instanceID string) error {
	return p.powerAction(ctx, "reboot-instances", instanceID)
}

func (p *AWSEC2Provider) powerAction(ctx context.Context, subcmd, instanceID string) error {
	actionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := p.baseArgs("ec2", subcmd, "--instance-ids", instanceID)
	if _, err := p.run(actionCtx, args); err != nil {
		return classifyEC2Error(err, instanceID, p.profile)
	}
	return nil
}

// ValidateAuth confirma que o perfil AWS consegue de fato chamar a API EC2 — diferente de
// AWSNodeGroupProvider.ValidateAuth (que retorna ErrNotSupported, pois o EKS delega auth ao exec
// plugin do kubeconfig), aqui não há nenhum client-go no meio — o próprio describe-instances é o
// teste de auth, então uma chamada barata e real é o jeito mais direto de validar.
func (p *AWSEC2Provider) ValidateAuth(ctx context.Context) error {
	authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := p.baseArgs("sts", "get-caller-identity")
	if _, err := p.run(authCtx, args); err != nil {
		return classifyEC2Error(err, "", p.profile)
	}
	return nil
}

// classifyEC2Error transforma erros brutos do AWS CLI em mensagens acionáveis — mesmo espírito de
// classifyAWSError (nodegroup.go), mas generalizado pra "recurso" em vez de "cluster" (uma
// instância EC2 não é um cluster) e sem duplicar a lógica de detecção de erro de credencial.
func classifyEC2Error(err error, resourceHint, profile string) error {
	msg := err.Error()
	profileHint := ""
	if profile != "" {
		profileHint = fmt.Sprintf(" --profile %s", profile)
	}
	switch {
	case strings.Contains(msg, "Partial credentials") || strings.Contains(msg, "aws_secret_access_key"):
		return fmt.Errorf("credenciais AWS incompletas (SSO expirado?). Execute: aws sso login%s", profileHint)
	case strings.Contains(msg, "ExpiredToken") || strings.Contains(msg, "expired"):
		return fmt.Errorf("token AWS expirado. Execute: aws sso login%s", profileHint)
	case strings.Contains(msg, "NoCredentialProviders") || strings.Contains(msg, "no credentials"):
		return fmt.Errorf("sem credenciais AWS configuradas para o perfil '%s'. Execute: aws sso login%s", profile, profileHint)
	case strings.Contains(msg, "executable file not found") || strings.Contains(msg, "aws: command not found"):
		return fmt.Errorf("AWS CLI não encontrado no PATH do servidor. Instale: https://aws.amazon.com/cli/")
	case strings.Contains(msg, "InvalidInstanceID") || strings.Contains(msg, "does not exist"):
		return fmt.Errorf("instância '%s' não encontrada na AWS (profile %s). Verifique a região e o profile corretos", resourceHint, profile)
	default:
		return fmt.Errorf("falha ao chamar AWS EC2: %w", err)
	}
}

// compile-time check
var _ cloudprovider.VMProvider = (*AWSEC2Provider)(nil)
