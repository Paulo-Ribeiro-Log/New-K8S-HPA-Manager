// Para onde uma linha da lista de dependências pode levar: a aba do recurso (ConfigMaps/Secrets/
// Deployments) ou o modal de Volumes, no cluster e namespace da dependência, já filtrado pelo nome.
// Lógica pura, testada em dependencyNav.test.ts.

export interface DependencyNavTarget {
  cluster: string;
  namespace: string;
  tab: "configmaps" | "secrets" | "deployments" | "volumes";
  name: string;
}

export const NAV_LABEL: Record<DependencyNavTarget["tab"], string> = {
  configmaps: "ConfigMap",
  secrets: "Secret",
  deployments: "Deployment",
  volumes: "Volumes",
};

// O mínimo que dependencyTargets precisa de uma linha do registro (DependencyRecord é estruturalmente compatível).
export interface NavDependency {
  cluster: string;
  namespace: string;
  deployment: string;
  source_type: string;
  source_name: string;
}

// dependencyTargets decide o destino principal pela origem da dependência e, quando a linha traz o
// deployment e o principal é outro recurso, um destino secundário para o deployment.
export function dependencyTargets(dep: NavDependency): { primary?: DependencyNavTarget; deployment?: DependencyNavTarget } {
  const base = { cluster: dep.cluster, namespace: dep.namespace };
  const dpl: DependencyNavTarget | undefined = dep.deployment
    ? { ...base, tab: "deployments", name: dep.deployment }
    : undefined;
  switch (dep.source_type) {
    case "configmap":
      return { primary: { ...base, tab: "configmaps", name: dep.source_name }, deployment: dpl };
    case "secret":
      return { primary: { ...base, tab: "secrets", name: dep.source_name }, deployment: dpl };
    case "pvc":
      // namespace "-" = PV sem PVC (sem namespace): o modal de Volumes não precisa dele
      return { primary: { ...base, tab: "volumes", name: dep.source_name }, deployment: dpl };
    default:
      return { primary: dpl }; // env: o recurso é o próprio deployment
  }
}
