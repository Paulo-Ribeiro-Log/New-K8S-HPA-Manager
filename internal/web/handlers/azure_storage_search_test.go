package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeARM responde as listagens do ARM de forma estável (roteando pela URL).
func fakeARM(t *testing.T, routes map[string]string) func() {
	t.Helper()
	old := azRestGet
	azRestGet = func(_ context.Context, u string) ([]byte, error) {
		// o fragmento mais longo que casa vence (as URLs de página 2 casam com mais de uma rota)
		best, body := "", ""
		for frag, b := range routes {
			if strings.Contains(u, frag) && len(frag) > len(best) {
				best, body = frag, b
			}
		}
		if best == "" {
			return nil, fmt.Errorf("rota inesperada: %s", u)
		}
		if body == "ERR" {
			return nil, fmt.Errorf("AuthorizationFailed em %s", best)
		}
		return []byte(body), nil
	}
	return func() { azRestGet = old }
}

const subA = "aaaaaaaa-0000-0000-0000-000000000001"

func acctJSON(name, rg, kind string) string {
	return fmt.Sprintf(`{"name":%q,"id":"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s","location":"brazilsouth","kind":%q,"sku":{"name":"Standard_LRS"},"properties":{"accessTier":"Hot"}}`, name, subA, rg, name, kind)
}

func TestBuscaNaSubscriptionAchaContaComCicloDeVida(t *testing.T) {
	page1 := `{"value":[` + acctJSON("stoutra", "rg-x", "StorageV2") + `],"nextLink":"https://management.azure.com/subscriptions/` + subA + `/providers/Microsoft.Storage/storageAccounts?api-version=2023-05-01&page=2"}`
	page2 := `{"value":[` + acctJSON("strretirafnc", "rg-retira", "StorageV2") + `]}`
	defer fakeARM(t, map[string]string{
		"storageAccounts?api-version=2023-05-01&page=2": page2,
		"Microsoft.Storage/storageAccounts?api-version": page1,
		"Microsoft.Compute/disks":                       `{"value":[]}`,
		"strretirafnc/managementPolicies/default":       lifecyclePolicyJSON,
		"strretirafnc/blobServices/default/containers":  `{"value":[{"name":"retira"},{"name":"logs"}]}`,
	})()
	res := searchSubscriptions(context.Background(), []SubscriptionInfo{{ID: subA, Name: "DEV/HLG", Clusters: []string{"akspriv-oferta-hlg"}}}, "strretirafnc", false)
	if len(res.Warnings) != 0 || len(res.Results) != 1 {
		t.Fatalf("avisos=%v resultados=%+v", res.Warnings, res.Results)
	}
	h := res.Results[0]
	if h.Type != "storage_account" || h.Name != "strretirafnc" || h.ResourceGroup != "rg-retira" || h.SubscriptionName != "DEV/HLG" || h.AKSManaged {
		t.Errorf("conta: %+v", h)
	}
	if len(h.Clusters) != 1 || h.Clusters[0] != "akspriv-oferta-hlg" {
		t.Errorf("clusters da subscription: %+v", h.Clusters)
	}
	if h.Lifecycle == nil || !h.Lifecycle.HasPolicy || len(h.Lifecycle.Rules) != 2 {
		t.Errorf("ciclo de vida: %+v", h.Lifecycle)
	}
	if strings.Join(h.Containers, ",") != "retira,logs" {
		t.Errorf("containers: %v", h.Containers)
	}
}

