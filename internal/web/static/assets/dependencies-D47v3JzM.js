const e=`## Para que serve

Mantém um **registro das dependências externas** das aplicações (bancos, Kafka, Redis, storage, APIs), descobertas nas configurações dos clusters, e permite a **busca reversa**: "quem usa este serviço?". Também tem uma busca **por chave ou valor em Secrets e ConfigMaps**.

## Quando usar

- Saber **quais aplicações dependem** de um banco, uma fila ou um host antes de mudá-lo ou desligá-lo.
- Achar **onde um valor está configurado** (por exemplo, um host antigo ou uma string de conexão) em todos os clusters.
- Obter um inventário das dependências para um relatório (PDF, Markdown ou CSV).

## Como funciona

O servidor **escaneia os clusters** e procura, em ConfigMaps, Secrets e variáveis de ambiente, nomes de host que casam com padrões conhecidos. Cada achado vira uma dependência com **serviço**, **tipo**, **onde foi encontrado** (ConfigMap, Secret ou env, nome e chave), cluster e namespace. Os tipos são: \`rds\`, \`kafka\`, \`eventhub\`, \`redis\`, \`mongodb\`, \`sql\`, \`storage\`, \`api\` e \`other\`. Tudo é gravado em um banco **SQLite local no servidor**, para a busca ser rápida. O escaneamento acontece **automaticamente a cada 24 horas** em todos os clusters, e dá para disparar um scan à mão.

O scan também registra o **storage montado por volume**: Azure Files, Blob (CSI), NFS/EFS e discos, vindos dos PVs/PVCs do cluster. Esses achados têm origem **\`pvc\`** (nome do PVC ou PV), o serviço aparece como \`<conta>.file.core.windows.net\` ou \`<conta>.blob.core.windows.net\`, ou o host do NFS, e o share/container/caminho vai no campo de tópico. Assim, buscar uma conta de storage traz juntos quem a usa por configuração e quem a monta por volume. Cada aplicação que usa o volume vira uma linha; volume sem aplicação aparece sem deployment (referência órfã). Discos (Azure Disk, EBS, GCE PD) também entram: o serviço é o nome do disco e o tópico é o tipo. O **nome do PV** (\`pvc-<uuid>\`) fica no campo de chave, então dá para buscar por ele e achar a aplicação. Essas linhas são um **retrato por cluster**: cada scan substitui as anteriores, então volumes removidos saem do registro. Quando um scan **não consegue atualizar** um cluster (cluster inacessível ou volumes sem permissão de leitura), o card **Status do Registry** mostra um **triângulo de atenção** ao lado do último scan; passe o mouse para ver quais clusters e por quê. Os dados desses clusters são os do scan anterior, e o aviso some quando um novo scan funciona.

## Como usar (passo a passo)

1. Na aba **Registry**, veja o **Status do Registry** (serviços únicos, total de referências, clusters, namespaces e quantidade por tipo), escolha um cluster (ou **Todos os Clusters**) e **escaneie**. A lista traz as dependências com filtro por ambiente (hlg, sit, prd) e tipo, e um campo de filtro livre. Os serviços aparecem em cartões que você expande.
2. Na aba **Busca Reversa**, escolha o modo:
   - **Busca por Serviço:** digite o nome do serviço (aceita coringas \`*\` e \`%\`) e veja **quais recursos o usam**, com cluster, namespace e origem;
   - **Busca em Secrets e ConfigMaps:** procure por **chave** ou por **valor** no índice de conteúdo (veja o aviso abaixo).
3. **Exportar** gera o relatório em **PDF**, **Markdown** ou **CSV** (o CSV usa \`;\` como separador, para abrir direto no Excel em pt-BR).

## Abrir o recurso a partir da lista

Na coluna **Fonte** (no registro e na busca reversa) cada linha é um **link**: ao clicar, a ferramenta **troca para o cluster da dependência**, escolhe o **namespace** e abre o recurso já **filtrado pelo nome**:

- \`configmap: <nome>\` abre a aba **ConfigMaps**; \`secret: <nome>\` abre a aba **Secrets**;
- \`env: …\` (variável de ambiente de um deployment) abre a aba **Deployments**;
- \`pvc: <nome>\` (storage montado por volume) abre o modal **Volumes** do cluster, filtrado pelo nome do PVC ou PV.

Quando a linha também traz o **deployment**, ele aparece logo abaixo como um segundo link (\`deployment: <nome>\`). Se a troca de cluster falhar (VPN, por exemplo), a ferramenta avisa e **não** abre o recurso de outro cluster.

## Busca em Secrets e ConfigMaps (índice de conteúdo)

Este modo é diferente do registro de dependências: ele **guarda o conteúdo** de Secrets e ConfigMaps, ou seja, **chave e valor** (decodificado e em base64), para você poder buscar. Os resultados mostram o valor **escondido** e você escolhe **revelar** (e ver em texto ou em base64) e **copiar**. Valores muito grandes são **truncados** (então um termo que só aparece depois do corte não é achado). O índice é um **retrato por cluster**: cada novo scan substitui o anterior. Depois de um **Resync AKV** (na aba Secrets), o botão de resync no resultado relê só aquele recurso.

## Permissões

Consultar o registro e a busca por serviço não exige o grupo SRE. **Escanear, buscar em Secrets/ConfigMaps, atualizar um recurso e limpar o índice exigem perfil SRE.** Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**O registro é somente leitura nos clusters** (a ferramenta só lê). Mas ela **grava no disco do servidor** o registro e, no modo de Secrets, o **conteúdo dos Secrets e ConfigMaps**.

## Cuidados e erros comuns

- **O índice de Secrets guarda valores reais em texto decodificado, no disco do servidor.** Ele fica no arquivo \`~/.k8s-hpa-manager/dependency-registry.db\`, o mesmo banco do registro, **sem criptografia**. O arquivo é criado com permissão restrita ao dono (\`0600\`, e as instalações antigas são corrigidas quando o servidor reinicia com a versão atual), mas quem tem acesso ao usuário do servidor, ou a um backup do disco, lê os valores. Use **Limpar índice de Secrets e ConfigMaps** quando não precisar mais da busca.
- **Limpar o índice** apaga todo o conteúdo indexado, de **todos os clusters**, e **não afeta** o registro de dependências. Um novo **Atualizar** reconstrói o índice do zero.
- **A descoberta é por padrão de nome**, não por conexão real: uma dependência fora dos padrões não aparece, e uma que casa por acaso pode ser um falso positivo.
- **Os dados valem até o último scan.** Mudanças muito recentes só aparecem depois do próximo scan (a cada 24 horas, ou manual).
- **"Revelar" mostra a credencial na sua tela.** Não compartilhe o print nem deixe aberto.
- A busca em Secrets e ConfigMaps de um cluster exige que o servidor consiga **ler** os Secrets dele; clusters sem permissão ficam de fora.

## Veja também

[Secrets](ajuda:secrets), [ConfigMaps](ajuda:configmaps), [Remoção de Aplicação](ajuda:app-removal).
`;export{e as default};
