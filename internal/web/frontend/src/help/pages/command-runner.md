## Para que serve

Executa um **comando ou script em lote** em vários **clusters** e **namespaces** de uma vez e mostra a saída de cada um em tempo real. Aceita `kubectl`, `sh`, `bash`, `python` e `go`, e tem um **assistente de IA** que escreve o comando a partir de uma descrição.

## Quando usar

- Rodar a **mesma verificação em muitos clusters** (por exemplo, listar pods com problema em toda a frota).
- Aplicar uma mudança repetitiva em vários namespaces, com cuidado.
- Gerar um comando `kubectl` complexo descrevendo o que você quer em português.

## Como usar (passo a passo)

1. Escolha os **clusters** (há filtro por nome) e os **namespaces** alvo. Deixar o namespace vazio vale como **todos os namespaces**.
2. Escolha a **Linguagem**: `kubectl`, `sh`, `bash`, `python` ou `go`.
3. Escreva o comando. Use os **placeholders** `{{cluster}}` e `{{namespace}}`: a ferramenta troca pelo alvo de cada execução. Há um modelo de exemplo para cada linguagem.
4. Ajuste o **Timeout (s)**: padrão **300**, máximo **1800**.
5. **Executar.** Cada cluster roda em **paralelo** e a saída aparece **linha a linha**, separada por cluster, com um marcador de fim. **Cancelar** (parar sessão) interrompe tudo.
6. O **Assistente AI** gera um comando a partir de uma frase: ele usa o provedor de IA configurado em [AI Diagnostics](ajuda:ai-diagnostics) e conhece os clusters e namespaces que você selecionou.

## Como o comando é executado

O comando roda **no servidor da aplicação**, em um `bash -c`, **como o usuário do sistema que executa o servidor** e **com as variáveis de ambiente dele**. Para `kubectl`, a ferramenta injeta um *wrapper* que força `--context=<cluster>` em **toda** chamada a `kubectl` do script. `python` e `go` são gravados em arquivos temporários (no `go`, com um diretório e `go.mod` temporários) e removidos depois. A saída sai colorida, como em um terminal.

## Permissões

**Executar exige perfil SRE.** Gerar um comando com a IA e cancelar uma sessão não exigem. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Pode alterar ou apagar qualquer coisa**, em vários clusters ao mesmo tempo. Cada execução fica no Change History (`command_runner`) com o **tipo**, os **primeiros 60 caracteres do comando**, os alvos e o resultado (sucesso ou falha). **A saída do comando não é gravada.**

## Cuidados e erros comuns

- **`sh`, `bash`, `python` e `go` executam código arbitrário no servidor**, não só no cluster. O wrapper do `kubectl` só vale para chamadas a `kubectl`: um script pode ler arquivos do servidor, usar outras credenciais e acessar a rede dele. **Use só código que você entende.**
- **Um comando errado multiplica o estrago**: ele roda em todos os clusters e namespaces selecionados. Teste primeiro em **um** alvo, com um comando só de leitura.
- **Cuidado com comandos que apagam** (`kubectl delete`, `rm`): não há confirmação nem desfazer. **Namespace vazio vira "todos"** (`-A`).
- **Não coloque segredos no texto do comando**: os 60 primeiros caracteres vão para o histórico.
- **Código gerado por IA precisa de revisão**: o modelo pode errar a sintaxe ou o alvo. Leia antes de executar.
- **A saída fica só na tela.** Se precisar guardar, copie; ela não é registrada.
- **O comando é interrompido no timeout** (e a saída mostra isso). Para tarefas longas, aumente o tempo, até 1800 s.
- **`go` e `python` dependem de estarem instalados no servidor.**

## Veja também

[AI Diagnostics](ajuda:ai-diagnostics), [Code Editor](ajuda:code-editor), [Explorer](ajuda:explorer).
