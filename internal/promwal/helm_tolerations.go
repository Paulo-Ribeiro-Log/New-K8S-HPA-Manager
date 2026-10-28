package promwal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Correção das tolerations do kube-prometheus-stack pelo Helm: o mesmo
//
//	helm upgrade <release> kube-prometheus-stack --repo <repo> --version <v> -n <ns> --reuse-values -f <override> [--dry-run]
//
// que o SRE roda à mão. O --reuse-values mantém os values atuais e o override só soma as tolerations. A saída do
// helm (manifesto completo, com Secrets) nunca vai para a tela: só os recursos que levam tolerations.

const (
	DefaultHelmChart = "kube-prometheus-stack"
	DefaultHelmRepo  = "https://prometheus-community.github.io/helm-charts"
	maxValuesBytes   = 64 * 1024
)

// DefaultTolerationsYAML é o override padrão: tolera o taint do pool dedicado do Prometheus e o do spot, nos quatro
// componentes do chart que agendam pods.
const DefaultTolerationsYAML = `alertmanager:
  alertmanagerSpec:
    tolerations:
      - effect: NoSchedule
        key: prometheus-only
        operator: Equal
        value: "true"
      - effect: NoSchedule
        key: kubernetes.azure.com/scalesetpriority
        operator: Equal
        value: spot

kube-state-metrics:
  tolerations:
    - effect: NoSchedule
      key: prometheus-only
      operator: Equal
      value: "true"
    - effect: NoSchedule
      key: kubernetes.azure.com/scalesetpriority
      operator: Equal
      value: spot

prometheus:
  prometheusSpec:
    tolerations:
      - effect: NoSchedule
        key: prometheus-only
        operator: Equal
        value: "true"
      - effect: NoSchedule
        key: kubernetes.azure.com/scalesetpriority
        operator: Equal
        value: spot

prometheusOperator:
  tolerations:
    - effect: NoSchedule
      key: prometheus-only
      operator: Equal
      value: "true"
    - effect: NoSchedule
      key: kubernetes.azure.com/scalesetpriority
      operator: Equal
      value: spot
`

// HelmRunner executa o helm com os args (o contexto e o kubeconfig já vão por conta do runner).
type HelmRunner func(ctx context.Context, args ...string) ([]byte, error)

// helmEnv isola o helm da lista de repositórios do usuário. Com --repo <url>, o helm varre TODOS os repositórios de
// repositories.yaml atrás do dono da URL e aborta com "no cached repo found" se um deles (ex.: um repositório
// interno, só alcançável na VPN) não tem o índice em cache. Os comandos daqui usam sempre --repo, então a lista do
// usuário não serve para nada: aponta para um arquivo que não existe (o helm trata como lista vazia).
func helmEnv(dir string) []string {
	return append(os.Environ(), "HELM_REPOSITORY_CONFIG="+filepath.Join(dir, "repositories.yaml"))
}

// HelmCLIRunner é o runner real: `helm` do PATH, no contexto do cluster, sem shell.
func HelmCLIRunner(cluster string) HelmRunner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		dir, err := os.MkdirTemp("", "helm-norepos-")
		if err != nil {
			return nil, fmt.Errorf("criar diretório temporário do helm: %w", err)
		}
		defer os.RemoveAll(dir)
		full := append([]string{"--kube-context", cluster}, args...)
		cmd := exec.CommandContext(ctx, "helm", full...)
		cmd.Env = helmEnv(dir)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("helm %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}
}

type HelmTolerationsRequest struct {
	Cluster    string `json:"cluster"`
	Namespace  string `json:"namespace"`
	Release    string `json:"release"`
	Chart      string `json:"chart"`
	Repo       string `json:"repo"`
	Version    string `json:"version"`
	ValuesYAML string `json:"values_yaml"`
	DryRun     bool   `json:"dry_run"`
}

// HelmWorkload é um recurso do manifesto que leva tolerations, com as que o chart vai aplicar.
type HelmWorkload struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Tolerations  []string `json:"tolerations"`             // "key=value:Effect" (ou "key:Effect" / "Exists")
	NodeSelector []string `json:"node_selector,omitempty"` // "label=valor"
	Resources    []string `json:"resources,omitempty"`     // "container: requests cpu=… mem=… · limits cpu=… mem=…"
}

type HelmTolerationsResult struct {
	DryRun       bool           `json:"dry_run"`
	Release      string         `json:"release"`
	ChartVersion string         `json:"chart_version"`
	Revision     int            `json:"revision"`
	Status       string         `json:"status"`
	Workloads    []HelmWorkload `json:"workloads"`
	Command      string         `json:"command"` // equivalente para copiar e rodar à mão
}

// HelmRelease descreve o release do kube-prometheus-stack encontrado no namespace.
type HelmRelease struct {
	Release      string `json:"release"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chart_version"`
	Status       string `json:"status"`
}

var (
	reHelmName    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	reHelmVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
	reHelmRepo    = regexp.MustCompile(`^https://[A-Za-z0-9][A-Za-z0-9._~:/?#@!$&'()*+,;=%-]{0,255}$`)
)

