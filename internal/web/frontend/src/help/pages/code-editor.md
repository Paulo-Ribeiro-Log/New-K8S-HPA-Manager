## Para que serve

Um editor de código no navegador, com **Git completo**, **terminal** e **integração com o Kubernetes**. Serve para editar manifestos, charts, Terraform e código em repositórios do **GitHub** (clonados no servidor) ou em uma **pasta da máquina do servidor**, e aplicar manifestos direto em um cluster.

## Quando usar

- Corrigir um manifesto, um values ou um arquivo Terraform e abrir um Pull Request.
- Revisar o que mudou em um branch, o histórico de um arquivo ou quem alterou cada linha (**blame**).
- Testar um manifesto contra um cluster (diff, dry-run e apply) sem sair do editor.
- Resolver conflitos de merge.

## Como usar (passo a passo)

1. **Abrir um repositório.** Clone um repositório do GitHub com a sua conta (o servidor guarda o clone em `~/.k8s-hpa-manager/repos`; o clone exige pelo menos **500 MB livres**) ou use **Abrir pasta** para **editar uma pasta desta máquina, sem clonar**. Dá para ter **várias contas GitHub** e alternar entre elas em **Gerenciar contas GitHub**.
2. **Árvore de arquivos.** Cria, renomeia, move, copia, exclui e envia arquivos; pastas e arquivos novos, **Revelar na tree** e **Copiar caminho**. Arquivos acima de **5 MB** não abrem.
3. **Editar.** O editor tem abas, **dividir editor** (com comparação entre as duas janelas), realce por linguagem, desfazer e atalhos de VS Code: `Ctrl+S` salva, `Ctrl+P` abre o **Quick Open** de arquivos e `Ctrl+B` recolhe a barra lateral.
4. **Buscar e substituir** em todos os arquivos do repositório (painel **replace**).
5. **Formatar.** O botão **Fmt** e a opção **Fmt ao salvar** formatam `.go` (gofmt), `.json`, `.yaml`/`.yml` e `.tf`/`.tfvars` (terraform fmt). Outras linguagens não têm formatador.
6. **Terminal integrado.** Abre um shell na raiz do repositório (veja os avisos abaixo).

## Painéis laterais

| Painel | Para que serve |
|---|---|
| **Arquivos** | árvore do repositório |
| **Branches** | listar, criar e trocar de branch; **renomear** e **apagar** branch; **comparar dois branches**; merge; tags |
| **Git / Source Control** | ver o que mudou, **adicionar ao staging**, descartar, commit, **pull**, **push**, sincronizar (pull e depois push), stash e **cherry-pick** |
| **Painel Git (lado direito, recolhível)** | branch atual, **histórico** com busca (e seletor de outra branch), remotes, push/pull/fetch e commit com seleção de arquivos; cada commit tem o botão de **cherry-pick** |
| **Log** | histórico de commits (da branch atual ou de **outra branch**, escolhida no seletor), **cherry-pick** e criar tag |
| **Replace** | buscar e substituir em vários arquivos |
| **K8s** | diff, dry-run e apply do manifesto contra um cluster |

Dá ainda para ver o **histórico do arquivo**, o **blame** inline e o **diff** contra o que está no repositório.

## Pull Request e View Plan

**Criar Pull Request** abre o PR do branch atual. **View Plan** mostra os **plans e outputs do PR** (Atlantis, comentários, checks e jobs de plan/diff do GitHub Actions). O botão fica **desabilitado** quando o branch não tem PR, ou quando o PR não tem plan nem output.

**Diff PR** (botão ao lado de View Plan) abre o **diff do Pull Request** da branch atual, somente leitura: os arquivos alterados à esquerda e o patch do arquivo selecionado à direita (linhas adicionadas em verde, removidas em vermelho). Tem **Copiar link** e **Abrir no GitHub**. Fica desabilitado quando a branch não tem PR. Arquivo binário ou grande demais não vem com patch do GitHub — use **Abrir no GitHub**.

**Aprovar o PR pelo diff.** No modal do **Diff PR**, a barra **Revisão do PR** (embaixo) mostra quem já revisou (**aprovou**, **pediu alterações**, **comentou**; revisão feita num commit antigo vem marcada). Ao abri-la, escolha **Comentar**, **Aprovar** ou **Solicitar alterações**, escreva o texto (obrigatório, exceto para aprovar) e envie. Pontos de segurança:

