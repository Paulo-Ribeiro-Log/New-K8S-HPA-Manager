## Para que serve

Lista as **instâncias EC2** da AWS e permite **ligar, desligar e reiniciar**, abrir um **terminal** (por SSH ou por SSM), navegar e **transferir arquivos** (SFTP) e gerenciar seus **perfis de credencial SSH**. Hoje só existe a AWS EC2.

## Quando usar

- Ver o estado e os IPs das VMs de um profile e região.
- Ligar ou reiniciar uma VM parada ou travada.
- Entrar em uma VM para diagnóstico, sem abrir um cliente SSH à parte.
- Enviar ou baixar um arquivo de uma VM (por exemplo, um certificado).

## Como usar (passo a passo)

1. Escolha o **Profile AWS** e a **Região** (há atalhos para `us-east-1`, `us-east-2`, `us-west-2` e `sa-east-1`; o campo aceita qualquer região). **Atualizar** relê da AWS.
2. A lista mostra cada instância com nome, **estado** (Rodando, Parada, Iniciando, Parando, Terminada), tipo, sistema (Linux ou Windows), IPs privado e público e um selo **SSM Agent detectado** quando a instância tem o agente. A busca filtra por nome, ID, IP e tipo.
3. As ações por instância só ficam habilitadas com a instância **rodando**:
   - **SSH:** abre o terminal por SSH;
   - **SSM:** abre o terminal pelo **Session Manager** da AWS;
   - **pasta (SFTP):** abre o gerenciador de arquivos;
   - **⋮ (energia):** **Iniciar**, **Parar** e **Reiniciar**, cada um com um diálogo de confirmação.
4. **Perfis de Credencial SSH** (no topo) cria, edita e remove os seus perfis.
5. Se o servidor não tem o Session Manager disponível, a aba mostra o selo **SSM indisponível neste servidor**.

## Terminal: SSH e SSM

O fluxo é o mesmo nos dois modos: a conexão é feita **sem exibir o terminal**, é executado um **`sudo su -`**, aparece um **navegador de pastas** para você escolher o diretório e só então o terminal abre. Dá para voltar ao navegador sem perder a sessão.

| Modo | Precisa de | Observação |
|---|---|---|
| **SSH** | host e porta (padrão 22) e um **perfil de credencial SSH** | a primeira conexão pede para **confirmar a impressão digital (fingerprint) do host** |
| **SSM** | profile e região AWS (autorização por IAM) e o **SSM Agent** na instância | não pede credencial SSH nem exige `sshd` |

**Confirmação de host key (SSH):** na primeira vez que o app acessa um servidor, ele mostra a fingerprint e pede confirmação. **Confira com o administrador da VM** antes de aceitar: aceitar uma fingerprint errada pode expor suas credenciais. A confiança vale uma vez e é compartilhada entre o terminal e o SFTP. Se você não responder em 2 minutos, a conexão é encerrada.

## Perfis de credencial SSH

Cada perfil tem um **nome**, o **usuário SSH** e o método de autenticação: **chave privada** ou **senha**. Para a chave, há três origens: **colar** uma chave, **gerar** um par novo (**ED25519**, recomendado, ou RSA com o tamanho que escolher) ou **importar** uma chave de `~/.ssh` **da máquina do servidor**; a passphrase é opcional. Os perfis são **por usuário** e ficam **criptografados** (AES-256-GCM); o segredo não volta para o navegador.

## Arquivos (SFTP)

Lista, baixa, envia, cria pasta, renomeia e remove. Funciona por SSH direto, por **SSM com túnel** (o app abre um túnel `aws ssm` até a porta 22 e o SFTP passa por ele, com a mesma credencial) e também pelo modo **SSM sem SSH**, para instâncias sem `sshd`.

## Certificados em VM

A sub-aba **Certificados em VM**, dentro de Certificados TLS, lê e valida o par certificado e chave, grava na VM e reinicia o serviço (nginx, apache e semelhantes). Usa o mesmo transporte (SFTP ou SSM).

## Permissões

Listar instâncias e arquivos é leitura. **Ligar, parar, reiniciar, terminal, túnel e as operações de escrita de arquivo exigem perfil SRE.** Ler um certificado por SSM também, porque executa um comando remoto. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera máquinas reais e abre acesso a elas.** Ficam no Change History: as ações de energia (iniciar, parar, reiniciar), cada conexão de terminal (`vm-ssh-connect`, para SSH e SSM) e as operações de arquivo (`vm-sftp-download`, `vm-sftp-upload`, `vm-sftp-rename`, `vm-sftp-remove`, com o caminho). **Os comandos digitados no terminal não são gravados**, só o fato da conexão. Credenciais nunca vão para o histórico.

## Cuidados e erros comuns

- **Parar ou reiniciar derruba o que roda na VM**; um IP público que não é fixo pode mudar ao ligar de novo. A transição leva de segundos a minutos: o estado pode aparecer como "Iniciando" ou "Parando".
- **O terminal usa `sudo su -`**: você entra como root. Faça só o necessário e saia.
- **Confira a fingerprint** antes de confiar em um host novo.
- **Remover um arquivo por SFTP é definitivo**: não há lixeira.
- Prefira **chave com passphrase** a senha, e **gere um par novo** em vez de importar uma chave pessoal.
- A importação de `~/.ssh` lê as chaves **da máquina do servidor**, não da sua.
- Uma instância sem tag `Name` aparece com o próprio ID como nome.

## Veja também

[Remoção de Aplicação](ajuda:app-removal), [Pods](ajuda:pods).