// Validate confere os campos que viram argumento do helm (sempre sem shell; o 1º caractere alfanumérico impede
// que um valor seja lido como flag) e o YAML do override.
func (r *HelmTolerationsRequest) Validate() error {
	if r.Chart == "" {
		r.Chart = DefaultHelmChart
	}
	if r.Repo == "" {
		r.Repo = DefaultHelmRepo
	}
	if !reHelmName.MatchString(r.Namespace) {
		return fmt.Errorf("namespace inválido")
	}
	if !reHelmName.MatchString(r.Release) {
		return fmt.Errorf("release inválido")
	}
	if !reHelmName.MatchString(r.Chart) {
		return fmt.Errorf("chart inválido")
	}
	if r.Version != "" && !reHelmVersion.MatchString(r.Version) {
		return fmt.Errorf("versão do chart inválida")
	}
	if !reHelmRepo.MatchString(r.Repo) {
		return fmt.Errorf("repositório inválido (use uma URL https)")
	}
	if strings.TrimSpace(r.ValuesYAML) == "" {
		return fmt.Errorf("o YAML de tolerations está vazio")
	}
	if len(r.ValuesYAML) > maxValuesBytes {
		return fmt.Errorf("o YAML passa de %d KiB", maxValuesBytes/1024)
	}
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(r.ValuesYAML), &parsed); err != nil || len(parsed) == 0 {
		return fmt.Errorf("YAML inválido: precisa ser um mapa de values do chart")
	}
	return nil
}

// helmUpgradeArgs monta o comando; valuesFile é o caminho do override gravado.
func helmUpgradeArgs(r HelmTolerationsRequest, valuesFile string, jsonOut bool) []string {
	args := []string{"upgrade", r.Release, r.Chart, "--repo", r.Repo}
	if r.Version != "" {
		args = append(args, "--version", r.Version)
	}
	args = append(args, "-n", r.Namespace, "--reuse-values", "-f", valuesFile)
	if r.DryRun {
		args = append(args, "--dry-run")
	}
	if jsonOut {
		args = append(args, "-o", "json")
	}
	return args
}

// HelmCommandText é o comando equivalente, para exibir/copiar.
func HelmCommandText(r HelmTolerationsRequest) string {
	parts := append([]string{"helm"}, helmUpgradeArgs(r, "tolerations-override.yaml", false)...)
	return strings.Join(parts, " ")
}

