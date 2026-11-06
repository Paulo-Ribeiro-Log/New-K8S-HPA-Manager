const e=`## Para que serve

Testa a **conexão com um banco de dados** e, se você quiser, **explora o que há nele em modo somente leitura**: bancos, tabelas, collections ou chaves, com estatísticas e uma amostra dos dados. Suporta **PostgreSQL**, **MySQL/MariaDB**, **MongoDB**, **Redis** e **SQL Server (Azure SQL)**.

## Quando usar

- Descobrir **em qual etapa** a conexão de uma aplicação falha: rede, autenticação ou TLS.
- Confirmar que um workload específico consegue alcançar o banco (NetworkPolicy, Istio).
- Olhar o que existe em um banco sem abrir um cliente à mão.
- Conferir a saúde de um Redis (versão, memória, clientes, taxa de acerto) e, no Azure, o tier do Cache.

## Como usar (passo a passo)

1. Escolha **onde o teste roda**:
   - **Via Pod do Cluster:** anexa um *ephemeral container* a um pod real do **Deployment** que você escolher. A conexão parte **da identidade de rede desse workload**, então reflete NetworkPolicy e Istio. É o modo certo para responder "a aplicação consegue chegar ao banco?".
   - **Direto do servidor (terminal local):** roda o cliente do banco em um \`docker run\` **na máquina do servidor**, sem tocar o cluster. Serve quando o banco é alcançável pela rede do servidor (VPN, endpoint público). Exige **Docker** instalado e funcionando no servidor; a aba faz uma pré-checagem e mostra o que falta.
2. No modo pod, escolha **cluster, namespace e Deployment**. Escolha a **Engine** e o **Timeout** (padrão 5 s, no máximo 15 s).
3. Informe o endereço: **host e porta** digitados, ou lidos de um **ConfigMap** do Kubernetes (chaves de host e de porta). Na modalidade connection string, o endereço já vem nela.
4. Escolha a **autenticação**:
   - **Sem autenticação**;
   - **Usuário e senha**, digitados ou lidos de um **Secret** do K8s (chaves de usuário e de senha; há opção de decodificar mais uma vez em base64, para valores sincronizados do Key Vault). No MongoDB dá para escolher o mecanismo (\`SCRAM-SHA-1\` ou \`SCRAM-SHA-256\`);
   - **Connection string**, digitada ou lida de um **Secret** ou **ConfigMap**. Cada engine aceita seu formato (\`postgresql://\`, \`mysql://\`, \`mongodb://\`, \`redis://\`/\`rediss://\`; no SQL Server, URI, JDBC ou ADO.NET).
5. Marque **Usar TLS** se o banco exige, e **ignorar verificação do certificado** só se precisar.
6. Para explorar, ligue **Explorar dados (só leitura)**. Sem informar o *database*, a ferramenta lista os bancos; informando, desce e lista as tabelas ou collections. No Redis, há um **padrão de chaves** opcional.
7. Execute. O resultado mostra três etapas, **TCP/DNS**, **Autenticação** e **TLS**, cada uma com seu status, para você ver **onde** falhou.

## Explorar e ver amostras

A tabela de estatísticas (ordenável) mostra por tabela ou collection a **contagem** e o **tamanho**, a partir do catálogo do banco, **sem varrer os dados**. No Redis mostra o tipo e o tamanho das chaves. **Clique em uma linha** para abrir uma **amostra dos dados**: página de 20 itens (máximo 100), com **ordenação por coluna** e **Anterior/Próxima**. É uma consulta pontual somente de leitura. No Redis, o topo mostra as estatísticas do servidor e a saída do \`INFO\` organizada em abas.

## Permissões

Executar, visualizar amostras e cancelar exigem perfil SRE. No modo pod, é preciso permissão para criar *ephemeral containers* no pod escolhido. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Não altera o banco**: o teste conecta e, no máximo, lê. Mas **anexa um container a um pod real** (modo pod) ou **roda um container no servidor** (modo local). Cada execução fica no Change History (\`db_test\`), com engine, host, namespace, deployment, modo e resultado resumido, **nunca com usuário, senha ou connection string**.

## Cuidados e erros comuns

- **A senha é usada dentro do comando do cliente** no container (ou no \`docker run\` local). Ela **não vai para o histórico**, mas evite credenciais pessoais ou de alto privilégio; prefira um usuário **somente leitura**.
- **Prefira ler as credenciais de um Secret** em vez de digitar: assim a senha não passa pelo seu navegador.
- **Ephemeral container herda a rede do pod**, mas é preciso que o cluster aceite o recurso e consiga **baixar a imagem do cliente** (por exemplo \`postgres\`, \`mariadb\`, \`mongo\`, \`redis\`). No primeiro uso, o download pode levar até 2 minutos.
- **"unknown_failed"** (etapas em cinza): a saída do cliente não foi reconhecida, como Docker ausente no modo local ou connection string malformada. Veja a saída bruta.
- **Redis e \`KEYS\`:** a exploração usa \`SCAN\` com teto de **100 chaves** (e avisa quando a lista é uma amostra). Nunca roda \`KEYS *\`.
- **Exploração em bancos grandes pode demorar**: o estágio de explorar tem um tempo mínimo de 30 s, independente do timeout de conectividade.
- Ao cancelar no modo local, o container é removido pelo nome; um *reaper* limpa containers órfãos mais velhos que 10 minutos.

## Veja também

[Teste Kafka](ajuda:kafka-test), [Teste de Latência](ajuda:latency-test), [Pods](ajuda:pods).
`;export{e as default};
