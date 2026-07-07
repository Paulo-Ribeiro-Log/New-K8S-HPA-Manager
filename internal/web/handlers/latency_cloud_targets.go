package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CloudRegionTarget é um alvo de teste de latência pra uma região de nuvem específica — usado no
// seletor "Alvo rápido: região de nuvem" do frontend (Fase 6.2), inspirado no `gcping.com` mas
// com endpoints que já são nossos ou oficialmente documentados, não os serviços de demonstração
// do gcping (que são infraestrutura de terceiros — não deveríamos gerar tráfego de teste
// repetido lá).
type CloudRegionTarget struct {
	Provider string `json:"provider"` // "aws" | "gcp" | "azure"
	Region   string `json:"region"`
	Label    string `json:"label"` // nome amigável pro seletor
	Host     string `json:"host"`
	Protocol string `json:"protocol"` // protocolo sugerido pra esse alvo
}

// cloudRegionTargets é uma lista curada e pequena — não uma cobertura mundial exaustiva de
// manter. Regiões escolhidas por relevância pra uma organização brasileira (sa-east-1 = São
// Paulo) + as mais comuns em arquiteturas multi-região.
//
// AWS: `s3.<região>.amazonaws.com` é convenção OFICIAL e documentada da AWS (não um palpite) —
// todo endpoint regional de S3 responde nesse padrão via TCP/TLS mesmo sem bucket ou credencial
// (a requisição HTTP pode retornar 403/400, mas isso já é suficiente pra medir round-trip real).
//
// GCP e Azure: DELIBERADAMENTE VAZIOS aqui — ver LATENCY-METRICS-PLAN.md Fase 6.2 pra decisão
// completa. Resumo: `gcping.com` usa serviços de demonstração próprios do autor (Cloud
// Run/App Engine deployados por ele em cada região), não endpoints públicos genéricos do GCP —
// gerar tráfego de teste repetido contra infraestrutura de terceiros sem necessidade não é
// apropriado. Não existe um equivalente documentado ao "S3 por região" pra GCP nem Azure que não
// dependa de um recurso específico já provisionado (bucket GCS ou storage account Azure de
// alguém). Preencher essas duas listas exige uma decisão consciente: provisionar recursos
// "canário" próprios por região, ou usar um recurso de teste já existente da organização.
var cloudRegionTargets = []CloudRegionTarget{
	{Provider: "aws", Region: "sa-east-1", Label: "AWS sa-east-1 (São Paulo)", Host: "s3.sa-east-1.amazonaws.com", Protocol: "https"},
	{Provider: "aws", Region: "us-east-1", Label: "AWS us-east-1 (N. Virginia)", Host: "s3.us-east-1.amazonaws.com", Protocol: "https"},
	{Provider: "aws", Region: "us-west-2", Label: "AWS us-west-2 (Oregon)", Host: "s3.us-west-2.amazonaws.com", Protocol: "https"},
	{Provider: "aws", Region: "eu-west-1", Label: "AWS eu-west-1 (Irlanda)", Host: "s3.eu-west-1.amazonaws.com", Protocol: "https"},
	{Provider: "aws", Region: "ap-southeast-1", Label: "AWS ap-southeast-1 (Singapura)", Host: "s3.ap-southeast-1.amazonaws.com", Protocol: "https"},
}

// GetCloudTargets retorna a lista curada de alvos de nuvem pro seletor "Alvo rápido" do
// frontend. Estático — não depende de cluster/credenciais, por isso sem RequireSREGroup (mesmo
// critério de outros endpoints de config somente-leitura).
// GET /api/v1/latency-test/cloud-targets
func (h *LatencyTestHandler) GetCloudTargets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"targets": cloudRegionTargets})
}
