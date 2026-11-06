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
| **Branches** | listar, criar e trocar de branch; **comparar dois branches**; merge; tags |
| **Git / Source Control** | ver o que mudou, **adicionar ao staging**, descartar, commit, **pull**, **push**, sincronizar (pull e depois push), stash e **cherry-pick** |
| **Painel Git (lado direito, recolhível)** | branch atual, **histórico** com busca (e seletor de outra branch), remotes, push/pull/fetch e commit com seleção de arquivos; cada commit tem o botão de **cherry-pick** |
| **Log** | histórico de commits (da branch atual ou de **outra branch**, escolhida no seletor), **cherry-pick** e criar tag |
| **Replace** | buscar e substituir em vários arquivos |
| **K8s** | diff, dry-run e apply do manifesto contra um cluster |

Dá ainda para ver o **histórico do arquivo**, o **blame** inline e o **diff** contra o que está no repositório.

## Pull Request e View Plan

**Criar Pull Request** abre o PR do branch atual. **View Plan** mostra os **plans e outputs do PR** (Atlantis, comentários, checks e jobs de plan/diff do GitHub Actions). O botão fica **desabilitado** quando o branch não tem PR, ou quando o PR não tem plan nem output.

## Conflitos de merge

Quando um merge, pull ou cherry-pick gera conflito, o editor lista os arquivos em conflito, deixa **resolver** cada um e permite **finalizar** (commit do merge) ou **abortar** o merge.

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

**Escreve arquivos no servidor, publica no GitHub e pode alterar o cluster.** Ficam no Change History: **commit**, **push**, **criar PR**, **excluir repositório** e **apply no Kubernetes** (com os objetos e o SHA-256 do manifesto, nunca o conteúdo). **Merge, cherry-pick, exclusão de arquivo, tags e substituição em massa não ficam registrados.**

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
