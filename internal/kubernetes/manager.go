package kubernetes

import (
	"k8s.io/client-go/kubernetes"
)

// KubeManager é um wrapper para gerenciamento de clients Kubernetes
// (compatível com módulo AI Diagnostics)
type KubeManager struct {
	getClientFunc        func(string) (kubernetes.Interface, error)
	executeKubectlDescribe func(cluster, namespace, resourceType, resourceName string) (string, error)
}

// NewKubeManager cria um novo KubeManager
func NewKubeManager(
	getClientFunc func(string) (kubernetes.Interface, error),
	executeKubectlDescribe func(cluster, namespace, resourceType, resourceName string) (string, error),
) *KubeManager {
	return &KubeManager{
		getClientFunc:        getClientFunc,
		executeKubectlDescribe: executeKubectlDescribe,
	}
}

// GetClient retorna client Kubernetes para um cluster
func (km *KubeManager) GetClient(cluster string) (kubernetes.Interface, error) {
	return km.getClientFunc(cluster)
}

// ExecuteKubectlDescribe executa kubectl describe
func (km *KubeManager) ExecuteKubectlDescribe(cluster, namespace, resourceType, resourceName string) (string, error) {
	if km.executeKubectlDescribe == nil {
		return "", nil
	}
	return km.executeKubectlDescribe(cluster, namespace, resourceType, resourceName)
}
