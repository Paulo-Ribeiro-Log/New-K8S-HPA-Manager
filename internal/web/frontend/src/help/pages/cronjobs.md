## Para que serve

Lista os **CronJobs** do cluster e permite editar o YAML, **suspender**, disparar um Job na hora, ver e apagar os Jobs, criar Jobs e CronJobs e excluir um CronJob com várias proteções.

## Quando usar

- Ver o agendamento de cada CronJob e se está suspenso.
- Rodar um CronJob agora, sem esperar o horário (**Trigger**).
- Pausar um CronJob que está causando problema (**Suspender**).
- Limpar Jobs antigos ou travados.
- Criar um Job de execução única ou um CronJob novo.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace (ou todos). A busca filtra a lista.
2. **Clique em um CronJob** para abrir o YAML. Para editar, siga [Diff, Validar, Aplicar](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar); o botão **Cancelar** descarta a edição.
3. **Suspender / Ativar** liga ou desliga o campo `suspend` do CronJob.
4. **Trigger** cria um Job a partir do CronJob, depois de uma confirmação.
5. **Jobs** abre a lista dos Jobs criados por esse CronJob, com status (`Running`, `Succeeded`, `Failed`); dali você apaga um Job.
6. **Describe** mostra o equivalente a `kubectl describe`.

## Novo Job ou CronJob

O botão **Novo** oferece **Job (execução única)** e **CronJob (agendado)**. Você edita o YAML (há um modelo) ou usa **Template de <cronjob>**, que carrega o spec do CronJob selecionado. O YAML pode incluir ServiceAccount, ConfigMap, Role e RoleBinding. Tudo é validado antes por dry-run, inclusive pelas políticas Gatekeeper e Kyverno, e **só é criado se tudo passar**. Há ainda a opção **Salvar YAML no GitHub**, para guardar o arquivo numa pasta do repositório.

## Excluir um CronJob

O diálogo de exclusão mostra uma prévia: o agendamento, quantos Jobs existem e quantos estão rodando, e avisa se o CronJob é **gerenciado por Helm, Argo CD ou Flux**. Você escolhe o que fazer com os Jobs (**Apagar junto**, que interrompe o que estiver rodando, ou **Manter os Jobs**, que continuam e os em execução terminam normalmente) e precisa **digitar o nome do CronJob** para confirmar. A exclusão só vale para o **mesmo objeto** que a prévia mostrou: se ele foi recriado nesse meio tempo, o pedido é recusado.

## Permissões

Aplicar, Suspender, Trigger, criar e excluir só ficam disponíveis com permissão de escrita. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster.** Ficam no Change History: aplicar, suspender/ativar, Trigger, criar Job ou CronJob e excluir CronJob ou Job.

## Cuidados e erros comuns

- **CronJob gerenciado por Helm, Argo CD ou Flux é recriado** no próximo sync depois de excluído. Remova pela origem (o chart ou o repositório).
- CronJob criado direto aqui pode ser **removido pelo Helm no próximo `helm upgrade`**; por isso a opção de salvar o YAML no GitHub.
- **Trigger roda o Job de verdade**, com os mesmos efeitos do horário normal (e-mails, escrita em banco, cobranças). Veja o que o job faz antes.
- Apagar um Job `Running` interrompe o processo; a ferramenta pede uma confirmação extra.
- Excluir é definitivo: se for preciso recriar o CronJob, tenha o YAML no Git.

## Veja também

[Deployments](ajuda:deployments), [Pods](ajuda:pods), [Remoção de Aplicação](ajuda:app-removal).