- A revisão é enviada ao **GitHub em nome do seu usuário** (o PAT do perfil ativo), e a tela diz como quem e para qual commit.
- Ela fica presa ao **commit que este diff mostra**. Se o PR recebeu commits novos depois que você abriu o diff, o envio é **recusado** e a tela oferece **Recarregar**: não se aprova código que não foi visto.
- **Aprovar** e **Solicitar alterações** pedem **confirmação**.
- Você **não pode aprovar o próprio PR** (regra do GitHub): nesse caso só dá para comentar, e a tela explica. PR **fechado ou mergeado** não recebe revisão.
- O PAT precisa da permissão **Pull requests: Read and write** (fine-grained) ou o escopo `repo` (clássico); sem ela, o erro orienta a corrigir o token.
- No Change History fica `code_editor_pr_review` com o PR, o tipo de revisão e o commit, **sem o texto do comentário**.

Esta barra **não faz merge**: só revisa.

**Diff e plan de qualquer branch com PR aberto.** No painel **Branches**, toda branch (a atual, as locais e as remotas) que tem um **Pull Request aberto, ainda não mergeado**, mostra o selo **PR #n** (clique para abrir no GitHub; PR em rascunho vem marcado) e dois ícones: **Diff** (o diff do PR) e **Plan** (os plans e outputs do PR, com as mesmas fontes do View Plan). Eles abrem a **branch do PR escolhido, sem precisar trocar de branch** nem baixá-la: é só leitura do GitHub. O selo some quando o PR é mergeado ou fechado. PRs vindos de **forks** não aparecem. Use o botão de atualizar do painel (**Fetch**) para reconsultar os PRs.

**Cor do selo = status do PR no GitHub.** 🔴 vermelho: conflito com o destino ou checks falhando; 🟠 laranja: alterações solicitadas; ⚪ cinza: rascunho; 🟡 âmbar: checks em andamento; 🟢 verde: aprovado ou checks ok; 🔵 azul: aberto, sem checks nem revisão. Quando vários valem, vence o primeiro da lista. O texto do selo é só **PR #n**; o status fica na cor e no tooltip, que lista cada item (rascunho, checks, revisão, conflito). Se o GitHub não devolver o status (token sem acesso ao GraphQL), o selo aparece azul só com o número. Se a busca de PRs falhar, uma faixa âmbar no painel mostra o motivo.

No modal **Criar Pull Request**, a **branch de origem** (azul) e o **destino** (verde, onde será mergeada) aparecem em blocos separados, com uma frase explicando o merge. Abaixo, o editor simula o merge e mostra se **pode ser mergeado**: verde (sem conflitos), amarelo (destino avançou, nada a mergear ou não deu para simular) ou vermelho (**conflitos**, com os arquivos). A simulação é local, usa `origin/<destino>` (faça **Fetch** para ficar em dia) e não altera seus arquivos.

Ao **criar** o PR, a tela de sucesso mostra a origem → destino, o **status de merge calculado pelo GitHub** (pode levar alguns segundos: sem conflitos, bloqueado por checks/aprovações, desatualizado, rascunho ou conflitos) e o link, com **Copiar**, além de **Abrir no GitHub** e **Ver diff**.

## Conflitos de merge

Quando um merge, pull ou cherry-pick gera conflito, o editor lista os arquivos em conflito, deixa **resolver** cada um e permite **finalizar** (commit do merge) ou **abortar** o merge.

## Renomear e apagar branch

No painel **Branches**, passe o mouse sobre uma branch para ver os ícones de **lápis** (renomear) e **lixeira** (apagar). A branch atual só pode ser renomeada.

- **Renomear:** troca o nome local. Marque **Renomear também no origin** para publicar o nome novo e apagar o antigo do GitHub (PRs abertos da branch antiga são fechados pelo GitHub). Se o remoto falhar, a renomeação local fica feita e a tela avisa.
- **Apagar:** apaga a branch local só se ela já estiver mergeada; se tiver commits que não estão em outro lugar, o editor avisa e oferece **Apagar mesmo assim**. Marque **Apagar também do origin** para remover do GitHub. Branch que existe só no remoto (seção Remotos) mostra a lixeira para apagar do origin.
- **Proteção:** `main` e `master` não são apagadas nem renomeadas no remoto por aqui, e a branch atual não pode ser apagada (troque antes).

