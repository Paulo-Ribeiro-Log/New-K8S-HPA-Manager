const o=`## Para que serve

Compara a **versão que está em produção** de uma aplicação com uma **nova tag do GitHub** e mostra **o que muda**: commits, arquivos alterados e linhas adicionadas e removidas. Serve para **revisar uma mudança (CHG) antes de ir para produção**, com os dados da CHG do ServiceNow.

## Quando usar

- Revisar **o que vai subir** em uma CHG: quais commits e arquivos entram.
- Conferir se a versão em produção é a que você acha que é.
- Preparar uma lista de comparações (**lote**) para um dia de mudanças.

## Como funciona

A ferramenta mantém uma **base de deployments** com a tag de imagem em uso em cada cluster. Quando você informa o **nome do deployment** (por exemplo, \`vv-geolocalizacao-api\`), ela acha a **tag em produção** nessa base e pede ao **GitHub** a comparação entre essa tag e a **nova tag** que você digitou, no repositório da aplicação.

## Como usar (passo a passo)

1. **Configure o seu token do GitHub** no botão **Token** (a API usa a sua conta). Sem ele, a comparação não roda.
2. **Atualize a base** com o botão **Scan** (escaneia os clusters e popula a base com as versões em uso). A aba faz **um scan automático por dia**, ao ser aberta.
3. Para uma comparação avulsa, abra a **Busca Manual de Release** e informe: o **Nome do Deployment** (da base), o **Repositório GitHub**, a **Tag em Produção** (confirme ou ajuste; dá para **informar a versão manualmente** se o deployment não está na base) e a **Tag da Nova Release**. Use **Comparar Agora**.
4. O resultado mostra a **versão atual**, o **namespace**, a **idade** do deployment em produção, o **total de commits**, os **arquivos alterados** (com as linhas adicionadas e removidas) e um atalho **Ver no GitHub**. Se nada mudou, aparece **Nenhuma alteração**.
5. Para trabalhar em **lote**, **adicione a comparação ao lote**: o lote **fica salvo no seu navegador** e você pode refazer, remover e buscar por CHG, nome, repositório ou tag.
6. Para **importar os dados de uma CHG** do ServiceNow, use **Importar dados de uma CHG**: há um modo pelo **Teams** (mensagens do bot de CHG), um **via navegador** (a ferramenta abre um Chromium e você faz o login do Azure AD se pedir) e um **colando o texto** do campo "Motivo da mudança". Também dá para **colar o link de aprovação SRE** da CHG.

## Permissões

Consultar e comparar não exige o grupo SRE; as rotas usam o **seu token do GitHub**, então você só vê os repositórios que a sua conta enxerga. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Somente leitura** no GitHub, no ServiceNow e no cluster. O **Scan** só lê Deployments, CronJobs, StatefulSets e DaemonSets para guardar as versões. A importação via navegador usa a **sessão** do ServiceNow e do Teams do servidor.

## Cuidados e erros comuns

- **"Deployment não encontrado em produção"**: o nome não está na base. Rode o **Scan** e confira o nome, ou informe a tag à mão.
- **A tag em produção vem do último scan.** Se houve deploy depois, o resultado fica velho: faça um novo **Scan**.
- **O repositório precisa ser o certo.** A ferramenta compara no repositório que você informou; um repositório errado dá comparação vazia ou erro.
- **"Ver no GitHub" abre com a sua sessão do navegador**, que pode ser de **outra conta** além da do token. Confira com qual conta você está logado.
- **Erros de SAML** do GitHub indicam que o token precisa ser **autorizado para a organização** (SSO). Autorize o token e tente de novo.
- **O diff do GitHub é limitado**: comparações muito grandes podem vir truncadas.
- **O lote é local**: ele não é compartilhado com outros usuários nem entre navegadores.
- **Servidor no WSL sem modo gráfico (WSLg):** o login do ServiceNow e do Teams precisa de um Chrome com display. Sem ele aparece um aviso com o botão **Ver tutorial de instalação** (passo a passo com comandos copiáveis e **Testar de novo**). Enquanto isso, links abertos pelo servidor (inclusive o \`az login\`) vão para o navegador padrão do Windows; \`K8S_HPA_BROWSER=windows|linux|auto\` força a escolha, e \`new-k8s-hpa diagnose-display\` mostra o diagnóstico.
- **A extração via navegador pode travar por vários minutos** no login do Azure AD; **Cancelar** interrompe de verdade.

## Veja também

[Deployments](ajuda:deployments), [Code Editor](ajuda:code-editor).
`;export{o as default};
