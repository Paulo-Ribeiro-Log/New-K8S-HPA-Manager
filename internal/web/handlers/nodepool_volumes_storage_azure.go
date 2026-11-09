package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Detalhes do storage consultados na Azure (Resource Manager) — só quando o usuário clica em
// "Consultar na Azure". Somente GET, pelo `az rest` (a mesma credencial do `az login` do servidor),
// com o alvo derivado do PV lido no servidor: nada vindo da tela vira parte da URL.

const (
	armBase       = "https://management.azure.com"
	storageAPIVer = "2023-05-01"
	diskAPIVer    = "2023-10-02"
)

var (
	reAccountName = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	reShareName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	reRGName      = regexp.MustCompile(`^[-\w._()]{1,90}$`)
	reUUID        = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reDiskID      = regexp.MustCompile(`(?i)^/subscriptions/([0-9a-f-]{36})/resourceGroups/([-\w._()]{1,90})/providers/Microsoft\.Compute/disks/([-\w._]{1,80})$`)
)

type AzField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Warn  bool   `json:"warn,omitempty"`
}

type AzSection struct {
	Title  string    `json:"title"`
	Fields []AzField `json:"fields"`
}

type AzStorageDetails struct {
	Sections     []AzSection `json:"sections"`
	Notes        []string    `json:"notes"`    // pontos de atenção deduzidos dos dados
	Warnings     []string    `json:"warnings"` // partes que não foram lidas (permissão, recurso em outra subscription…)
	Subscription string      `json:"subscription,omitempty"`
	QueriedAt    string      `json:"queried_at"`
}

