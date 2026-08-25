package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ─── "Descoberta de Rede" — Fase 3 (IP-ROUTE-DISCOVERY-PLAN.md, seções 3.5/3.6/3.7):
// enriquecimento PASSIVO por salto — DNS reverso, ASN/organização (Team Cymru), faixa de nuvem
// pública (AWS/GCP/Azure — Azure adicionado na Fase 5, item P3 do roadmap). Roda inteiramente do
// BACKEND (nunca precisa de pod/container — são consultas DNS/HTTP/CLI simples, sem depender da
// rede de um cluster específico), em paralelo pra todos os saltos que responderam, depois do
// fingerprint do destino (Fase 2).
//
// RDAP (seção 3.6 do plano) fica de fora por ora — é enriquecimento opcional "sob demanda" no
// plano original, não uma camada sempre-ligada.

// netDiscoveryCloudEntry é uma faixa de IP conhecida de um provedor de nuvem pública.
type netDiscoveryCloudEntry struct {
	Net      *net.IPNet
	Provider string // "aws" | "gcp" | "azure"
	Region   string
}

// netDiscoveryCloudRangesTTL — mesmo padrão de cache de chamada externa já documentado no
// CLAUDE.md (ex: AzurePricer/GCPPricer, TTL 24h) — os feeds de IP público mudam raramente.
// Cache em MEMÓRIA (não SQLite): ao contrário dos pricers, não há necessidade de sobreviver a um
// restart do servidor — um fetch novo (rápido, ~200KB) na primeira consulta depois de subir já
// resolve, sem justificar o custo extra de uma tabela SQLite pra isso.
const netDiscoveryCloudRangesTTL = 24 * time.Hour

var (
	cloudRangesMu     sync.RWMutex
	cloudRangesCache  []netDiscoveryCloudEntry
	cloudRangesExpiry time.Time
)

// getCloudRanges devolve o cache atual, buscando de novo só quando expirado. Nunca falha pro
// chamador — se o fetch der erro (rede fora do ar, feed indisponível), devolve o que tiver em
// cache (mesmo vencido) ou uma lista vazia na 1ª tentativa, nunca propaga erro pra cima (mesmo
// espírito best-effort do resto da Fase 3 — enriquecimento nunca bloqueia o resultado principal).
func getCloudRanges(ctx context.Context) []netDiscoveryCloudEntry {
	cloudRangesMu.RLock()
	if time.Now().Before(cloudRangesExpiry) && cloudRangesCache != nil {
		cached := cloudRangesCache
		cloudRangesMu.RUnlock()
		return cached
	}
	cloudRangesMu.RUnlock()

	entries := fetchCloudRanges(ctx)
	if len(entries) == 0 {
		// Fetch falhou por completo — mantém o cache anterior (mesmo vencido) em vez de zerar,
		// pra não perder o enriquecimento de nuvem só porque um fetch pontual falhou.
		cloudRangesMu.RLock()
		cached := cloudRangesCache
		cloudRangesMu.RUnlock()
		return cached
	}

	cloudRangesMu.Lock()
	cloudRangesCache = entries
	cloudRangesExpiry = time.Now().Add(netDiscoveryCloudRangesTTL)
	cloudRangesMu.Unlock()
	return entries
}

func fetchCloudRanges(ctx context.Context) []netDiscoveryCloudEntry {
	var entries []netDiscoveryCloudEntry
	entries = append(entries, fetchAWSRanges(ctx)...)
	entries = append(entries, fetchGCPRanges(ctx)...)
	entries = append(entries, fetchAzureRanges(ctx)...)
	return entries
}

// awsIPRangesURL — verificado ao vivo (200, JSON bem-formado, ~10600 prefixos IPv4 em
// `prefixes[].ip_prefix`) — URL pública estável, sem autenticação.
const awsIPRangesURL = "https://ip-ranges.amazonaws.com/ip-ranges.json"

type awsIPRangesDoc struct {
	Prefixes []struct {
		IPPrefix string `json:"ip_prefix"`
		Region   string `json:"region"`
	} `json:"prefixes"`
}

