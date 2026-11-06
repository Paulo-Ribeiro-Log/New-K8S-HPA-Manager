const e=`## Para que serve

Mostra os **namespaces** do cluster, o consumo de CPU e memória por namespace e permite ver, editar, criar e **excluir** um namespace.

## Quando usar

- Ver quais namespaces mais consomem recursos (requests) do cluster.
- Entrar nos deployments e pods de um namespace.
- Conferir eventos, quotas, network policies, services e quem tem acesso (RBAC e squads).
- Criar um namespace novo, inclusive para nodes Spot.

## Como usar (passo a passo)

1. Escolha o cluster. Sem nenhum namespace selecionado, a tela mostra os **gráficos de uso** (os 5 maiores em CPU e em memória, mais "outros") e, ao lado, a lista **Workloads**: filtre pelo nome e **clique em um namespace** para ver os Deployments dele e, a partir deles, os pods.
2. Para ver o **namespace em si** (manifesto, quotas, etc.), selecione-o na lista de namespaces. O botão de fechar volta para a visão geral.
3. Com um namespace selecionado, os botões do painel abrem: **Deployments**, **Events**, **Quotas** (ResourceQuotas), **Policies** (NetworkPolicies), **Services** e **RBAC** (grupos e squads declaradas em \`squads.devops.k8s.io\`).
4. O **manifesto YAML** do namespace é editável. O fluxo é [Diff, Validar, Aplicar](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar).
5. **Describe** mostra o equivalente a \`kubectl describe namespace\`. O menu **⋮** tem **Shell** (abre um terminal num pod do namespace, preferindo um que esteja \`Running\`) e **Deletar Namespace**.

## Criar um namespace

**Criar Namespace** pede o nome e o **tipo de node**: normal ou **Spot** (adiciona as tolerations para os pods rodarem em nodes Spot do Azure). Você pode ligar **Adicionar Annotations e Labels** para preencher esses campos num YAML com modelo.

## Certificado TLS (AWX)

Se a integração com o **AWX** estiver configurada, aparece o botão **Certificado TLS**, para instalar ou renovar o certificado do namespace por um job do AWX.

## Permissões

Criar, aplicar e excluir só ficam disponíveis com permissão de escrita no cluster. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster e pode apagar tudo de uma aplicação.** Criar, aplicar e excluir namespace ficam registrados no Change History.

## Cuidados e erros comuns

- **Deletar Namespace apaga tudo que há dentro dele** (deployments, services, secrets, PVCs e os volumes ligados a eles, conforme a política de retenção). O diálogo pede só uma confirmação, **sem digitar o nome**: leia o nome antes de confirmar.
- Namespaces de **sistema** ficam ocultos até você usar o botão de mostrar sistema. Nunca exclua \`kube-system\` e semelhantes.
- A exclusão pode demorar e ficar em \`Terminating\` por causa de *finalizers*; isso é do Kubernetes, não da ferramenta.
- Os gráficos usam os **requests** dos pods, não o consumo real.
- Para remover só uma aplicação, a [Remoção de Aplicação](ajuda:app-removal) é mais segura que apagar o namespace.

## Veja também

[Deployments](ajuda:deployments), [Pods](ajuda:pods), [Events](ajuda:events), [Remoção de Aplicação](ajuda:app-removal).
`;export{e as default};
