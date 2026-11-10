import { describe, expect, it } from "vitest";
import { dependencyTargets, type NavDependency } from "./dependencyNav";

const dep = (o: Partial<NavDependency>): NavDependency => ({
  cluster: "akspriv-a-hlg-admin",
  namespace: "ns1",
  deployment: "",
  source_type: "",
  source_name: "",
  ...o,
});

describe("dependencyTargets", () => {
  it("configmap abre a aba ConfigMaps e oferece o deployment como segundo destino", () => {
    const t = dependencyTargets(dep({ source_type: "configmap", source_name: "app-cfg", deployment: "api" }));
    expect(t.primary).toEqual({ cluster: "akspriv-a-hlg-admin", namespace: "ns1", tab: "configmaps", name: "app-cfg" });
    expect(t.deployment).toEqual({ cluster: "akspriv-a-hlg-admin", namespace: "ns1", tab: "deployments", name: "api" });
  });

  it("secret sem deployment não tem segundo destino", () => {
    const t = dependencyTargets(dep({ source_type: "secret", source_name: "app-secret" }));
    expect(t.primary?.tab).toBe("secrets");
    expect(t.primary?.name).toBe("app-secret");
    expect(t.deployment).toBeUndefined();
  });

  it("env abre o próprio deployment (e não repete como segundo destino)", () => {
    const t = dependencyTargets(dep({ source_type: "env", source_name: "main", deployment: "worker" }));
    expect(t.primary).toMatchObject({ tab: "deployments", name: "worker" });
    expect(t.deployment).toBeUndefined();
  });

  it("env sem deployment não tem destino", () => {
    expect(dependencyTargets(dep({ source_type: "env", source_name: "main" })).primary).toBeUndefined();
  });

  it("pvc abre o modal de Volumes pelo nome do PVC, com o deployment como segundo destino", () => {
    const t = dependencyTargets(dep({ source_type: "pvc", source_name: "data-pg-0", deployment: "pg", namespace: "db" }));
    expect(t.primary).toMatchObject({ tab: "volumes", name: "data-pg-0", namespace: "db" });
    expect(t.deployment).toMatchObject({ tab: "deployments", name: "pg" });
  });

  it("pvc órfão (PV sem PVC, namespace '-') ainda abre Volumes pelo nome do PV", () => {
    const t = dependencyTargets(dep({ source_type: "pvc", source_name: "pv-old", namespace: "-" }));
    expect(t.primary).toMatchObject({ tab: "volumes", name: "pv-old", namespace: "-" });
    expect(t.deployment).toBeUndefined();
  });

  it("origem desconhecida cai no deployment, se houver", () => {
    expect(dependencyTargets(dep({ source_type: "outra", deployment: "d" })).primary).toMatchObject({ tab: "deployments", name: "d" });
    expect(dependencyTargets(dep({ source_type: "outra" })).primary).toBeUndefined();
  });
});
