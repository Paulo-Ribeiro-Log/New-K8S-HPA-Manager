## Para que serve

Abre **dois editores de YAML lado a lado**, cada um com **o seu cluster, namespace e recurso**, para **comparar e editar** objetos parecidos: por exemplo o mesmo Deployment em homologação e em produção, ou dois ConfigMaps de namespaces diferentes.

## Quando usar

- Descobrir **o que difere** entre o mesmo recurso em dois clusters (hlg e prd, por exemplo).
- Copiar uma configuração de um lado para o outro, editando e aplicando.
- Conferir um recurso contra uma versão anterior que você tem aberta no outro painel.

## Como usar (passo a passo)

1. Escolha **Edição Lado a Lado** no menu de ferramentas. Ela abre em **uma janela** (não troca de aba) e dá para ver em **tela cheia**.
2. Em cada painel (**esquerdo** e **direito**), escolha o **tipo**, o **namespace** e o **recurso**. Os tipos disponíveis são: **Deployment, ConfigMap, Secret, DaemonSet, StatefulSet, Ingress, Namespace, Pod e Gateway**.
3. O **YAML** carrega no editor. Cada painel é **independente**: tem o seu próprio histórico de desfazer e refazer (`Ctrl+Z` e `Ctrl+Y`), o botão de **recarregar do cluster** e o seu próprio diff.
4. Ligue **Clusters diferentes** para escolher, no painel de cada lado, um **cluster** distinto.
5. **Diff Esquerdo × Direito** compara o YAML dos dois painéis. Fica habilitado só quando os **dois lados têm o mesmo tipo de recurso** carregado.
6. Em cada painel, o fluxo de aplicação é o de sempre: **Validar (dry-run)**, ver o **diff** contra o cluster e **aplicar** (com uma confirmação que mostra o "antes" e o "depois").

## Permissões

Ver e comparar não exige o grupo SRE. **Aplicar** usa as mesmas rotas das abas de cada recurso e exige perfil SRE e permissão real de escrita no cluster. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster ao aplicar.** O Aplicar é um `kubectl apply` com o gerenciador de campos `web-resource-editor`, e **não há rollback automático**: a própria confirmação avisa. Ao aplicar, o registro no Change History segue o do tipo de recurso (o de Secret, por exemplo, grava só o SHA-256 do YAML, nunca o conteúdo).

## Cuidados e erros comuns

- **Confira qual painel você está aplicando.** Com dois clusters abertos, é fácil aplicar o YAML certo no cluster errado. O cabeçalho de cada painel mostra o cluster e o namespace: leia antes de confirmar.
- **O YAML de um Secret mostra os valores** (em base64), nos dois painéis. Não deixe a janela aberta com a tela visível.
- **Copiar de um cluster para outro leva junto o que é específico dele**: hosts, nomes, anotações de ambiente e IPs. Revise o diff antes de aplicar.
- **Alguns campos são imutáveis** depois que o objeto existe (em um Pod, por exemplo, quase todos): o cluster recusa e a mensagem aparece em um quadro de erro com sugestão de resolução.
- **Recursos gerenciados por Helm, Argo CD ou Flux** são desfeitos no próximo sync. Ajuste a origem.
- **Alterações que você não aplicou se perdem** ao fechar a janela.

## Veja também

[Explorer](ajuda:explorer), [Deployments](ajuda:deployments), [ConfigMaps](ajuda:configmaps), [Secrets](ajuda:secrets).
