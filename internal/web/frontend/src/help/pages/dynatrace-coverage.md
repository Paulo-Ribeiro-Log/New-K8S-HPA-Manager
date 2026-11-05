## Para que serve

Mostra a **cobertura de deep monitoring do Dynatrace** por namespace no cluster escolhido: quais processos e serviços estão **instrumentados pelo OneAgent**, com que tecnologia e versão, e quais ficaram de fora ou precisam de atenção.

## Quando usar

- Saber **quanto do cluster está monitorado** com deep monitoring (e quanto não está).
- Achar processos com monitoramento **desativado** ou **pendente de reinício**.
- Ver quais **versões do OneAgent** estão em uso, para planejar atualização.
- Gerar uma lista (CSV) para cobrar as equipes dos namespaces.

## Como usar (passo a passo)

1. Escolha o **cluster** no topo do aplicativo (esta aba usa o cluster global). A consulta ao Dynatrace começa sozinha e, em clusters grandes, **pode levar de 1 a 2 minutos**.
2. Os **cartões de resumo** mostram: **Namespaces**, **Pods (processos)**, **% de pods com deep monitoring**, **linhas sem deep monitoring ativo** e **versões de OneAgent** distintas.
3. A tabela lista, por linha, **Namespace**, **Service Name**, **Technology**, **OneAgent Version**, **Deep Monitoring** (status), **Hosts** e **Pods**. Filtre por texto (namespace, serviço ou tecnologia) e por **status**.
4. **Atualizar** refaz a consulta ao Dynatrace; sem isso, o resultado é reaproveitado por 5 minutos. **CSV** exporta as linhas filtradas.

## Os status

| Status | Significado |
|---|---|
| **Ativo** | o deep monitoring está instrumentando o processo (ou o serviço prova que o OneAgent o instrumentou) |
| **Reinício pendente** | o monitoramento foi habilitado, mas o processo precisa ser **reiniciado** para valer |
| **Desativado** | o monitoramento está **desligado** no processo |
| **Não resolvido** | o Dynatrace não informou o estado do processo |
| **Sem serviço** | o processo não tem serviço associado e o estado não foi informado |

Um processo **sem serviço não é necessariamente sem monitoramento**: um worker ou consumer sem endpoint HTTP é instrumentado normalmente e só não gera um serviço. Por isso a tabela traz esses processos com o tipo e o status do **próprio processo**.

## Permissões

É uma aba de **consulta**. Usa as credenciais do Dynatrace que você configurou no **Perfil** (veja a aba [Dynatrace](ajuda:dynatrace)).

## Impacto

**Somente leitura.** Não altera o Dynatrace nem o cluster.

## Cuidados e erros comuns

- **"Dynatrace não configurado":** falta a URL ou o token do tenant no seu perfil.
- **"Nenhum host group com o nome deste cluster foi encontrado":** o cluster **não está monitorado por OneAgent**, ou o *host group* tem outro nome. A ferramenta procura um host group com o nome do cluster.
- **Alguns namespaces ficam de fora de propósito**: `kube-system`, `dynatrace`, `istio-system` e `calico-system`, como no painel de cobertura original.
- **Os números vêm da API clássica do Dynatrace**, então podem diferir do painel de DQL que usa o Grail.
- **A coluna "Pods" conta pods (processos)**, não pods únicos: um pod com vários processos aparece em mais de uma linha.
- **Resultado em cache:** uma mudança muito recente de monitoramento só aparece depois de **Atualizar**.

## Veja também

[Dynatrace](ajuda:dynatrace), [Pods](ajuda:pods).