func fetchAWSRanges(ctx context.Context) []netDiscoveryCloudEntry {
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, awsIPRangesURL, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var doc awsIPRangesDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil
	}

	entries := make([]netDiscoveryCloudEntry, 0, len(doc.Prefixes))
	for _, p := range doc.Prefixes {
		_, ipnet, err := net.ParseCIDR(p.IPPrefix)
		if err != nil {
			continue
		}
		entries = append(entries, netDiscoveryCloudEntry{Net: ipnet, Provider: "aws", Region: p.Region})
	}
	return entries
}

// gcpIPRangesURL — verificado ao vivo (200, JSON bem-formado, ~1090 prefixos misturando IPv4/
// IPv6 no mesmo array `prefixes[]` — campo é `ipv4Prefix`/`ipv6Prefix`, DIFERENTE do nome usado
// pelo feed da AWS, `ip_prefix` — achado real, confirmado inspecionando os dois JSONs ao vivo
// antes de escrever qualquer parser).
const gcpIPRangesURL = "https://www.gstatic.com/ipranges/cloud.json"

type gcpIPRangesDoc struct {
	Prefixes []struct {
		IPv4Prefix string `json:"ipv4Prefix"`
		Scope      string `json:"scope"` // região (ex: "africa-south1")
	} `json:"prefixes"`
}

func fetchGCPRanges(ctx context.Context) []netDiscoveryCloudEntry {
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, gcpIPRangesURL, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var doc gcpIPRangesDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil
	}

	entries := make([]netDiscoveryCloudEntry, 0, len(doc.Prefixes))
	for _, p := range doc.Prefixes {
		if p.IPv4Prefix == "" {
			continue // entrada IPv6 (campo ipv6Prefix) — fora de escopo por ora
		}
		_, ipnet, err := net.ParseCIDR(p.IPv4Prefix)
		if err != nil {
			continue
		}
		entries = append(entries, netDiscoveryCloudEntry{Net: ipnet, Provider: "gcp", Region: p.Scope})
	}
	return entries
}

// azureListServiceTagsRegion — parâmetro OBRIGATÓRIO da API (`az network list-service-tags -l
// <região>` recusa rodar sem uma região válida), mas puramente COSMÉTICO pro CONTEÚDO da resposta
// — achado real, verificado ao vivo ANTES de escrever este código (mesma disciplina das Fases
// 1-4): comparando a saída de `-l brazilsouth` contra `-l eastus`, as duas chamadas devolveram
// exatamente os mesmos 1556 registros (mesmo conjunto de nomes/prefixos, só o `changeNumber` do
// topo do documento difere entre as duas chamadas — atualização periódica da Azure entre uma
// chamada e outra, não uma diferença de conteúdo por região) — a API sempre devolve o catálogo
// GLOBAL inteiro de service tags, com o campo `region` de CADA entrada individual (não o `-l` da
// chamada) indicando a região real daquele prefixo específico. `brazilsouth` escolhida por ser a
// região real desta empresa (mesma já usada nas outras integrações Azure do projeto — SNAT/FinOps
// pricing), sem nenhum efeito prático sobre o resultado.
const azureListServiceTagsRegion = "brazilsouth"

// azureServiceTagsDoc — schema real confirmado ao vivo (não documentado formalmente pela Azure em
// termos de contrato JSON estável, só inferido do output real). ~1556 entradas, ~3MB — bem maior
// que os feeds AWS/GCP (texto puro, sem paginação).
type azureServiceTagsDoc struct {
	Values []struct {
		Name       string `json:"name"`
		Properties struct {
			// AddressPrefixes mistura IPv4 e IPv6 no MESMO array (diferente de AWS — campos
			// separados — e GCP — nomes de campo separados) — só descoberto inspecionando o JSON
			// real antes de escrever o parser.
			AddressPrefixes []string `json:"addressPrefixes"`
			Region          string   `json:"region"` // pode vir vazio (tags sem região específica)
		} `json:"properties"`
	} `json:"values"`
}

// fetchAzureRanges busca via `az` CLI (não HTTP público como AWS/GCP — a Azure não publica um
// feed JSON estável fora de autenticação) — mesmo padrão de timeout já documentado no CLAUDE.md
// pra qualquer chamada `az` desta app (`exec.CommandContext` com contexto, nunca `exec.Command`
// nu). Falha (CLI ausente, não autenticado, timeout) retorna nil silenciosamente — mesmo
// tratamento best-effort de fetchAWSRanges/fetchGCPRanges, nunca propaga erro.
func fetchAzureRanges(ctx context.Context) []netDiscoveryCloudEntry {
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(fetchCtx, "az", "network", "list-service-tags",
		"-l", azureListServiceTagsRegion, "-o", "json").Output()
	if err != nil {
		return nil
	}

	var doc azureServiceTagsDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil
	}
	return parseAzureServiceTagsDoc(doc)
}

