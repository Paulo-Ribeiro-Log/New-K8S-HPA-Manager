## Para que serve

Descobre o **caminho de rede** (salto a salto) até um IP ou hostname e **identifica o que há no destino**: portas abertas, serviço e certificado TLS, e um palpite de sistema operacional. Cada salto aparece **no desenho ao vivo** assim que responde.

## Quando usar

- Entender **onde** uma conexão é bloqueada ou fica lenta entre o cluster (ou o servidor) e um destino.
- Descobrir de quem é um IP (nome reverso, organização, região de nuvem) ou se ele é um node, pod ou Service de um dos seus clusters.
- Conferir quais portas de um destino respondem a partir de um ponto de origem específico.
- Acompanhar a **estabilidade** da rota ao longo do tempo (perda e variação por salto).

## Como usar (passo a passo)

1. Escolha **de onde** a sondagem parte:
   - **A partir de um Cluster:** cria um pod temporário no namespace escolhido; reflete a rede real daquele cluster.
   - **Direto do servidor:** roda em um `docker run` na máquina do servidor. Exige Docker (a aba faz a pré-checagem).
2. Escolha o **modo** de entrada:
   - **Único:** um alvo, IP ou hostname.
   - **Lote:** vários alvos (no máximo **10**), um de cada vez, ou em **paralelo** (até 3 simultâneos, sem o grafo ao vivo por item).
   - **Monitor:** o mesmo alvo repetido por N **rodadas**, com um **intervalo** em segundos, para ver a estabilidade da rota.
3. Ajuste a sondagem, se precisar:
   - **Porta da sonda** (padrão **443**): a sonda é um *traceroute TCP*. Alvos que só abrem outras portas (por exemplo, Windows com 3389, 445, 5985 ou 5986) exigem trocar a porta;
   - **Timeout por salto** (padrão 2 s, até 8 s) e **sondas por salto** (padrão 3, no máximo 3): mais de uma sonda permite medir **perda** e **variação** de latência por salto;
   - **Portas extras do fingerprint**: até 10 portas além das ~18 que já são verificadas (22, 80, 443, 445, 3389, 21, 25, 587, 53, 3306, 5432, 6379, 27017, 9200, 8080, 8443, 5985, 5986);
   - **Detecção avançada de serviço (nmap)**: opcional, mais lenta (cada alvo ganha cerca de 7 segundos ou mais), mostra serviço e versão nas portas já abertas;
   - **Certificado de cliente (mTLS)**: opcional, só para alvos que exigem certificado de cliente; dá para colar o PEM ou escolher um já salvo.
4. Execute. O desenho mostra cada salto conforme responde; no fim aparece o veredito **alcançado** ou **não alcançado**.

## O que o resultado mostra

| Parte | Conteúdo |
|---|---|
| **Saltos** | latência (mínima, média e máxima quando há mais de uma amostra), perda e, na coluna **Contexto**, o **DNS reverso**, a **organização/ASN** e a **região de nuvem** (AWS, GCP ou Azure) |
| **Fingerprint do destino** | portas abertas, header `Server:`, certificado TLS e um **palpite** de SO ("provável", nunca certeza) |
| **Serviços (nmap)** | quando a detecção avançada foi ligada |
| **Cruzamento com o Kubernetes** | se o IP é um node, pod ou Service de um cluster (usa um cache gravado; consulta o cluster ao vivo só no modo cluster) |
| **Histórico por alvo** | as buscas anteriores daquele alvo |
| **Exportar** | PDF, CSV, JSON ou **Salvar como Nota** |

## Permissões

Executar, rodar em lote e cancelar exigem perfil SRE. No modo cluster é preciso poder criar pods no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Gera tráfego de sondagem real contra o alvo**: a ferramenta tenta conexões TCP em várias portas do destino e pode rodar `nmap`. Cada execução fica no Change History (`net_discovery`), com alvo, modo, IP resolvido, número de saltos e se foi alcançado.

## Cuidados e erros comuns

- **Use só em alvos seus ou autorizados.** Sondar portas de hosts de terceiros pode violar política da empresa e acionar alarmes de segurança deles. Não há bloqueio por tipo de endereço: a ferramenta aceita qualquer IP ou nome.
- **O IP dos saltos e do alvo é consultado fora do seu ambiente**: o ASN e a organização vêm de consultas **DNS à Team Cymru**, e as faixas de IP de nuvem vêm de arquivos públicos da AWS e do GCP e, no Azure, de uma consulta de *service tags* feita pelo `az` do servidor (guardadas em cache). Se isso for um problema para o alvo, não use a ferramenta com ele.
- **Saltos que não respondem não são necessariamente um problema.** Roteadores costumam ignorar a sonda; o que importa é se o **destino** foi alcançado.
- **Silêncio total** depois de vários saltos, mesmo trocando a porta, indica **bloqueio de firewall**, não porta errada.
- **O palpite de SO é heurístico** (baseado nas portas abertas): trate como pista.
- **O certificado de cliente nunca é gravado**: vive só na memória do formulário e é enviado ao script sem passar por argumentos ou logs. Mesmo assim, use apenas um certificado necessário.
- Um lote em **paralelo** não mostra o grafo ao vivo por item, e o modo **Monitor** não aceita paralelo.
- Cada busca tem um teto de **tempo total** (até 280 s); combinações de timeout e sondas muito altas podem estourar esse teto.
- O pod temporário usa a imagem `nicolaka/netshoot:v0.12` e vive no máximo 5 minutos; no modo local, containers órfãos são removidos por um *reaper*.

## Veja também

[Teste de Latência](ajuda:latency-test), [Pods](ajuda:pods), [Events](ajuda:events).