## Cherry-pick

1. Faça checkout da branch de **destino** (painel **Branches**).
2. No **painel Git da direita** (seção Histórico), no painel **Git** da esquerda (seção Cherry-pick) ou no **Log**, escolha no seletor a branch de **origem** (locais e remotas; no padrão, a branch atual).
3. Clique no ícone de cereja (**pick**) no commit e confirme. O commit é aplicado na branch atual, mantendo o autor original (você aparece como committer).

Com **conflito**, abre o resolvedor de conflitos ("Resolver Conflitos de Cherry-pick"): resolva os arquivos e finalize, ou use **Abortar Cherry-pick** para voltar ao estado anterior. Qualquer outra falha (ex.: o commit já foi aplicado e ficaria vazio) aborta o cherry-pick sozinho e mostra o motivo. Aplica **um commit por vez**; para vários, repita do mais antigo para o mais novo. Mudanças não commitadas que colidem com o commit fazem o git recusar: faça commit ou stash antes. Branch remota nova só aparece depois de um fetch (botão Atualizar do painel **Branches**).

## Integração com o Kubernetes

No painel **K8s**, escolha o **contexto** (cluster) e use **diff**, **dry-run** (`kubectl apply --dry-run=server`) e **apply** (`kubectl apply`) sobre o manifesto aberto, com o progresso na tela. Há também **buscar o recurso do cluster** (`kubectl get`). O `kubectl` roda **no servidor**, com o acesso que o servidor tem ao cluster.

## Linguagens (LSP)

Há suporte a **autocompletar, hover, ir para a definição e diagnósticos** para **Go** (gopls) e **Python** (pyright), quando o servidor tem esses programas instalados.

## Permissões

**Abrir pasta**, navegar pelas pastas do servidor e o **terminal integrado** exigem perfil SRE. A maioria das ações de Git e de arquivos exige apenas estar autenticado. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Escreve arquivos no servidor, publica no GitHub e pode alterar o cluster.** Ficam no Change History: **commit**, **push**, **criar PR**, **renomear e apagar branch**, **excluir repositório** e **apply no Kubernetes** (com os objetos e o SHA-256 do manifesto, nunca o conteúdo). **Merge, cherry-pick, exclusão de arquivo, tags e substituição em massa não ficam registrados.**

## Cuidados e erros comuns

- **O terminal é um shell no servidor**, com o usuário do sistema que roda a aplicação, na raiz do repositório e **com as variáveis de ambiente do servidor**. É poder demais para uso casual: use só o necessário.
- **Aplicar no Kubernetes muda o cluster de verdade.** Rode sempre **diff** e **dry-run** antes, e confira o **contexto** escolhido.
- **Os clones ficam no servidor e são compartilhados**: a lista mostra todos os repositórios clonados, de qualquer usuário (cada um é identificado por `dono-repo`). **Só quem clonou pode excluir o clone**, porque a exclusão apaga a pasta para todo mundo. Se o clone tiver **mudanças não commitadas** (inclusive arquivos novos), a ferramenta avisa e só exclui se você confirmar; essas mudanças **não têm como ser recuperadas**. Quem tentar excluir o clone de outra pessoa recebe um aviso com o nome do dono, e a tentativa fica no histórico. Clones **antigos, de antes desse registro, não têm dono** e qualquer pessoa pode excluí-los. Em instalações com token estático (sem login individual), todos aparecem como a mesma pessoa, então a trava não diferencia ninguém. **Abrir pasta** não tem essa trava: excluir só remove o atalho, nunca os arquivos da pasta.
- **O commit usa o seu nome e e-mail** (ou o e-mail do seu SSO), não o do aplicativo.
- **Push exige a conta GitHub certa.** Em caso de várias contas, confira qual está ativa antes de fazer push.
- **Edição em "Abrir pasta" altera os arquivos reais** daquela pasta do servidor, sem Git por trás se ela não for um repositório.
- **Os plans do View Plan não são do seu cluster:** são os do PR. Conferir um plan não aplica nada.
- Segredos em manifestos: evite colar valores reais em arquivos que vão para o Git.

## Veja também

[Helm](ajuda:helm), [Deployments](ajuda:deployments).