// parseAzureServiceTagsDoc extraído de fetchAzureRanges pra ser testável sem precisar shellar pro
// `az` CLI — recebe o doc já desserializado, devolve as entradas IPv4 (IPv6 pulado, mesmo padrão
// já usado pro feed do GCP — "fora de escopo por ora").
func parseAzureServiceTagsDoc(doc azureServiceTagsDoc) []netDiscoveryCloudEntry {
	entries := make([]netDiscoveryCloudEntry, 0, len(doc.Values)*2)
	for _, v := range doc.Values {
		for _, prefix := range v.Properties.AddressPrefixes {
			if strings.Contains(prefix, ":") {
				continue // IPv6 — fora de escopo por ora, mesmo padrão já usado pro feed do GCP
			}
			_, ipnet, err := net.ParseCIDR(prefix)
			if err != nil {
				continue
			}
			entries = append(entries, netDiscoveryCloudEntry{Net: ipnet, Provider: "azure", Region: v.Properties.Region})
		}
	}
	return entries
}

// matchCloudRange varre linearmente (sem necessidade de estrutura mais sofisticada — ~11 mil
// faixas × até ~30 saltos é sub-milissegundo no total, não vale a complexidade de uma trie aqui).
func matchCloudRange(ip net.IP, ranges []netDiscoveryCloudEntry) (provider, region string) {
	for _, e := range ranges {
		if e.Net.Contains(ip) {
			return e.Provider, e.Region
		}
	}
	return "", ""
}

// reverseIPv4Octets monta o nome usado pelas zonas DNS estilo-RBL da Team Cymru (mesma convenção
// de zonas anti-spam clássicas: octetos invertidos, não na ordem normal).
//
// Achado real, verificado ao vivo ANTES de escrever este código: testar só com 8.8.8.8 (um
// palíndromo de octetos) não provava nada sobre a ordem certa — repetindo o teste com um IP
// assimétrico (142.251.32.14, um IP real do Google): a consulta SEM inverter os octetos
// (`142.251.32.14.origin.asn.cymru.com`) devolveu um ASN completamente errado (4766, Korea
// Telecom — lixo, de uma faixa não relacionada); só a consulta invertida
// (`14.32.251.142.origin.asn.cymru.com`) devolveu o ASN certo (15169, Google). Sem esse 2º teste,
// esta função teria ido pra produção com a ordem errada, sempre devolvendo ASN de outra rede.
func reverseIPv4Octets(ip4 net.IP) string {
	return fmt.Sprintf("%d.%d.%d.%d", ip4[3], ip4[2], ip4[1], ip4[0])
}

// lookupASN consulta a Team Cymru via DNS TXT (net.LookupTXT — sem shell-out, sem HTTP) em duas
// etapas: origin (IP → ASN) e depois asn (ASN → nome da organização). Só IPv4 (a zona `origin`
// tem um equivalente IPv6 — `origin6` — fora de escopo por ora, mesmo escopo IPv4-first do resto
// desta Fase). Retorna ("", "") silenciosamente em qualquer falha — best-effort.
func lookupASN(ctx context.Context, ip4 net.IP) (asn, org string) {
	originName := reverseIPv4Octets(ip4) + ".origin.asn.cymru.com"
	txts, err := net.DefaultResolver.LookupTXT(ctx, originName)
	if err != nil || len(txts) == 0 {
		return "", ""
	}
	fields := strings.Split(txts[0], "|")
	if len(fields) < 1 {
		return "", ""
	}
	asn = strings.TrimSpace(fields[0])
	if asn == "" {
		return "", ""
	}

	asnTxts, err := net.DefaultResolver.LookupTXT(ctx, "AS"+asn+".asn.cymru.com")
	if err != nil || len(asnTxts) == 0 {
		return asn, "" // ASN sozinho já é útil mesmo sem o nome da organização
	}
	asnFields := strings.Split(asnTxts[0], "|")
	// Formato confirmado ao vivo: "asn | country | registry | date | orgname" (5 campos).
	if len(asnFields) >= 5 {
		org = strings.TrimSpace(asnFields[4])
	}
	return asn, org
}