func TestBuscaAchaDiscoDoAKSEContaSemPolitica(t *testing.T) {
	disk := fmt.Sprintf(`{"name":"pvc-8c091ac5-4e17","id":"/subscriptions/%s/resourceGroups/MC_rg-x_aks_brazilsouth/providers/Microsoft.Compute/disks/pvc-8c091ac5-4e17","location":"brazilsouth","sku":{"name":"Standard_LRS"},"managedBy":"/subscriptions/%s/resourceGroups/mc/providers/Microsoft.Compute/virtualMachineScaleSets/aks-vmss/virtualMachines/3","properties":{"diskSizeGB":8,"diskState":"Attached"}}`, subA, subA)
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[` + acctJSON("pvc-contas", "rg-a", "StorageV2") + `]}`,
		"Microsoft.Compute/disks":                       `{"value":[` + disk + `]}`,
		"managementPolicies/default":                    `ERR`,
		"blobServices/default/containers":               `{"value":[]}`,
	})()
	// a conta casa com "pvc-" e não tem política (erro de permissão aqui vira o campo Error do resumo)
	res := searchSubscriptions(context.Background(), []SubscriptionInfo{{ID: subA}}, "pvc-", false)
	var gotDisk *StorageHit
	for i := range res.Results {
		if res.Results[i].Type == "disk" {
			gotDisk = &res.Results[i]
		}
	}
	if gotDisk == nil || !gotDisk.AKSManaged || gotDisk.AttachedTo != "3" || gotDisk.SizeGB != "8" || gotDisk.State != "Attached" {
		t.Fatalf("disco: %+v", gotDisk)
	}
	if res.Results[0].Type != "storage_account" || res.Results[0].Lifecycle == nil || res.Results[0].Lifecycle.Error == "" {
		t.Errorf("conta deveria vir primeiro, com o erro do ciclo de vida: %+v", res.Results[0])
	}
}

func TestBuscaProfundaAcheContainerEmOutraConta(t *testing.T) {
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[` + acctJSON("stdados", "rg-d", "StorageV2") + `,` + acctJSON("stfiles", "rg-f", "FileStorage") + `]}`,
		"Microsoft.Compute/disks":                       `{"value":[]}`,
		"stdados/blobServices/default/containers":       `{"value":[{"name":"backup-retira"},{"name":"outro"}]}`,
		"stdados/fileServices/default/shares":           `{"value":[]}`,
		"stfiles/fileServices/default/shares":           `{"value":[{"name":"retira-share","properties":{"accessTier":"Premium"}}]}`,
	})()
	// sem profundo: nenhuma conta tem "retira" no nome -> nada
	if r := searchSubscriptions(context.Background(), []SubscriptionInfo{{ID: subA}}, "retira", false); len(r.Results) != 0 {
		t.Fatalf("sem o modo profundo não deveria achar: %+v", r.Results)
	}
	res := searchSubscriptions(context.Background(), []SubscriptionInfo{{ID: subA}}, "retira", true)
	if len(res.Results) != 2 {
		t.Fatalf("profundo: %+v", res.Results)
	}
	got := map[string]StorageHit{}
	for _, h := range res.Results {
		got[h.Type] = h
	}
	if got["container"].Name != "backup-retira" || got["container"].Account != "stdados" || got["file_share"].Name != "retira-share" || got["file_share"].Account != "stfiles" || got["file_share"].Tier != "Premium" {
		t.Errorf("hits: %+v", got)
	}
}

func TestFalhaEmUmaSubscriptionViraAvisoESegueNasOutras(t *testing.T) {
	subB := "bbbbbbbb-0000-0000-0000-000000000002"
	defer fakeARM(t, map[string]string{
		subB + "/providers/Microsoft.Storage": `ERR`,
		subB + "/providers/Microsoft.Compute": `ERR`,
		subA + "/providers/Microsoft.Storage": `{"value":[` + acctJSON("strretirafnc", "rg", "StorageV2") + `]}`,
		subA + "/providers/Microsoft.Compute": `{"value":[]}`,
		"managementPolicies/default":          lifecyclePolicyJSON,
		"blobServices/default/containers":     `{"value":[]}`,
	})()
	res := searchSubscriptions(context.Background(), []SubscriptionInfo{{ID: subA, Name: "A"}, {ID: subB, Name: "B"}}, "strretirafnc", false)
	if len(res.Results) != 1 || res.Results[0].SubscriptionName != "A" {
		t.Errorf("resultados: %+v", res.Results)
	}
	if len(res.Warnings) != 2 || !strings.Contains(strings.Join(res.Warnings, "|"), "B: contas de armazenamento") {
		t.Errorf("avisos: %v", res.Warnings)
	}
}