// azRestGet roda um GET no ARM pelo az CLI (substituível nos testes).
var azRestGet = func(ctx context.Context, rawURL string) ([]byte, error) {
	if _, err := exec.LookPath("az"); err != nil {
		return nil, fmt.Errorf("az CLI não encontrado no servidor")
	}
	out, err := exec.CommandContext(ctx, "az", "rest", "--method", "get", "--url", rawURL, "--only-show-errors", "-o", "json").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// azSubscriptionID aceita UUID ou nome da subscription (clusters-config.json guarda os dois).
var azSubscriptionID = func(ctx context.Context, sub string) (string, error) {
	if reUUID.MatchString(sub) {
		return sub, nil
	}
	out, err := exec.CommandContext(ctx, "az", "account", "show", "--subscription", sub, "--query", "id", "-o", "tsv", "--only-show-errors").Output()
	if err != nil {
		return "", fmt.Errorf("não consegui resolver a subscription %q: %w", sub, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// dig navega num JSON decodificado: dig(m, "properties", "sku", "name").
func dig(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

func digStr(m any, path ...string) string {
	switch v := dig(m, path...).(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func digLen(m any, path ...string) int {
	if a, ok := dig(m, path...).([]any); ok {
		return len(a)
	}
	return 0
}

func yesNo(s string) string {
	switch s {
	case "true":
		return "sim"
	case "false":
		return "não"
	}
	return s
}

func humanBytes(s string) string {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%.1f GiB", n/(1<<30))
}

// section monta uma seção só com os campos que vieram preenchidos.
func section(title string, fields ...AzField) AzSection {
	out := AzSection{Title: title}
	for _, f := range fields {
		if f.Value != "" {
			out.Fields = append(out.Fields, f)
		}
	}
	return out
}

func decode(raw []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// isGeoSKU: SKUs com região secundária (única situação em que geoReplicationStats existe).
func isGeoSKU(sku string) bool {
	_, r := splitSKU(sku)
	return r == "GRS" || r == "GZRS" || r == "RAGRS" || r == "RAGZRS"
}

func accountSections(acc map[string]any, d *AzStorageDetails) {
	sku, kind, tier := digStr(acc, "sku", "name"), digStr(acc, "kind"), digStr(acc, "properties", "accessTier")
	d.Sections = append(d.Sections, section("Conta de armazenamento",
		AzField{Label: "Kind", Value: kind},
		AzField{Label: "SKU / redundância", Value: sku},
		AzField{Label: "Tier de acesso", Value: tier},
		AzField{Label: "Região", Value: digStr(acc, "location")},
		AzField{Label: "Região secundária", Value: digStr(acc, "properties", "secondaryLocation")},
		AzField{Label: "Status primário", Value: digStr(acc, "properties", "statusOfPrimary")},
		AzField{Label: "Status secundário", Value: digStr(acc, "properties", "statusOfSecondary")},
		AzField{Label: "Última sincronização geo", Value: digStr(acc, "properties", "geoReplicationStats", "lastSyncTime")},
		AzField{Label: "Criada em", Value: digStr(acc, "properties", "creationTime")},
		AzField{Label: "Estado", Value: digStr(acc, "properties", "provisioningState")},
	))
	tls := digStr(acc, "properties", "minimumTlsVersion")
	pub := digStr(acc, "properties", "publicNetworkAccess")
	action := digStr(acc, "properties", "networkAcls", "defaultAction")
	blobPublic := digStr(acc, "properties", "allowBlobPublicAccess")
	sharedKey := digStr(acc, "properties", "allowSharedKeyAccess")
	d.Sections = append(d.Sections, section("Segurança e rede",
		AzField{Label: "TLS mínimo", Value: tls, Warn: tls != "" && tls != "TLS1_2" && tls != "TLS1_3"},
		AzField{Label: "Só HTTPS", Value: yesNo(digStr(acc, "properties", "supportsHttpsTrafficOnly"))},
		AzField{Label: "Acesso público a blobs", Value: yesNo(blobPublic), Warn: blobPublic == "true"},
		AzField{Label: "Chave compartilhada (Shared Key)", Value: yesNo(sharedKey)},
		AzField{Label: "Acesso de rede pública", Value: pub},
		AzField{Label: "Firewall (ação padrão)", Value: action, Warn: action == "Allow" && pub != "Disabled"},
		AzField{Label: "Regras de IP / VNet", Value: fmt.Sprintf("%d / %d", digLen(acc, "properties", "networkAcls", "ipRules"), digLen(acc, "properties", "networkAcls", "virtualNetworkRules"))},
		AzField{Label: "Private endpoints", Value: strconv.Itoa(digLen(acc, "properties", "privateEndpointConnections"))},
		AzField{Label: "Chave de criptografia", Value: digStr(acc, "properties", "encryption", "keySource")},
		AzField{Label: "Criptografia de infraestrutura", Value: yesNo(digStr(acc, "properties", "encryption", "requireInfrastructureEncryption"))},
	))
	d.Sections = append(d.Sections, section("Recursos habilitados",
		AzField{Label: "Namespace hierárquico (HNS)", Value: yesNo(digStr(acc, "properties", "isHnsEnabled"))},
		AzField{Label: "NFS v3", Value: yesNo(digStr(acc, "properties", "isNfsV3Enabled"))},
		AzField{Label: "SFTP", Value: yesNo(digStr(acc, "properties", "isSftpEnabled"))},
		AzField{Label: "Large file shares", Value: digStr(acc, "properties", "largeFileSharesState")},
	))
	if blobPublic == "true" {
		d.Notes = append(d.Notes, "A conta permite acesso público a blobs.")
	}
	if tls != "" && tls != "TLS1_2" && tls != "TLS1_3" {
		d.Notes = append(d.Notes, "TLS mínimo abaixo de 1.2 ("+tls+").")
	}
	if action == "Allow" && pub != "Disabled" {
		d.Notes = append(d.Notes, "Sem firewall: a conta aceita conexões de qualquer rede.")
	}
	if s := digStr(acc, "properties", "statusOfSecondary"); s != "" && s != "available" {
		d.Notes = append(d.Notes, "A região secundária está "+s+".")
	}
	if t, err := time.Parse(time.RFC3339, digStr(acc, "properties", "geoReplicationStats", "lastSyncTime")); err == nil && time.Since(t) > 24*time.Hour {
		d.Notes = append(d.Notes, "A replicação geo está atrasada em mais de 24 h.")
	}
}

func shareSection(sh map[string]any, d *AzStorageDetails) {
	d.Sections = append(d.Sections, section("File share",
		AzField{Label: "Tier", Value: digStr(sh, "properties", "accessTier")},
		AzField{Label: "Protocolo", Value: digStr(sh, "properties", "enabledProtocols")},
		AzField{Label: "Cota", Value: strings.TrimSpace(digStr(sh, "properties", "shareQuota") + " GiB")},
		AzField{Label: "Usado", Value: humanBytes(digStr(sh, "properties", "shareUsageBytes"))},
		AzField{Label: "Root squash (NFS)", Value: digStr(sh, "properties", "rootSquash")},
		AzField{Label: "Modificado em", Value: digStr(sh, "properties", "lastModifiedTime")},
	))
	if q, u := digStr(sh, "properties", "shareQuota"), digStr(sh, "properties", "shareUsageBytes"); q != "" && u != "" {
		qq, _ := strconv.ParseFloat(q, 64)
		uu, _ := strconv.ParseFloat(u, 64)
		if qq > 0 && uu/(qq*(1<<30)) >= 0.9 {
			d.Notes = append(d.Notes, "O file share está com mais de 90% da cota usada.")
		}
	}
}

func containerSection(ct map[string]any, d *AzStorageDetails) {
	pa := digStr(ct, "properties", "publicAccess")
	d.Sections = append(d.Sections, section("Container",
		AzField{Label: "Acesso público", Value: pa, Warn: pa != "" && pa != "None"},
		AzField{Label: "Imutabilidade (WORM)", Value: yesNo(digStr(ct, "properties", "hasImmutabilityPolicy"))},
		AzField{Label: "Legal hold", Value: yesNo(digStr(ct, "properties", "hasLegalHold"))},
		AzField{Label: "Modificado em", Value: digStr(ct, "properties", "lastModifiedTime")},
	))
	if pa != "" && pa != "None" {
		d.Notes = append(d.Notes, "O container tem acesso público ("+pa+").")
	}
}

func protectionSection(title string, svc map[string]any, retentionPath []string, versioning bool, d *AzStorageDetails) {
	del := dig(svc, retentionPath...)
	enabled := digStr(del, "enabled")
	days := digStr(del, "days")
	val := "desligado"
	if enabled == "true" {
		val = "ligado (" + days + " dias)"
	}
	fields := []AzField{{Label: "Soft delete", Value: val, Warn: enabled != "true"}}
	if versioning {
		fields = append(fields, AzField{Label: "Versionamento", Value: yesNo(digStr(svc, "properties", "isVersioningEnabled"))})
	}
	d.Sections = append(d.Sections, section(title, fields...))
	if enabled != "true" {
		d.Notes = append(d.Notes, title+": soft delete desligado — apagar é definitivo.")
	}
}

func diskSections(dk map[string]any, d *AzStorageDetails) {
	d.Sections = append(d.Sections, section("Managed Disk",
		AzField{Label: "SKU", Value: digStr(dk, "sku", "name")},
		AzField{Label: "Tier de desempenho", Value: digStr(dk, "properties", "tier")},
		AzField{Label: "Tamanho", Value: strings.TrimSpace(digStr(dk, "properties", "diskSizeGB") + " GiB")},
		AzField{Label: "IOPS", Value: digStr(dk, "properties", "diskIOPSReadWrite")},
		AzField{Label: "Throughput (MB/s)", Value: digStr(dk, "properties", "diskMBpsReadWrite")},
		AzField{Label: "Burst", Value: yesNo(digStr(dk, "properties", "burstingEnabled"))},
		AzField{Label: "Estado", Value: digStr(dk, "properties", "diskState")},
		AzField{Label: "Anexado a", Value: lastSegment(digStr(dk, "managedBy"))},
		AzField{Label: "Região / zona", Value: strings.TrimSpace(digStr(dk, "location") + " " + zonesOf(dk))},
		AzField{Label: "Criptografia", Value: digStr(dk, "properties", "encryption", "type")},
		AzField{Label: "Acesso de rede", Value: digStr(dk, "properties", "networkAccessPolicy")},
		AzField{Label: "Criado em", Value: digStr(dk, "properties", "timeCreated")},
	))
	if digStr(dk, "properties", "diskState") == "Unattached" {
		d.Notes = append(d.Notes, "O disco está Unattached na Azure.")
	}
	if n := digStr(dk, "properties", "networkAccessPolicy"); n == "AllowAll" {
		d.Notes = append(d.Notes, "O disco permite exportação por qualquer rede (networkAccessPolicy AllowAll).")
	}
}

func zonesOf(m map[string]any) string {
	var z []string
	if a, ok := m["zones"].([]any); ok {
		for _, v := range a {
			if s, ok := v.(string); ok {
				z = append(z, s)
			}
		}
	}
	if len(z) == 0 {
		return ""
	}
	sort.Strings(z)
	return "(zona " + strings.Join(z, ",") + ")"
}

func lastSegment(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// azTarget é o que será consultado, já validado.
type azTarget struct {
	kind                       string // "storage" | "disk"
	driver, sub                string
	rg, account, share, diskID string
}

// azureTargetFor deriva e valida o alvo a partir do StorageInfo do PV (nunca de dados da tela).
func azureTargetFor(info *StorageInfo, clusterSub string) (azTarget, error) {
	if info == nil || !strings.Contains(info.Driver, "azure") {
		return azTarget{}, fmt.Errorf("este volume não é de storage Azure")
	}
	t := azTarget{driver: info.Driver, sub: clusterSub}
	if info.Driver == "disk.csi.azure.com" || info.ResourceID != "" {
		m := reDiskID.FindStringSubmatch(info.ResourceID)
		if m == nil {
			return azTarget{}, fmt.Errorf("não consegui identificar o ID do disco Azure deste PV")
		}
		t.kind, t.sub, t.diskID = "disk", m[1], info.ResourceID
		return t, nil
	}
	if !reAccountName.MatchString(info.Account) {
		return azTarget{}, fmt.Errorf("o PV não informa a storage account (volume provisionado dinamicamente numa conta gerenciada pelo AKS): sem o nome da conta não há o que consultar")
	}
	if info.ResourceGroup != "" && !reRGName.MatchString(info.ResourceGroup) {
		return azTarget{}, fmt.Errorf("resource group inválido no PV")
	}
	if info.Share != "" && !reShareName.MatchString(info.Share) {
		return azTarget{}, fmt.Errorf("nome de share/container inválido no PV")
	}
	t.kind, t.account, t.rg, t.share = "storage", info.Account, info.ResourceGroup, info.Share
	return t, nil
}

// diskNotFound reconhece a resposta "ResourceNotFound" do ARM para o disco.
func diskNotFound(err error) bool {
	m := err.Error()
	return strings.Contains(m, "ResourceNotFound") || strings.Contains(m, "was not found")
}

// collectAzureDetails faz as leituras (em paralelo) e monta o resultado. Falha parcial vira aviso.
func collectAzureDetails(ctx context.Context, t azTarget) *AzStorageDetails {
	d := &AzStorageDetails{Subscription: t.sub, QueriedAt: time.Now().UTC().Format(time.RFC3339), Sections: []AzSection{}, Notes: []string{}, Warnings: []string{}}
	get := func(rawURL string) (map[string]any, error) {
		raw, err := azRestGet(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		if m := decode(raw); m != nil {
			return m, nil
		}
		return nil, fmt.Errorf("resposta inesperada da Azure")
	}
	if t.kind == "disk" {
		dk, err := get(armBase + t.diskID + "?api-version=" + diskAPIVer)
		if err != nil {
			if diskNotFound(err) {
				d.Warnings = append(d.Warnings, "O disco não existe mais na Azure: o PV ainda aponta para ele, mas o recurso foi apagado (ou movido). "+
					"Um pod que tentar usar este volume não consegue montá-lo. Se o volume está ocioso, é uma referência órfã: o PVC/PV podem ser removidos. Detalhe da Azure: "+err.Error())
			} else {
				d.Warnings = append(d.Warnings, "Disco: "+err.Error())
			}
			return d
		}
		diskSections(dk, d)
		lifecycleNotApplicable("disco gerenciado", d)
		return d
	}

	base := fmt.Sprintf("%s/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s", armBase, url.PathEscape(t.sub), url.PathEscape(t.rg), url.PathEscape(t.account))
	if t.rg == "" { // sem resource group no PV: acha a conta pelo nome na subscription do cluster
		raw, err := azRestGet(ctx, fmt.Sprintf("%s/subscriptions/%s/providers/Microsoft.Storage/storageAccounts?api-version=%s", armBase, url.PathEscape(t.sub), storageAPIVer))
		if err != nil {
			d.Warnings = append(d.Warnings, "Conta: "+err.Error())
			return d
		}
		if l, ok := decode(raw)["value"].([]any); ok {
			for _, it := range l {
				if strings.EqualFold(digStr(it, "name"), t.account) {
					// id: /subscriptions/<sub>/resourceGroups/<RG>/providers/...
					if parts := strings.Split(digStr(it, "id"), "/"); len(parts) > 4 && reRGName.MatchString(parts[4]) {
						t.rg = parts[4]
					}
				}
			}
		}
		if t.rg == "" {
			d.Warnings = append(d.Warnings, fmt.Sprintf("A conta %s não está na subscription do cluster (%s); ela pode estar em outra.", t.account, t.sub))
			return d
		}
		base = fmt.Sprintf("%s/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s", armBase, url.PathEscape(t.sub), url.PathEscape(t.rg), url.PathEscape(t.account))
	}

	var wg sync.WaitGroup
	var acc, svc, item, policy map[string]any
	var accErr, svcErr, itemErr, policyErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		// $expand=geoReplicationStats só vale em conta geo-replicada: em LRS/ZRS a Azure responde
		// InvalidAccountType. Lê a conta sem o expand e pede as estatísticas só se o SKU for geo.
		if acc, accErr = get(base + "?api-version=" + storageAPIVer); accErr == nil && isGeoSKU(digStr(acc, "sku", "name")) {
			if withStats, err := get(base + "?api-version=" + storageAPIVer + "&$expand=geoReplicationStats"); err == nil {
				acc = withStats
			}
		}
	}()
	svcName, itemKind := "blobServices", "containers"
	if t.driver == "file.csi.azure.com" {
		svcName, itemKind = "fileServices", "shares"
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc, svcErr = get(base + "/" + svcName + "/default?api-version=" + storageAPIVer)
	}()
	if itemKind == "containers" { // ciclo de vida só existe para blobs
		wg.Add(1)
		go func() {
			defer wg.Done()
			policy, policyErr = get(base + "/managementPolicies/default?api-version=" + storageAPIVer)
		}()
	}
	if t.share != "" {
		wg.Add(1)
		extra := ""
		if itemKind == "shares" {
			extra = "&$expand=stats"
		}
		go func() {
			defer wg.Done()
			item, itemErr = get(base + "/" + svcName + "/default/" + itemKind + "/" + url.PathEscape(t.share) + "?api-version=" + storageAPIVer + extra)
		}()
	}
	wg.Wait()

	if accErr != nil {
		d.Warnings = append(d.Warnings, "Conta: "+accErr.Error())
		return d
	}
	accountSections(acc, d)
	switch {
	case svcErr != nil:
		d.Warnings = append(d.Warnings, "Proteção de dados: "+svcErr.Error())
	case itemKind == "shares":
		protectionSection("Proteção dos file shares", svc, []string{"properties", "shareDeleteRetentionPolicy"}, false, d)
	default:
		protectionSection("Proteção dos blobs", svc, []string{"properties", "deleteRetentionPolicy"}, true, d)
	}
	if itemKind == "containers" {
		lifecycleSection(policy, policyErr, d)
	} else {
		lifecycleNotApplicable("Azure Files", d)
	}
	switch {
	case t.share == "":
	case itemErr != nil:
		d.Warnings = append(d.Warnings, "Share/container: "+itemErr.Error())
	case itemKind == "shares":
		shareSection(item, d)
	default:
		containerSection(item, d)
	}
	return d
}

// GetVolumeAzureDetails consulta a Azure sobre o storage de UM volume (a pedido do usuário).
func (h *NodePoolHandler) GetVolumeAzureDetails(c *gin.Context) {
	cluster, ns, pvcName, pvName := c.Query("cluster"), c.Query("namespace"), c.Query("pvc"), c.Query("pv")
	if cluster == "" || (pvcName == "" && pvName == "") {
		volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("cluster e pvc (ou pv) são obrigatórios"))
		return
	}
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "KUBERNETES_CLIENT_ERROR", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()

	if pvName == "" {
		pvc, err := client.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
			return
		}
		pvName = pvc.Spec.VolumeName
	}
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
		return
	}
	var params map[string]string
	if sc, err := client.StorageV1().StorageClasses().Get(ctx, pv.Spec.StorageClassName, metav1.GetOptions{}); err == nil {
		params = sc.Parameters
	}
	info := storageInfoFor(*pv, params)

	sub := ""
	if cfg := h.kubeManager.GetClusterConfig(cluster); cfg != nil {
		if sub = cfg.SubscriptionID; sub == "" {
			sub = cfg.Subscription
		}
	}
	target, err := azureTargetFor(info, sub)
	if err != nil {
		volumeActionFail(c, http.StatusUnprocessableEntity, "AZURE_UNAVAILABLE", err)
		return
	}
	if target.kind == "storage" {
		if target.sub == "" {
			volumeActionFail(c, http.StatusUnprocessableEntity, "AZURE_UNAVAILABLE", fmt.Errorf("o cluster não tem subscription em clusters-config.json — rode o autodiscover"))
			return
		}
		if target.sub, err = azSubscriptionID(ctx, target.sub); err != nil {
			volumeActionFail(c, http.StatusBadGateway, "AZURE_ERROR", err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": collectAzureDetails(ctx, target)})
}