// DetectHelmRelease acha o release do kube-prometheus-stack no namespace (para preencher release e versão).
func DetectHelmRelease(ctx context.Context, run HelmRunner, namespace string) (*HelmRelease, error) {
	if !reHelmName.MatchString(namespace) {
		return nil, fmt.Errorf("namespace inválido")
	}
	out, err := run(ctx, "list", "-n", namespace, "--all", "-o", "json")
	if err != nil {
		return nil, err
	}
	var list []struct {
		Name   string `json:"name"`
		Chart  string `json:"chart"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("saída do helm list ilegível: %w", err)
	}
	prefix := DefaultHelmChart + "-"
	for _, e := range list {
		if strings.HasPrefix(e.Chart, prefix) {
			return &HelmRelease{Release: e.Name, Chart: DefaultHelmChart, ChartVersion: strings.TrimPrefix(e.Chart, prefix), Status: e.Status}, nil
		}
	}
	return nil, nil
}

// ApplyHelmTolerations roda o upgrade (com --dry-run se pedido) e devolve o resumo: release, revisão e as
// tolerations que cada workload do manifesto passa a ter.
func ApplyHelmTolerations(ctx context.Context, run HelmRunner, req HelmTolerationsRequest) (*HelmTolerationsResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "tolerations-override-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("criar arquivo temporário do override: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := io.WriteString(f, req.ValuesYAML); err != nil {
		f.Close()
		return nil, fmt.Errorf("gravar o override: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	out, err := run(ctx, helmUpgradeArgs(req, f.Name(), true)...)
	if err != nil {
		return nil, err
	}
	res, err := parseHelmUpgrade(out)
	if err != nil {
		return nil, err
	}
	res.DryRun = req.DryRun
	res.Command = HelmCommandText(req)
	return res, nil
}

// parseHelmUpgrade lê o JSON do `helm upgrade -o json` e descarta tudo menos o resumo seguro.
func parseHelmUpgrade(out []byte) (*HelmTolerationsResult, error) {
	var rel struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
		Info    struct {
			Status string `json:"status"`
		} `json:"info"`
		Chart struct {
			Metadata struct {
				Version string `json:"version"`
			} `json:"metadata"`
		} `json:"chart"`
		Manifest string `json:"manifest"`
	}
	if err := json.Unmarshal(out, &rel); err != nil {
		return nil, fmt.Errorf("saída do helm upgrade ilegível: %w", err)
	}
	return &HelmTolerationsResult{
		Release:      rel.Name,
		ChartVersion: rel.Chart.Metadata.Version,
		Revision:     rel.Version,
		Status:       rel.Info.Status,
		Workloads:    workloadsWithTolerations(rel.Manifest),
	}, nil
}

type tolerationDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Tolerations  []tolerationYAML  `yaml:"tolerations"`
		NodeSelector map[string]string `yaml:"nodeSelector"`
		Resources    resourcesYAML     `yaml:"resources"` // CRs Prometheus/Alertmanager
		Template     struct {
			Spec struct {
				Tolerations  []tolerationYAML  `yaml:"tolerations"`
				NodeSelector map[string]string `yaml:"nodeSelector"`
				Containers   []struct {
					Name      string        `yaml:"name"`
					Resources resourcesYAML `yaml:"resources"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type resourcesYAML struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}

// text: "requests cpu=100m mem=256Mi · limits cpu=500m mem=1Gi" ("" se não define nada).
func (r resourcesYAML) text() string {
	part := func(name string, m map[string]string) string {
		if len(m) == 0 {
			return ""
		}
		s := name
		for _, k := range []string{"cpu", "memory"} {
			if v, ok := m[k]; ok {
				s += " " + strings.Replace(k, "memory", "mem", 1) + "=" + v
			}
		}
		return s
	}
	var out []string
	for _, p := range []string{part("requests", r.Requests), part("limits", r.Limits)} {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

type tolerationYAML struct {
	Key      string `yaml:"key"`
	Operator string `yaml:"operator"`
	Value    string `yaml:"value"`
	Effect   string `yaml:"effect"`
}

func (t tolerationYAML) String() string {
	s := t.Key
	if t.Operator != "Exists" && t.Value != "" {
		s += "=" + t.Value
	}
	if s == "" {
		s = "Exists"
	}
	if t.Effect != "" {
		s += ":" + t.Effect
	}
	return s
}

// workloadsWithTolerations percorre o manifesto (vários documentos YAML) e devolve só os recursos com
// tolerations: Deployment/StatefulSet/DaemonSet (spec.template.spec) e os CRs Prometheus/Alertmanager (spec).
// Os demais documentos (Secrets, ConfigMaps…) nem chegam a ser guardados.
func workloadsWithTolerations(manifest string) []HelmWorkload {
	var res []HelmWorkload
	dec := yaml.NewDecoder(strings.NewReader(manifest))
	for {
		var d tolerationDoc
		if err := dec.Decode(&d); err != nil {
			if err == io.EOF {
				break
			}
			break // manifesto com documento ilegível: devolve o que já leu
		}
		tols, sel := d.Spec.Template.Spec.Tolerations, d.Spec.Template.Spec.NodeSelector
		if len(tols) == 0 {
			tols = d.Spec.Tolerations
		}
		if len(sel) == 0 {
			sel = d.Spec.NodeSelector
		}
		var resTxt []string
		if t := d.Spec.Resources.text(); t != "" {
			resTxt = append(resTxt, t)
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			if t := c.Resources.text(); t != "" {
				resTxt = append(resTxt, c.Name+": "+t)
			}
		}
		if (len(tols) == 0 && len(sel) == 0 && len(resTxt) == 0) || d.Kind == "" {
			continue
		}
		w := HelmWorkload{Kind: d.Kind, Name: d.Metadata.Name, Resources: resTxt}
		for _, t := range tols {
			w.Tolerations = append(w.Tolerations, t.String())
		}
		for _, k := range sortedKeys(sel) {
			w.NodeSelector = append(w.NodeSelector, k+"="+sel[k])
		}
		res = append(res, w)
	}
	return res
}

// HelmRevision é uma revisão do release (cada `helm upgrade` aplicado cria uma).
type HelmRevision struct {
	Revision    int    `json:"revision"`
	Updated     string `json:"updated"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	AppVersion  string `json:"app_version"`
	Description string `json:"description"`
}

const helmHistoryMax = 25

// HelmHistory lista as últimas revisões do release (a mais recente por último, como o helm devolve).
func HelmHistory(ctx context.Context, run HelmRunner, namespace, release string) ([]HelmRevision, error) {
	if !reHelmName.MatchString(namespace) || !reHelmName.MatchString(release) {
		return nil, fmt.Errorf("namespace ou release inválido")
	}
	out, err := run(ctx, "history", release, "-n", namespace, "--max", fmt.Sprint(helmHistoryMax), "-o", "json")
	if err != nil {
		return nil, err
	}
	var revs []HelmRevision
	if err := json.Unmarshal(out, &revs); err != nil {
		return nil, fmt.Errorf("saída do helm history ilegível: %w", err)
	}
	return revs, nil
}

// HelmRollback volta o release para uma revisão anterior (`helm rollback`), o que cria uma revisão nova com os
// values e o manifesto daquela. dryRun só simula.
func HelmRollback(ctx context.Context, run HelmRunner, namespace, release string, revision int, dryRun bool) error {
	if !reHelmName.MatchString(namespace) || !reHelmName.MatchString(release) {
		return fmt.Errorf("namespace ou release inválido")
	}
	if revision < 1 {
		return fmt.Errorf("revisão inválida")
	}
	args := []string{"rollback", release, fmt.Sprint(revision), "-n", namespace}
	if dryRun {
		args = append(args, "--dry-run")
	}
	_, err := run(ctx, args...)
	return err
}
