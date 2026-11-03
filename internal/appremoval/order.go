package appremoval

import "sort"

// removalPlan é a ordem de remoção, de cima para baixo. Ela existe porque o que sobra pode ser
// recriado por quem ainda está de pé: o release Helm reaplica tudo; o autoscaler briga com a
// remoção do controller; o CronJob cria Jobs novos; o StatefulSet recria PVCs; o ExternalSecret
// recria o Secret. Por isso: primeiro quem recria, depois o que é recriado.
var removalPlan = []struct {
	Title string
	Kinds []string
}{
	{"Desinstalar o release Helm", []string{KindHelmRelease}},
	{"Cortar o tráfego", []string{KindIngress, KindHTTPRoute, KindVirtualService}},
	{"Parar o autoscaling", []string{KindHPA}},
	{"Parar os CronJobs", []string{KindCronJob}},
	{"Remover os controllers (aguarda os pods saírem)", []string{KindDeployment, KindStatefulSet, KindDaemonSet}},
	{"Políticas e monitoramento", []string{KindPDB, KindNetworkPolicy, KindServiceMonitor, KindPodMonitor}},
	{"Services", []string{KindService}},
	{"Fontes externas de segredo", []string{KindExternalSecret}},
	{"ConfigMaps e Secrets", []string{KindConfigMap, KindSecret}},
	{"Volumes (PVC)", []string{KindPVC}},
	{"RBAC", []string{KindRoleBinding, KindRole}},
	{"ServiceAccounts", []string{KindSA}},
}

// removalRank devolve a posição do kind no plano (etapa e, dentro dela, a ordem em que os kinds
// estão listados); kinds desconhecidos vão por último.
func removalRank(kind string) int {
	for i, step := range removalPlan {
		for j, k := range step.Kinds {
			if k == kind {
				return i*10 + j
			}
		}
	}
	return len(removalPlan) * 10
}

// stepTitle é o título da etapa do kind, mostrado ao usuário.
func stepTitle(kind string) string {
	if r := removalRank(kind); r < len(removalPlan)*10 {
		return removalPlan[r/10].Title
	}
	return "Outros"
}

// sortByRemovalPlan preenche Step e ordena pelo plano (dentro do mesmo kind, por nome).
func sortByRemovalPlan(items []Item) {
	for i := range items {
		items[i].Step = stepTitle(items[i].Kind)
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := removalRank(items[i].Kind), removalRank(items[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return items[i].Name < items[j].Name
	})
}
