package models

import "time"

// PowerState representa o estado de energia de uma VM/instância, normalizado entre providers
// (AWS EC2 usa "running"/"stopped"/"pending"/"stopping"/"shutting-down"/"terminated" — os mesmos
// nomes já batem com o vocabulário usual de Azure/GCP, então não precisa de tradução por enquanto).
type PowerState string

const (
	PowerStateRunning      PowerState = "running"
	PowerStateStopped      PowerState = "stopped"
	PowerStatePending      PowerState = "pending"
	PowerStateStopping     PowerState = "stopping"
	PowerStateShuttingDown PowerState = "shutting-down"
	PowerStateTerminated   PowerState = "terminated"
	PowerStateUnknown      PowerState = "unknown"
)

// ConnectionMode identifica o transporte usado para abrir terminal/SFTP numa VM — a escolha entre
// os modos é sempre manual (decisão explícita do usuário, ver VM-EC2-PLAN): esta app nunca decide
// sozinha qual modo usar, mesmo quando só um deles parece viável pela heurística de
// SupportedConnectionModes.
type ConnectionMode string

const (
	ConnectionModeSSH ConnectionMode = "ssh"
	ConnectionModeSSM ConnectionMode = "ssm"
)

// VMInstance representa uma VM/instância de qualquer cloud provider (hoje só AWS EC2 implementado
// — ver internal/cloudprovider/aws/ec2.go — mas o formato já é genérico o bastante pra Azure VM/
// GCE no futuro, sem campos específicos demais de uma nuvem só).
type VMInstance struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Provider     string            `json:"provider"` // "aws" | "azure" | "gcp"
	Region       string            `json:"region"`
	Zone         string            `json:"zone,omitempty"`
	PublicIP     string            `json:"publicIp,omitempty"`
	PrivateIP    string            `json:"privateIp,omitempty"`
	State        PowerState        `json:"state"`
	OS           string            `json:"os,omitempty"` // "linux" | "windows" | "" (desconhecido)
	InstanceType string            `json:"instanceType,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	LaunchTime   time.Time         `json:"launchTime,omitempty"`

	// SupportedConnectionModes é uma HEURÍSTICA (ex: SSM só entra aqui se o agente aparecer
	// registrado em `ssm describe-instance-information`) — usada só pra popular badges
	// informativos no frontend, nunca pra esconder um botão ou pré-selecionar um modo de conexão.
	// A escolha do modo é sempre manual, ver ConnectionMode.
	SupportedConnectionModes []ConnectionMode `json:"supportedConnectionModes,omitempty"`
}

// VMFilter restringe uma listagem de instâncias — todos os campos são opcionais (zero-value =
// sem filtro nesse critério).
type VMFilter struct {
	States []PowerState      `json:"states,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}