// netDiscoveryEnrichment é o resultado do enriquecimento passivo de UM IP (aplicado por salto).
type netDiscoveryEnrichment struct {
	ReverseDNS  string
	ASN         string
	ASNOrg      string
	CloudMatch  string // "aws" | "gcp" | ""
	CloudRegion string
}

// enrichHopIP roda as três checagens (DNS reverso, ASN, faixa de nuvem) pra um único IP — sempre
// best-effort, nunca retorna erro (mesmo padrão do restante da Fase 3: silêncio numa camada
// nunca esconde as outras que funcionaram).
func enrichHopIP(ctx context.Context, ip string, ranges []netDiscoveryCloudEntry) netDiscoveryEnrichment {
	var enr netDiscoveryEnrichment

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return enr
	}

	if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
		enr.ReverseDNS = strings.TrimSuffix(names[0], ".")
	}

	if ip4 := parsedIP.To4(); ip4 != nil {
		enr.ASN, enr.ASNOrg = lookupASN(ctx, ip4)
	}

	enr.CloudMatch, enr.CloudRegion = matchCloudRange(parsedIP, ranges)

	return enr
}

// netDiscoveryEnrichConcurrency — teto de checagens de enriquecimento em paralelo. Mesmo padrão
// de semáforo já usado noutras varreduras desta app (ex: access_check_scan.go) — protege contra
// disparar dezenas de consultas DNS simultâneas sem necessidade real (o traceroute já limita a
// no máximo netDiscoveryMaxHops=30 saltos).
const netDiscoveryEnrichConcurrency = 8

// enrichHops enriquece TODOS os saltos que responderam (têm IP, não timed_out) EM PARALELO,
// mutando `hops` in-place — seguro porque cada goroutine escreve num ÍNDICE diferente do slice,
// nunca faz append nem toca o mesmo elemento que outra goroutine. Chamado depois do fingerprint
// do destino (Fase 2), antes de montar o NetDiscoveryResult final.
//
// `originalHostname` — string vazia quando o usuário buscou por IP direto (resolveTarget não
// resolveu nada, não existe hostname "real" pra usar). Quando não-vazia (usuário buscou por
// hostname/FQDN), é o valor ORIGINAL digitado — usado só pra sobrescrever o ReverseDNS do salto
// que é o próprio destino (IsTarget), SEM sequer rodar o PTR pra esse salto específico.
//
// Achado real, relatado ao vivo pelo usuário contra um host atrás de um cofre Delinea (bastion/
// PAM): "não retornou o hostname correto" — o PTR (DNS reverso) de um IP atrás de um jump host/
// bastion frequentemente resolve pro nome DNS do PRÓPRIO bastion (o dono real daquele IP do ponto
// de vista do DNS), não pro nome que o usuário efetivamente buscou — isso não é bug de parsing
// nenhum, é a verdade da rede (o PTR aponta pra quem É dono do IP, que pode não ser "o serviço que
// o usuário queria alcançar" quando há um proxy/bastion no meio). Mas pro salto ALVO
// especificamente, já temos uma fonte de verdade estritamente melhor que qualquer PTR: o próprio
// texto que o usuário digitou — se a busca começou por hostname, aquele nome É, por definição, o
// nome correto do destino (é o que resolveu pro IP em primeiro lugar). Usar isso em vez do PTR
// elimina a divergência nesse caso, sem inventar nenhuma heurística nova.
func enrichHops(ctx context.Context, hops []NetDiscoveryHop, originalHostname string) {
	ranges := getCloudRanges(ctx)

	var wg sync.WaitGroup
	sem := make(chan struct{}, netDiscoveryEnrichConcurrency)

	for i := range hops {
		if hops[i].TimedOut || hops[i].IP == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			enrCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			enr := enrichHopIP(enrCtx, hops[idx].IP, ranges)
			hops[idx].ReverseDNS = enr.ReverseDNS
			hops[idx].ASN = enr.ASN
			hops[idx].ASNOrg = enr.ASNOrg
			hops[idx].CloudMatch = enr.CloudMatch
			hops[idx].CloudRegion = enr.CloudRegion

			if hops[idx].IsTarget && originalHostname != "" {
				hops[idx].ReverseDNS = originalHostname
			}
		}(i)
	}

	wg.Wait()
}
