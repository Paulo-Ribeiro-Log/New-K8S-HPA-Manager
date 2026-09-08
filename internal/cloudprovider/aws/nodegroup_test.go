package aws

import (
	"errors"
	"strings"
	"testing"
)

// TestNewAWSNodeGroupProvider_FriendlyAliasContext cobre o bug real: contexts do kubeconfig com
// alias amigável (ex: "cluster-apis-prd") diferentes do nome real do cluster na AWS ("cluster-apis")
// faziam toda chamada `aws eks ...` falhar com ResourceNotFoundException, porque o provider usava
// o context como nome de cluster na AWS. O nome real (resolvido via eks-clusters-config.json) e o
// context precisam ser mantidos separados: awsClusterName para as chamadas AWS CLI, contextName só
// para popular NodePool.ClusterName (usado do lado K8s).
func TestNewAWSNodeGroupProvider_FriendlyAliasContext(t *testing.T) {
	p := NewAWSNodeGroupProvider("cluster-apis", "cluster-apis-prd", "sa-east-1", "cnt")

	if p.clusterName != "cluster-apis" {
		t.Errorf("clusterName (usado nas chamadas AWS CLI) = %q, esperado %q", p.clusterName, "cluster-apis")
	}
	if p.contextName != "cluster-apis-prd" {
		t.Errorf("contextName (usado do lado K8s) = %q, esperado %q", p.contextName, "cluster-apis-prd")
	}
	if p.region != "sa-east-1" || p.profile != "cnt" {
		t.Errorf("region/profile = %q/%q, esperado sa-east-1/cnt", p.region, p.profile)
	}
}

// TestNewAWSNodeGroupProvider_ARNContext cobre o caso histórico (sem alias amigável): o context é
// o ARN completo do cluster, e awsClusterName/contextName chegam com o mesmo valor — precisa
// continuar extraindo o nome curto do ARN pras chamadas AWS CLI, preservando o ARN completo em
// contextName (comportamento já existente, não deve regredir com a mudança de assinatura).
func TestNewAWSNodeGroupProvider_ARNContext(t *testing.T) {
	arn := "arn:aws:eks:us-east-1:123456789012:cluster/asaplog-preprod"
	p := NewAWSNodeGroupProvider(arn, arn, "", "asapops")

	if p.clusterName != "asaplog-preprod" {
		t.Errorf("clusterName = %q, esperado nome curto extraído do ARN %q", p.clusterName, "asaplog-preprod")
	}
	if p.contextName != arn {
		t.Errorf("contextName = %q, esperado ARN completo preservado", p.contextName)
	}
	if p.region != "us-east-1" {
		t.Errorf("region = %q, esperado extraída do ARN: %q", p.region, "us-east-1")
	}
}

// TestNewAWSNodeGroupProvider_EmptyContextFallsBackToClusterName garante que passar contextName
// vazio (chamador não tem um context distinto pra oferecer) não deixa o campo vazio — cai pro
// mesmo valor de awsClusterName, mesmo comportamento de antes da mudança de assinatura (quando só
// havia um parâmetro, reaproveitado pros dois campos).
func TestNewAWSNodeGroupProvider_EmptyContextFallsBackToClusterName(t *testing.T) {
	p := NewAWSNodeGroupProvider("my-cluster", "", "", "")
	if p.contextName != "my-cluster" {
		t.Errorf("contextName = %q, esperado fallback pra awsClusterName %q", p.contextName, "my-cluster")
	}
}

// TestClassifyAWSError_ResourceNotFound_UsesClusterNameNotProfile cobre o 2º bug real achado no
// mesmo lote: a mensagem de "cluster não encontrado" citava o PROFILE AWS no lugar do nome do
// cluster que de fato falhou — nunca ajudava a diagnosticar (ex: "cluster 'cnt' não encontrado",
// sendo "cnt" só o profile, não um nome de cluster).
func TestClassifyAWSError_ResourceNotFound_UsesClusterNameNotProfile(t *testing.T) {
	raw := errors.New("An error occurred (ResourceNotFoundException) when calling the ListNodegroups operation: No cluster found for name: cluster-apis-prd.")
	err := classifyAWSError(raw, "cluster-apis-prd", "cnt")

	if !strings.Contains(err.Error(), "cluster-apis-prd") {
		t.Errorf("mensagem de erro não cita o cluster que falhou: %v", err)
	}
	if strings.Contains(err.Error(), "cluster 'cnt'") {
		t.Errorf("mensagem de erro regrediu — voltou a citar o profile como se fosse o cluster: %v", err)
	}
}