func TestArmListIgnoraNextLinkDeOutroHost(t *testing.T) {
	defer fakeARM(t, map[string]string{"x": `{"value":[{"name":"a"}],"nextLink":"https://evil.example.com/steal"}`})()
	items, _, err := armList(context.Background(), "https://management.azure.com/x")
	if err == nil || len(items) != 1 {
		t.Errorf("deveria devolver o que leu e recusar o link: items=%d err=%v", len(items), err)
	}
}

func TestConsultaDeBuscaValidada(t *testing.T) {
	for _, ok := range []string{"strretirafnc", "pvc-8c091ac5-4e17-47fb-991e-e83f0264bfc0", "a.b_c-1"} {
		if !reStorageQuery.MatchString(ok) {
			t.Errorf("%q deveria valer", ok)
		}
	}
	for _, bad := range []string{"ab", "", "a b", "x;rm -rf", "../etc", strings.Repeat("a", 65), "-abc", "a/b"} {
		if reStorageQuery.MatchString(bad) {
			t.Errorf("%q não deveria valer", bad)
		}
	}
}

func TestRGFromID(t *testing.T) {
	if got := rgFromID("/subscriptions/s/resourceGroups/MC_rg_aks/providers/x"); got != "MC_rg_aks" {
		t.Errorf("rg = %q", got)
	}
	if got := rgFromID("/subscriptions/s/resourcegroups/rg2/providers/x"); got != "rg2" {
		t.Errorf("case-insensitive = %q", got)
	}
	if rgFromID("lixo") != "" {
		t.Error("id inválido deveria dar vazio")
	}
}

func TestBuscaContaQuantasContasForamListadas(t *testing.T) {
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[` + acctJSON("stoutra", "rg", "StorageV2") + `,` + acctJSON("stdois", "rg", "StorageV2") + `]}`,
		"Microsoft.Compute/disks":                       `{"value":[{"name":"d1","id":"/subscriptions/x/resourceGroups/r/providers/Microsoft.Compute/disks/d1"}]}`,
	})()
	subs := []SubscriptionInfo{{ID: subA, Name: "A"}}
	res := searchSubscriptions(context.Background(), subs, "naoexiste", false)
	if len(res.Results) != 0 {
		t.Fatalf("não deveria achar: %+v", res.Results)
	}
	if res.Subscriptions[0].AccountsListed != 2 || res.Subscriptions[0].DisksListed != 1 {
		t.Errorf("contagens da listagem: %+v", res.Subscriptions[0])
	}
}

func TestAddVisibleSubscriptions(t *testing.T) {
	subB, subC, subD := "bbbbbbbb-0000-0000-0000-000000000002", "cccccccc-0000-0000-0000-000000000003", "dddddddd-0000-0000-0000-000000000004"
	defer fakeARM(t, map[string]string{"management.azure.com/subscriptions?api-version": fmt.Sprintf(`{"value":[
		{"subscriptionId":%q,"displayName":"A (já conhecida)","state":"Enabled"},
		{"subscriptionId":%q,"displayName":"B sem cluster","state":"Enabled"},
		{"subscriptionId":%q,"displayName":"C desativada","state":"Disabled"},
		{"subscriptionId":%q,"displayName":"D sem cluster","state":"enabled"}]}`, subA, subB, subC, subD)})()
	known := []SubscriptionInfo{{ID: subA, Name: "A", Clusters: []string{"c1"}}}
	out, warns, err := addVisibleSubscriptions(context.Background(), known)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v avisos=%v", err, warns)
	}
	if len(out) != 3 || out[0].ID != subA || out[1].ID != subB || out[2].ID != subD {
		t.Fatalf("subscriptions: %+v", out)
	}
	if len(out[1].Clusters) != 0 || out[1].Name != "B sem cluster" || len(out[0].Clusters) != 1 {
		t.Errorf("as novas ficam sem clusters e as conhecidas mantêm os seus: %+v", out)
	}
	if len(known) != 1 {
		t.Error("não pode alterar a lista original")
	}
}

func TestAddVisibleSubscriptionsFalhaDeListagem(t *testing.T) {
	defer fakeARM(t, map[string]string{"management.azure.com/subscriptions?api-version": "ERR"})()
	if _, _, err := addVisibleSubscriptions(context.Background(), nil); err == nil {
		t.Error("erro do ARM deveria subir")
	}
}

func TestRespostaDaBuscaNuncaTemSlicesNulos(t *testing.T) {
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[]}`,
		"Microsoft.Compute/disks":                       `{"value":[]}`,
	})()
	res := searchSubscriptions(context.Background(), []SubscriptionInfo{{ID: subA, Clusters: []string{}}}, "nada", false)
	// mesma montagem do handler quando all=true e não há avisos
	res.Warnings = append(append([]string{}, []string(nil)...), res.Warnings...)
	b, _ := json.Marshal(res)
	for _, bad := range []string{`"warnings":null`, `"results":null`, `"subscriptions":null`, `"clusters":null`} {
		if strings.Contains(string(b), bad) {
			t.Errorf("JSON com %s: %s", bad, b)
		}
	}
}

