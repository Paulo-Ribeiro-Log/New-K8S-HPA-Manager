package cloudprovider

import (
	"context"
	"errors"

	"k8s-hpa-manager/internal/models"
)

// ErrNotSupported é retornado quando uma operação não é suportada pelo cloud provider.
var ErrNotSupported = errors.New("operação não suportada por este cloud provider")

// NodeGroupProvider abstrai operações de node groups/pools por cloud provider.
// AzureNodeGroupProvider usa az CLI; AWSNodeGroupProvider usa aws CLI.
type NodeGroupProvider interface {
	// ListNodeGroups retorna todos os node groups/pools de um cluster.
	ListNodeGroups(ctx context.Context, cluster string) ([]models.NodePool, error)

	// ScaleNodeGroup define o número desejado de nodes em um grupo.
	ScaleNodeGroup(ctx context.Context, cluster, group string, count int) error

	// SetAutoscaling habilita ou desabilita o autoscaler com min/max nodes.
	SetAutoscaling(ctx context.Context, cluster, group string, enable bool, min, max int) error

	// AbortOperation cancela uma operação em andamento no provider.
	// Retorna ErrNotSupported se o provider não suportar abort.
	AbortOperation(ctx context.Context, cluster, group string) error

	// ValidateAuth verifica se a autenticação com o cloud provider está ativa.
	ValidateAuth(ctx context.Context) error
}
