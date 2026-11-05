## Para que serve

Inventaria os **certificados TLS** do ambiente: os guardados em **Secrets** dos clusters, os de **endpoints externos** (fora do Kubernetes) e os de **VMs**, mostra o que **vence**, valida a cadeia e permite **instalar, copiar e atualizar** certificados, com **backup e rollback**.

## Quando usar

- Saber **o que vai vencer** nos próximos 30 dias, em todos os clusters de uma vez.
- Renovar um certificado em vários clusters e namespaces.
- Desfazer uma troca de certificado que deu problema.
- Monitorar a validade de servidores fora do Kubernetes (por exemplo, um AD ou um balanceador).
- Atualizar o **CA Bundle** de um webhook depois de rotacionar o certificado dele.

## As quatro sub-abas

| Sub-aba | O que faz |
|---|---|
| **Certificados K8s** | escaneia os Secrets TLS dos clusters, valida a cadeia e permite copiar, instalar e atualizar |
| **Endpoints Externos** | monitora a validade de servidores **fora de qualquer cluster**, por handshake TLS |
| **Webhooks (CA Bundle)** | atualiza o `caBundle` de uma `MutatingWebhookConfiguration` |
| **Certificados em VM** | lê, valida, grava na VM o par certificado e chave e reinicia o serviço (a aba VMs / EC2 explica o transporte por SFTP e SSM) |

## Certificados K8s

1. Escolha os **clusters** (há filtro por nome e agrupamento) e clique em **Escanear**.
2. O resultado traz contagens **Válidos**, **Expirando (<30d)** e **Expirados**, e dois alertas extras: **Com Alerta de Cadeia** (a cadeia x509 não valida) e **Conflito de Host/SAN** (o SAN não cobre o host do Ingress ou Gateway, host duplicado ou aviso de re-encryption). Dá para filtrar (por exemplo, **Usados por Ingress** ou **Comuns (multi-ref)**) e buscar por nome, namespace, subject ou SAN.
3. **Clique em um certificado** para ver o detalhe: subject, issuer, SANs, validade, cadeia, onde é usado e a checagem do TLS do backend.
4. **Exportar relatório** gera um PDF dos certificados filtrados.

**Ações de escrita:**

| Ação | O que faz |
|---|---|
| **Copiar** | copia um Secret TLS para outros clusters ou namespaces |
| **Upload** | cria ou atualiza um Secret TLS a partir de um `tls.crt` e um `tls.key` em PEM, em vários clusters e namespaces |
| **Atualização em Massa** | atualiza o certificado de um Secret **nos clusters onde ele já existe** |
| **AWX** | instala ou renova **pelo AWX**, um destino por vez (alternativa ao modo manual) |
| **Backup e Rollback** | guarda o estado anterior antes de sobrescrever e permite voltar a uma versão (os **20 mais recentes** por Secret); **Copiar para Backup** cria um backup manual, com comentário |
| **Extrair de .pfx** | decodifica um `.pfx`/`.p12` (até 1 MB) e guarda o par certificado e chave para reutilizar |

## Endpoints Externos

Cadastre um **host e porta** (por exemplo, `ad.datacenter.local`) e o monitor faz um **handshake TLS** para ler o certificado e a validade; **Verificar agora** testa todos. Dá para importar uma lista em lote (`host[:porta]`, uma por linha) e exportar. Serve para servidores que nunca aparecem em um scan de cluster.

## Webhooks (CA Bundle)

Lista as `MutatingWebhookConfiguration` do cluster. Escolha a configuração, as **entradas** (`webhooks[]`) a atualizar e cole o **novo CA Bundle (PEM)**. A ferramenta confirma antes de sobrescrever e mostra o novo subject, issuer e validade.

## Permissões

Ler e escanear não exige o grupo SRE. **Copiar, upload, atualização em massa, backup, rollback, ler o conteúdo de backups (que expõe a chave privada), extrair `.pfx`, restaurar e atualizar CA Bundle exigem perfil SRE** e permissão real de escrita nos Secrets do cluster. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster e pode derrubar tráfego.** Ficam no Change History: copiar (`cert_copy`), upload (`cert_upload`), **rollback** (`cert-rollback`) e atualização de **CA Bundle** (`webhook-cabundle-update`). **O `tls.crt` e o `tls.key` nunca vão para o histórico**: só destinos, subject, serial e validade.

## Cuidados e erros comuns

- **Trocar um certificado afeta o tráfego na hora.** Um certificado errado (chave que não casa, cadeia incompleta, SAN que não cobre o host) derruba o HTTPS do Ingress. **Valide a cadeia** e confira o SAN antes de instalar.
- **Atualizar o CA Bundle com o PEM errado** faz o kube-apiserver **deixar de confiar no webhook**. Dependendo do `failurePolicy`, isso pode **bloquear a criação e a edição de recursos** que dependem dele. Confira o PEM colado.
- **Chaves privadas ficam em arquivos no servidor**: os backups de rollback, os backups manuais e as extrações de `.pfx` guardam o `tls.key` em arquivos com permissão restrita ao dono (`0600`, em `~/.k8s-hpa-manager/`), **sem criptografia adicional**. Apague o que não precisa mais.
- **A senha do `.pfx` nunca é guardada**: é usada uma vez e descartada.
- **O scan lê Secrets de todos os clusters escolhidos**; em frotas grandes pode demorar.
- **"Expirando" é um corte fixo de 30 dias.** Um certificado com 31 dias aparece como válido.
- **Em atualização em massa pelo AWX, só um destino vale por vez.**

## Veja também

[Secrets](ajuda:secrets), [Ingresses](ajuda:ingresses).