func TestSelectSubscriptions(t *testing.T) {
	subB := "bbbbbbbb-0000-0000-0000-000000000002"
	subs := []SubscriptionInfo{{ID: subA, Name: "A"}, {ID: subB, Name: "B"}}
	got, err := selectSubscriptions(subs, strings.ToUpper(subB)) // sem diferenciar caixa
	if err != nil || len(got) != 1 || got[0].Name != "B" {
		t.Fatalf("escolha de uma: %+v err=%v", got, err)
	}
	if got, err = selectSubscriptions(subs, subA+","+subB); err != nil || len(got) != 2 {
		t.Errorf("duas: %+v err=%v", got, err)
	}
	for name, csv := range map[string]string{
		"fora do escopo": "cccccccc-0000-0000-0000-000000000003",
		"não é UUID":     "../etc",
		"vazio":          " , ",
	} {
		if _, err := selectSubscriptions(subs, csv); err == nil {
			t.Errorf("%s deveria ser recusado", name)
		}
	}
}

func TestBuscaProfundaDeUmaSubscriptionNaoRelistaDiscos(t *testing.T) {
	// sem rota de discos: se a busca tentasse listá-los, o fake devolveria "rota inesperada" e viraria aviso
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[` + acctJSON("stdados", "rg-d", "StorageV2") + `]}`,
		"stdados/blobServices/default/containers":       `{"value":[{"name":"backup-retira"}]}`,
		"stdados/fileServices/default/shares":           `{"value":[]}`,
	})()
	subs := []SubscriptionInfo{{ID: subA, Name: "A", DisksListed: 42}}
	res := searchSubscriptionsOpts(context.Background(), subs, "retira", true, searchOpts{SkipDisks: true, DeepCap: storageSearchDeepCapOne})
	if len(res.Warnings) != 0 || len(res.Results) != 1 || res.Results[0].Name != "backup-retira" || res.Results[0].Account != "stdados" {
		t.Fatalf("resultado: %+v avisos=%v", res.Results, res.Warnings)
	}
	if res.Subscriptions[0].DisksListed != 42 {
		t.Errorf("não relistou os discos: o número anterior deve ficar: %+v", res.Subscriptions[0])
	}
}

func TestBuscaProfundaAvisaQuandoLimitaAsContas(t *testing.T) {
	var accts []string
	for i := 0; i < 5; i++ {
		accts = append(accts, acctJSON(fmt.Sprintf("st%02d", i), "rg", "StorageV2"))
	}
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[` + strings.Join(accts, ",") + `]}`,
		"blobServices/default/containers":               `{"value":[]}`,
		"fileServices/default/shares":                   `{"value":[]}`,
	})()
	res := searchSubscriptionsOpts(context.Background(), []SubscriptionInfo{{ID: subA, Name: "PRD"}}, "x", true, searchOpts{SkipDisks: true, DeepCap: 3})
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "PRD") || !strings.Contains(res.Warnings[0], "3 primeiras de 5") {
		t.Errorf("aviso de limite: %v", res.Warnings)
	}
}

