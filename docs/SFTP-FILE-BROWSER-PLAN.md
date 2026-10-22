# Plano: Navegador de Arquivos via SFTP embutido

**Status**: ✅ implementado.

## Pedido original

> "e como fazer uma forma de tranferencia de arquivos usando sftp ?"

Perguntado logo depois do Port Forward. Ofereci 3 abordagens (servidor SFTP embutido com
interface externa via cliente real / reaproveitar o Port Forward pra apontar num pod que já tenha
SSH / só adicionar upload no modal de download já existente). Resposta do usuário:

> "a opção 1 é a preferivel, mas com uma inteface em nossa aplicação."

Ou seja: robustez de protocolo SFTP real, mas sem depender de WinSCP/FileZilla/`sftp` externo —
tudo dentro da própria UI da aplicação.

## Desenho

O app já tinha DOIS mecanismos de transferência que **não** servem pra isso:

- Download-only (`PodFileTransferModal.tsx` + `internal/kubernetes.Client.CopyFromPod`, via
  `kubectl cp`) — sem upload, sem mkdir/rename/remove.
- `internal/config/portforward.go`/o pacote novo de Port Forward — encaminham porta de rede pro
  pod, mas isso só ajudaria SFTP se o pod **já tivesse um servidor SSH/SFTP de verdade rodando**
  (raro em pods de aplicação comuns).

### Servidor SFTP real, in-process, sem porta de rede nenhuma

`internal/podsftp/` implementa as 4 interfaces que `github.com/pkg/sftp` precisa pra rodar como
**servidor** (`FileReader`/`FileWriter`/`FileCmder`/`FileLister`), cada uma traduzindo a operação
SFTP pra `kubectl exec`/`kubectl cp` contra o pod (via `internal/kubernetes.Client`, reaproveitando
`CopyFromPod`/`ListDirectory` já existentes + primitivas novas em `pod_file_ops.go`: `CopyToPod`,
`MkdirInPod`, `RemoveInPod`, `RenameInPod`, `StatInPod`).

O truque pra nunca expor uma porta de rede: o server E o client do `pkg/sftp` rodam no **mesmo
processo Go**, conectados por um `net.Pipe()` em memória (`sftp.NewRequestServer` de um lado,
`sftp.NewClientPipe` do outro). O protocolo SFTP real garante correção (offsets de leitura/escrita,
EOF, IDs de requisição concorrentes) sem precisar reimplementar nada disso à mão — só a tradução
pra `kubectl` do lado do servidor.

`internal/web/handlers/pod_sftp.go` expõe isso via REST simples (list/download/upload/mkdir/
rename/remove) que o modal do frontend (`PodSFTPModal.tsx`) consome — cada chamada abre uma sessão
SFTP nova, faz UMA operação, fecha (deliberadamente sem persistência entre requisições — cada
operação já paga o custo de um subprocesso `kubectl exec`/`cp` de qualquer forma, então manter uma
sessão viva entre chamadas não economiza nada e só adicionaria risco de vazamento).

### Nome de arquivo do handler

`internal/web/handlers/portforward.go` já existia (infraestrutura antiga do Kiali, não relacionada)
— o handler novo desta feature foi pro `internal/web/handlers/pod_sftp.go` (nome que não colide).

## Bug real corrigido durante o desenvolvimento

`ListDirectory` (já existente, usado pelo download antigo) usava `ls -la --time-style=...`, que o
`ls` do **BusyBox não reconhece** (confirmado ao vivo contra `nginx-ingress-controller`, imagem
comum baseada em BusyBox). O erro do BusyBox era parseado como se fosse listagem de arquivo real
(entradas fantasma). Corrigido removendo `--time-style` (GNU e BusyBox usam o mesmo formato padrão
sem essa flag) + parser novo pro formato de 3 tokens de data ("Mon DD HH:MM"/"Mon DD YYYY"). Corrige
retroativamente o `/browse` já existente também, não só o SFTP novo.

## Validação

Testado ponta a ponta via API HTTP real contra um pod real (`nginx-ingress-controller`, AKS):
list, mkdir, upload, download, rename, remove (arquivo e pasta) — cada resultado conferido de forma
independente via `kubectl exec` direto (fora da aplicação).