func TestLerConteudoDaConta(t *testing.T) {
	defer fakeARM(t, map[string]string{
		"strretirafnc/blobServices/default/containers": `{"value":[
			{"name":"admin-importacao-entrada","properties":{"publicAccess":"None","leaseState":"Available","lastModifiedTime":"2026-11-01T10:00:00Z"}},
			{"name":"retencao","properties":{"publicAccess":"Blob","immutabilityPolicy":{"immutabilityPeriodSinceCreationInDays":30}}}]}`,
		"strretirafnc/fileServices/default/shares": `{"value":[{"name":"compartilhado","properties":{"accessTier":"Hot","enabledProtocols":"SMB","shareQuota":100}}]}`,
	})()
	out := readAccountContents(context.Background(), subA, "rg-retira-fnc-prd", "strretirafnc")
	if len(out.Warnings) != 0 || len(out.Containers) != 2 || len(out.Shares) != 1 {
		t.Fatalf("conteúdo: %+v", out)
	}
	if c := out.Containers[0]; c.Name != "admin-importacao-entrada" || c.PublicAccess != "None" || c.LeaseState != "Available" || c.Immutable {
		t.Errorf("container 1: %+v", c)
	}
	if c := out.Containers[1]; c.PublicAccess != "Blob" || !c.Immutable {
		t.Errorf("container com acesso público e imutabilidade: %+v", c)
	}
	if s := out.Shares[0]; s.Name != "compartilhado" || s.Tier != "Hot" || s.Protocol != "SMB" || s.QuotaGB != "100" {
		t.Errorf("share: %+v", s)
	}
}

func TestLerConteudoDaContaComFalhaParcial(t *testing.T) {
	// conta sem file shares (BlobStorage): um erro de um dos lados vira aviso e o outro lado continua
	defer fakeARM(t, map[string]string{
		"blobServices/default/containers": `{"value":[{"name":"a"}]}`,
		"fileServices/default/shares":     "ERR",
	})()
	out := readAccountContents(context.Background(), subA, "rg", "stfoo")
	if len(out.Containers) != 1 || len(out.Shares) != 0 || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "File shares") {
		t.Errorf("falha parcial: %+v", out)
	}
}

func TestRotaDoConteudoDaContaValidaOsParametros(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer fakeARM(t, map[string]string{})() // qualquer chamada ao ARM vira erro: a validação tem que barrar antes
	h := &AzureStorageSearchHandler{}
	r := gin.New()
	r.GET("/c", h.AccountContents)
	for name, qs := range map[string]string{
		"sem nada":            "",
		"subscription ruim":   "subscription=x&resource_group=rg&account=stfoo",
		"resource group ruim": "subscription=" + subA + "&resource_group=a%2Fb&account=stfoo",
		"conta com maiúscula": "subscription=" + subA + "&resource_group=rg&account=StFoo",
		"conta curta demais":  "subscription=" + subA + "&resource_group=rg&account=ab",
		"injeção na conta":    "subscription=" + subA + "&resource_group=rg&account=st..%2F..%2Fx",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/c?"+qs, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestBuscaProfundaAvisaQuandoOTempoAcaba(t *testing.T) {
	defer fakeARM(t, map[string]string{
		"Microsoft.Storage/storageAccounts?api-version": `{"value":[` + acctJSON("st01", "rg", "StorageV2") + `,` + acctJSON("st02", "rg", "StorageV2") + `]}`,
		"blobServices/default/containers":               `{"value":[]}`,
		"fileServices/default/shares":                   `{"value":[]}`,
	})()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // o prazo já acabou
	res := searchSubscriptionsOpts(ctx, []SubscriptionInfo{{ID: subA, Name: "PRD"}}, "x", true, searchOpts{SkipDisks: true})
	if !strings.Contains(strings.Join(res.Warnings, "|"), "o tempo da varredura de containers acabou") || !strings.Contains(strings.Join(res.Warnings, "|"), "PRD") {
		t.Errorf("deveria avisar que o resultado é parcial: %v", res.Warnings)
	}
}
