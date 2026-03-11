import { useState, useMemo, useCallback } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { CertificateDetailModal } from "@/components/CertificateDetailModal";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ProtectedAction } from "@/components/rbac";
import {
  Search,
  RefreshCcw,
  Shield,
  ShieldCheck,
  Copy,
  Upload,
  FileText,
  Loader2,
  Download,
  ArrowUpDown,
  X,
  FileDown,
  ChevronDown,
  ChevronRight,
  Server,
  Table,
} from "lucide-react";
import { toast } from "sonner";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { addLogoHeaderToPDF, getMarkdownHeader, addFooterToPDF } from "@/lib/logoUtils";

import { useClusters } from "@/hooks/useAPI";
import { useCertificates } from "@/hooks/useCertificates";
import type { Cluster } from "@/lib/api/types";
import type {
  CertificateInfo,
  ScanRequest,
  CopyRequest,
  UploadRequest,
} from "@/types/certificates";

interface CertificatesTabProps {
  selectedCluster?: string;
}

type SortField = "status" | "secretName" | "namespace" | "cluster" | "subject" | "daysRemaining";
type SortDirection = "asc" | "desc";

// Remover emojis para compatibilidade jsPDF
const removeEmojis = (text: string): string => {
  return text
    .replace(/[\u{1F300}-\u{1F9FF}]/gu, "")
    .replace(/[\u{2600}-\u{26FF}]/gu, "")
    .replace(/[\u{2700}-\u{27BF}]/gu, "")
    .replace(/\uFE0F/g, "")
    .trim();
};

export default function CertificatesTab({ selectedCluster }: CertificatesTabProps) {
  const { clusters: allClusters } = useClusters();
  const {
    scanResult,
    scanning,
    scanError,
    scanCertificates,
    copyCertificate,
    uploadCertificate,
  } = useCertificates();

  // Scan config
  const [selectedClusters, setSelectedClusters] = useState<string[]>(
    selectedCluster ? [selectedCluster] : []
  );
  const [filterType, setFilterType] = useState<"all" | "ingress" | "common">("all");
  const [statusFilters, setStatusFilters] = useState<Set<string>>(new Set(["valid", "expiring", "expired"]));

  // UI state
  const [searchQuery, setSearchQuery] = useState("");
  const [clusterSearch, setClusterSearch] = useState("");
  const [selectedCert, setSelectedCert] = useState<CertificateInfo | null>(null);
  const [sortField, setSortField] = useState<SortField>("daysRemaining");
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");
  const [collapsedClusters, setCollapsedClusters] = useState<Set<string>>(new Set());

  // Modals
  const [detailModalOpen, setDetailModalOpen] = useState(false);
  const [copyModalOpen, setCopyModalOpen] = useState(false);
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [reportModalOpen, setReportModalOpen] = useState(false);

  // Copy state
  const [copyTargetClusters, setCopyTargetClusters] = useState<string[]>([]);
  const [copyTargetNamespaces, setCopyTargetNamespaces] = useState("");
  const [isCopying, setIsCopying] = useState(false);

  // Upload state
  const [uploadName, setUploadName] = useState("");
  const [uploadCrt, setUploadCrt] = useState("");
  const [uploadKey, setUploadKey] = useState("");
  const [uploadTargetClusters, setUploadTargetClusters] = useState<string[]>([]);
  const [uploadTargetNamespaces, setUploadTargetNamespaces] = useState("");
  const [isUploading, setIsUploading] = useState(false);

  // Report state
  const [isGeneratingReport, setIsGeneratingReport] = useState(false);

  // Extrair contextos dos clusters (com sufixo -admin)
  const clusterNames = useMemo(() => {
    if (!allClusters) return [] as string[];
    return allClusters.map((c: Cluster) => c.context);
  }, [allClusters]);

  // Clusters agrupados
  const clusterGroups = useMemo(() => {
    const groups = { prd: [] as string[], hlg: [] as string[], dev: [] as string[], other: [] as string[] };
    clusterNames.forEach((name: string) => {
      const lower = name.toLowerCase();
      if (lower.includes("-prd") || lower.includes("-prod")) groups.prd.push(name);
      else if (lower.includes("-hlg") || lower.includes("-uat") || lower.includes("-sit")) groups.hlg.push(name);
      else if (lower.includes("-dev")) groups.dev.push(name);
      else groups.other.push(name);
    });
    return groups;
  }, [clusterNames]);

  // Clusters filtrados pelo campo de busca
  const filteredClusterNames = useMemo(() => {
    if (!clusterSearch) return clusterNames;
    const q = clusterSearch.toLowerCase();
    return clusterNames.filter((name: string) => name.toLowerCase().includes(q));
  }, [clusterNames, clusterSearch]);

  const toggleCluster = (cluster: string) => {
    setSelectedClusters(prev =>
      prev.includes(cluster) ? prev.filter(c => c !== cluster) : [...prev, cluster]
    );
  };

  const selectClusterGroup = (group: "prd" | "hlg" | "dev" | "all") => {
    if (group === "all") {
      setSelectedClusters(clusterNames);
    } else {
      setSelectedClusters(clusterGroups[group]);
    }
  };

  const clearClusterSelection = () => {
    setSelectedClusters([]);
  };

  const toggleStatusFilter = (status: string) => {
    setStatusFilters(prev => {
      const next = new Set(prev);
      if (next.has(status)) next.delete(status);
      else next.add(status);
      return next;
    });
  };

  const toggleClusterCollapse = (cluster: string) => {
    setCollapsedClusters(prev => {
      const next = new Set(prev);
      if (next.has(cluster)) next.delete(cluster);
      else next.add(cluster);
      return next;
    });
  };

  // Executar scan
  const handleScan = async () => {
    if (selectedClusters.length === 0) {
      toast.error("Selecione pelo menos um cluster");
      return;
    }

    try {
      const req: ScanRequest = {
        clusters: selectedClusters,
        filter: filterType,
      };
      await scanCertificates(req);
      toast.success("Scan concluido");
    } catch {
      toast.error("Erro ao escanear certificados", {
        description: scanError || "Erro desconhecido",
      });
    }
  };

  // Filtrar e ordenar certificados
  const filteredCerts = useMemo(() => {
    if (!scanResult?.certificates) return [];

    let certs = scanResult.certificates.filter(cert => {
      if (!statusFilters.has(cert.status)) return false;
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        return (
          cert.secretName.toLowerCase().includes(q) ||
          cert.namespace.toLowerCase().includes(q) ||
          cert.cluster.toLowerCase().includes(q) ||
          cert.subject.toLowerCase().includes(q) ||
          cert.dnsNames?.some(d => d.toLowerCase().includes(q))
        );
      }
      return true;
    });

    certs.sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case "status": {
          const order = { expired: 0, expiring: 1, valid: 2 };
          cmp = (order[a.status] || 0) - (order[b.status] || 0);
          break;
        }
        case "daysRemaining":
          cmp = a.daysRemaining - b.daysRemaining;
          break;
        case "secretName":
          cmp = a.secretName.localeCompare(b.secretName);
          break;
        case "namespace":
          cmp = a.namespace.localeCompare(b.namespace);
          break;
        case "cluster":
          cmp = a.cluster.localeCompare(b.cluster);
          break;
        case "subject":
          cmp = a.subject.localeCompare(b.subject);
          break;
      }
      return sortDirection === "asc" ? cmp : -cmp;
    });

    return certs;
  }, [scanResult, statusFilters, searchQuery, sortField, sortDirection]);

  // Certificados agrupados por cluster
  const certsByCluster = useMemo(() => {
    const map = new Map<string, CertificateInfo[]>();
    filteredCerts.forEach(cert => {
      const existing = map.get(cert.cluster) || [];
      existing.push(cert);
      map.set(cert.cluster, existing);
    });
    return map;
  }, [filteredCerts]);

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(prev => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDirection("asc");
    }
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "valid":
        return <Badge className="bg-green-500/20 text-green-400 border-green-500/30">Valido</Badge>;
      case "expiring":
        return <Badge className="bg-yellow-500/20 text-yellow-400 border-yellow-500/30">Expirando</Badge>;
      case "expired":
        return <Badge className="bg-red-500/20 text-red-400 border-red-500/30">Expirado</Badge>;
      default:
        return <Badge variant="secondary">{status}</Badge>;
    }
  };

  const getStatusLabel = (status: string): string => {
    switch (status) {
      case "valid": return "Valido";
      case "expiring": return "Expirando";
      case "expired": return "Expirado";
      default: return status;
    }
  };

  // Abrir modal de detalhes
  const handleCertClick = (cert: CertificateInfo) => {
    setSelectedCert(cert);
    setDetailModalOpen(true);
  };

  // Copy handler
  const handleCopy = async () => {
    if (!selectedCert) return;
    setIsCopying(true);
    try {
      const req: CopyRequest = {
        sourceCluster: selectedCert.cluster,
        sourceNamespace: selectedCert.namespace,
        secretName: selectedCert.secretName,
        targetClusters: copyTargetClusters,
        targetNamespaces: copyTargetNamespaces.split(",").map(s => s.trim()).filter(Boolean),
      };
      const copied = await copyCertificate(req);
      toast.success(`Certificado copiado para ${copied} destino(s)`);
      setCopyModalOpen(false);
    } catch (err) {
      toast.error("Erro ao copiar certificado", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsCopying(false);
    }
  };

  // Upload handler
  const handleUpload = async () => {
    setIsUploading(true);
    try {
      const req: UploadRequest = {
        name: uploadName,
        tlsCrt: uploadCrt,
        tlsKey: uploadKey,
        targetClusters: uploadTargetClusters,
        targetNamespaces: uploadTargetNamespaces.split(",").map(s => s.trim()).filter(Boolean),
      };
      const uploaded = await uploadCertificate(req);
      toast.success(`Certificado uploaded para ${uploaded} destino(s)`);
      setUploadModalOpen(false);
      setUploadName("");
      setUploadCrt("");
      setUploadKey("");
    } catch (err) {
      toast.error("Erro ao fazer upload", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsUploading(false);
    }
  };

  // Formatar data
  const formatDate = (dateStr: string): string => {
    return new Date(dateStr).toLocaleString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  // ============================================================================
  // EXPORTACAO PDF (jsPDF + autoTable) - AGRUPADO POR CLUSTER
  // ============================================================================
  const generatePDFReport = useCallback(async () => {
    if (!scanResult) return;

    const doc = new jsPDF({ orientation: "landscape", unit: "mm", format: "a4" });
    const pageWidth = doc.internal.pageSize.getWidth();

    // Header com logo
    let yPos = await addLogoHeaderToPDF(
      doc,
      "RELATORIO DE CERTIFICADOS TLS",
      `Data de Geracao: ${formatDate(new Date().toISOString())}`
    );

    // Sumario executivo
    doc.setFontSize(12);
    doc.setFont("helvetica", "bold");
    doc.text("SUMARIO EXECUTIVO", 14, yPos);
    yPos += 8;

    autoTable(doc, {
      startY: yPos,
      head: [["Metrica", "Valor"]],
      body: [
        ["Total de Certificados", String(scanResult.summary.total)],
        ["Validos", String(scanResult.summary.valid)],
        ["Expirando (<30 dias)", String(scanResult.summary.expiring)],
        ["Expirados", String(scanResult.summary.expired)],
        ["Clusters Escaneados", String(selectedClusters.length)],
        ["Filtro Aplicado", filterType === "all" ? "Todos" : filterType === "ingress" ? "Usados por Ingress" : "Comuns (multi-ref)"],
        ["Data do Scan", formatDate(scanResult.scannedAt)],
      ],
      theme: "grid",
      headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
      bodyStyles: { fontSize: 8 },
      columnStyles: { 0: { fontStyle: "bold", cellWidth: 60 } },
      margin: { left: 14, right: 14 },
    });

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    yPos = (doc as any).lastAutoTable?.finalY + 10 || yPos + 50;

    // Alertas (expirados e expirando)
    const alerts = filteredCerts.filter(c => c.status === "expired" || c.status === "expiring");
    if (alerts.length > 0) {
      if (yPos > doc.internal.pageSize.getHeight() - 40) {
        doc.addPage();
        yPos = 20;
      }

      doc.setFontSize(12);
      doc.setFont("helvetica", "bold");
      doc.text("ALERTAS", 14, yPos);
      yPos += 8;

      autoTable(doc, {
        startY: yPos,
        head: [["Status", "Secret", "Namespace", "Cluster", "Dias Restantes", "Expira Em"]],
        body: alerts.map(cert => [
          removeEmojis(getStatusLabel(cert.status)),
          cert.secretName,
          cert.namespace,
          cert.cluster,
          `${cert.daysRemaining}d`,
          new Date(cert.notAfter).toLocaleDateString("pt-BR"),
        ]),
        theme: "grid",
        headStyles: { fillColor: [192, 57, 43], fontSize: 8 },
        bodyStyles: { fontSize: 7 },
        didParseCell: (data) => {
          if (data.section === "body" && data.column.index === 0) {
            const status = data.cell.raw as string;
            if (status === "Expirado") {
              data.cell.styles.textColor = [192, 57, 43];
              data.cell.styles.fontStyle = "bold";
            } else if (status === "Expirando") {
              data.cell.styles.textColor = [211, 84, 0];
              data.cell.styles.fontStyle = "bold";
            }
          }
        },
        margin: { left: 14, right: 14 },
      });

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      yPos = (doc as any).lastAutoTable?.finalY + 10 || yPos + 50;
    }

    // Certificados agrupados por cluster
    const clusters = Array.from(certsByCluster.entries());
    for (const [clusterName, certs] of clusters) {
      if (yPos > doc.internal.pageSize.getHeight() - 40) {
        doc.addPage();
        yPos = 20;
      }

      const clusterExpired = certs.filter(c => c.status === "expired").length;
      const clusterExpiring = certs.filter(c => c.status === "expiring").length;
      const clusterValid = certs.filter(c => c.status === "valid").length;

      doc.setFontSize(11);
      doc.setFont("helvetica", "bold");
      doc.text(`CLUSTER: ${clusterName}`, 14, yPos);
      doc.setFontSize(8);
      doc.setFont("helvetica", "normal");
      doc.text(`${certs.length} certificados (${clusterValid} validos, ${clusterExpiring} expirando, ${clusterExpired} expirados)`, 14, yPos + 5);
      yPos += 10;

      autoTable(doc, {
        startY: yPos,
        head: [["Status", "Secret", "Namespace", "Subject", "Dias", "Expira Em", "SANs", "Ingresses"]],
        body: certs.map(cert => [
          removeEmojis(getStatusLabel(cert.status)),
          cert.secretName,
          cert.namespace,
          cert.subject,
          `${cert.daysRemaining}d`,
          new Date(cert.notAfter).toLocaleDateString("pt-BR"),
          cert.dnsNames?.length ? String(cert.dnsNames.length) : "0",
          cert.usedByIngresses?.length ? String(cert.usedByIngresses.length) : "0",
        ]),
        theme: "grid",
        headStyles: { fillColor: [41, 128, 185], fontSize: 7 },
        bodyStyles: { fontSize: 6 },
        didParseCell: (data) => {
          if (data.section === "body" && data.column.index === 0) {
            const status = data.cell.raw as string;
            if (status === "Expirado") {
              data.cell.styles.textColor = [192, 57, 43];
              data.cell.styles.fontStyle = "bold";
            } else if (status === "Expirando") {
              data.cell.styles.textColor = [211, 84, 0];
              data.cell.styles.fontStyle = "bold";
            } else {
              data.cell.styles.textColor = [39, 174, 96];
            }
          }
        },
        margin: { left: 14, right: 14 },
      });

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      yPos = (doc as any).lastAutoTable?.finalY + 12 || yPos + 50;
    }

    // Footer em todas as paginas
    const totalPages = doc.getNumberOfPages();
    for (let i = 1; i <= totalPages; i++) {
      doc.setPage(i);
      addFooterToPDF(doc, i, totalPages);
    }

    const filename = `certificados-tls-${new Date().toISOString().split("T")[0]}.pdf`;
    doc.save(filename);
    toast.success(`Relatorio PDF exportado: ${filename}`);
  }, [scanResult, filteredCerts, certsByCluster, selectedClusters, filterType]);

  // ============================================================================
  // EXPORTACAO MARKDOWN - AGRUPADO POR CLUSTER
  // ============================================================================
  const generateMarkdownReport = useCallback(() => {
    if (!scanResult) return;

    let md = getMarkdownHeader(
      "RELATORIO DE CERTIFICADOS TLS",
      `Data de Geracao: ${formatDate(new Date().toISOString())}`
    );

    // Sumario
    md += `## SUMARIO EXECUTIVO\n\n`;
    md += `| Metrica | Valor |\n`;
    md += `|---------|-------|\n`;
    md += `| Total de Certificados | ${scanResult.summary.total} |\n`;
    md += `| Validos | ${scanResult.summary.valid} |\n`;
    md += `| Expirando (<30 dias) | ${scanResult.summary.expiring} |\n`;
    md += `| Expirados | ${scanResult.summary.expired} |\n`;
    md += `| Clusters Escaneados | ${selectedClusters.length} |\n`;
    md += `| Filtro Aplicado | ${filterType === "all" ? "Todos" : filterType === "ingress" ? "Usados por Ingress" : "Comuns (multi-ref)"} |\n`;
    md += `| Data do Scan | ${formatDate(scanResult.scannedAt)} |\n\n`;

    // Alertas
    const alerts = filteredCerts.filter(c => c.status === "expired" || c.status === "expiring");
    if (alerts.length > 0) {
      md += `## ALERTAS\n\n`;
      md += `> **${alerts.filter(c => c.status === "expired").length}** certificados expirados e **${alerts.filter(c => c.status === "expiring").length}** expirando em menos de 30 dias.\n\n`;

      md += `| Status | Secret | Namespace | Cluster | Dias Restantes | Expira Em |\n`;
      md += `|--------|--------|-----------|---------|----------------|-----------|\n`;
      alerts.forEach(cert => {
        md += `| ${getStatusLabel(cert.status)} | ${cert.secretName} | ${cert.namespace} | ${cert.cluster} | ${cert.daysRemaining}d | ${new Date(cert.notAfter).toLocaleDateString("pt-BR")} |\n`;
      });
      md += `\n`;
    }

    // Certificados por cluster
    md += `## CERTIFICADOS POR CLUSTER\n\n`;
    const clusters = Array.from(certsByCluster.entries());
    for (const [clusterName, certs] of clusters) {
      const clusterExpired = certs.filter(c => c.status === "expired").length;
      const clusterExpiring = certs.filter(c => c.status === "expiring").length;
      const clusterValid = certs.filter(c => c.status === "valid").length;

      md += `### ${clusterName}\n\n`;
      md += `> ${certs.length} certificados (${clusterValid} validos, ${clusterExpiring} expirando, ${clusterExpired} expirados)\n\n`;

      md += `| Status | Secret | Namespace | Subject | Dias | Expira Em | SANs | Ingresses |\n`;
      md += `|--------|--------|-----------|---------|------|-----------|------|-----------|\n`;
      certs.forEach(cert => {
        md += `| ${getStatusLabel(cert.status)} | ${cert.secretName} | ${cert.namespace} | ${cert.subject} | ${cert.daysRemaining}d | ${new Date(cert.notAfter).toLocaleDateString("pt-BR")} | ${cert.dnsNames?.length || 0} | ${cert.usedByIngresses?.length || 0} |\n`;
      });
      md += `\n`;
    }

    md += `---\n\n*Gerado por SRE Logistica - K8s HPA Manager*\n`;

    const blob = new Blob([md], { type: "text/markdown" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `certificados-tls-${new Date().toISOString().split("T")[0]}.md`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success("Relatorio Markdown exportado");
  }, [scanResult, filteredCerts, certsByCluster, selectedClusters, filterType]);

  // ============================================================================
  // EXPORTACAO CSV - AGRUPADO POR CLUSTER
  // ============================================================================
  const generateCSVReport = useCallback(() => {
    if (!scanResult) return;

    const escapeCSV = (val: string): string => {
      if (val.includes(",") || val.includes('"') || val.includes("\n")) {
        return `"${val.replace(/"/g, '""')}"`;
      }
      return val;
    };

    const headers = ["Cluster", "Status", "Secret", "Namespace", "Subject", "Issuer", "Dias_Restantes", "Expira_Em", "Emitido_Em", "Algoritmo", "SANs", "Ingresses", "cert-manager"];
    const rows: string[] = [headers.join(",")];

    const clusters = Array.from(certsByCluster.entries());
    for (const [, certs] of clusters) {
      certs.forEach(cert => {
        rows.push([
          escapeCSV(cert.cluster),
          escapeCSV(getStatusLabel(cert.status)),
          escapeCSV(cert.secretName),
          escapeCSV(cert.namespace),
          escapeCSV(cert.subject),
          escapeCSV(cert.issuer),
          String(cert.daysRemaining),
          new Date(cert.notAfter).toLocaleDateString("pt-BR"),
          new Date(cert.notBefore).toLocaleDateString("pt-BR"),
          `${cert.keyAlgorithm} ${cert.keySize}bit`,
          escapeCSV(cert.dnsNames?.join("; ") || ""),
          String(cert.usedByIngresses?.length || 0),
          escapeCSV(cert.certManager?.certificateName || ""),
        ].join(","));
      });
    }

    const blob = new Blob(["\uFEFF" + rows.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `certificados-tls-${new Date().toISOString().split("T")[0]}.csv`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success("Relatorio CSV exportado");
  }, [scanResult, certsByCluster]);

  const handleExportReport = async (format: "pdf" | "markdown" | "csv") => {
    setIsGeneratingReport(true);
    try {
      if (format === "pdf") {
        await generatePDFReport();
      } else if (format === "csv") {
        generateCSVReport();
      } else {
        generateMarkdownReport();
      }
      setReportModalOpen(false);
    } catch (err) {
      toast.error("Erro ao gerar relatorio", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsGeneratingReport(false);
    }
  };

  // Left panel - Scan config
  const leftContent = (
    <div className="space-y-4">
      {/* Clusters */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Clusters</Label>
        <div className="flex flex-wrap gap-1 mb-2">
          <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectClusterGroup("all")}>
            Todos
          </Button>
          {clusterGroups.prd.length > 0 && (
            <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectClusterGroup("prd")}>
              Producao
            </Button>
          )}
          {clusterGroups.hlg.length > 0 && (
            <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectClusterGroup("hlg")}>
              Homologacao
            </Button>
          )}
          {clusterGroups.dev.length > 0 && (
            <Button size="sm" variant="outline" className="text-xs h-6" onClick={() => selectClusterGroup("dev")}>
              Dev
            </Button>
          )}
          {selectedClusters.length > 0 && (
            <Button
              size="sm"
              variant="ghost"
              className="text-xs h-6 text-red-400 hover:text-red-300"
              onClick={clearClusterSelection}
            >
              <X className="h-3 w-3 mr-1" />
              Limpar ({selectedClusters.length})
            </Button>
          )}
        </div>
        {/* Campo de busca de clusters */}
        <div className="relative mb-2">
          <Search className="absolute left-2 top-1/2 transform -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          <Input
            placeholder="Filtrar clusters..."
            value={clusterSearch}
            onChange={(e) => setClusterSearch(e.target.value)}
            className="pl-7 h-7 text-xs"
          />
          {clusterSearch && (
            <button
              className="absolute right-2 top-1/2 transform -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => setClusterSearch("")}
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <ScrollArea className="h-[180px]">
          <div className="space-y-1">
            {filteredClusterNames.map((cluster: string) => (
              <div key={cluster} className="flex items-center gap-2">
                <Checkbox
                  id={`cluster-${cluster}`}
                  checked={selectedClusters.includes(cluster)}
                  onCheckedChange={() => toggleCluster(cluster)}
                />
                <Label htmlFor={`cluster-${cluster}`} className="text-xs cursor-pointer truncate">
                  {cluster}
                </Label>
              </div>
            ))}
            {filteredClusterNames.length === 0 && clusterSearch && (
              <p className="text-xs text-muted-foreground text-center py-2">
                Nenhum cluster encontrado
              </p>
            )}
          </div>
        </ScrollArea>
      </div>

      {/* Filtro por tipo */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Tipo</Label>
        <Select value={filterType} onValueChange={(v) => setFilterType(v as typeof filterType)}>
          <SelectTrigger className="h-8 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Todos</SelectItem>
            <SelectItem value="ingress">Usados por Ingress</SelectItem>
            <SelectItem value="common">Comuns (multi-ref)</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Filtro por status */}
      <div>
        <Label className="text-sm font-medium mb-2 block">Status</Label>
        <div className="space-y-1">
          {[
            { key: "valid", label: "Validos", color: "text-green-400" },
            { key: "expiring", label: "Expirando (<30d)", color: "text-yellow-400" },
            { key: "expired", label: "Expirados", color: "text-red-400" },
          ].map(({ key, label, color }) => (
            <div key={key} className="flex items-center gap-2">
              <Checkbox
                id={`status-${key}`}
                checked={statusFilters.has(key)}
                onCheckedChange={() => toggleStatusFilter(key)}
              />
              <Label htmlFor={`status-${key}`} className={`text-xs cursor-pointer ${color}`}>
                {label}
                {scanResult && (
                  <span className="ml-1 text-muted-foreground">
                    ({scanResult.summary[key as keyof typeof scanResult.summary] ?? 0})
                  </span>
                )}
              </Label>
            </div>
          ))}
        </div>
      </div>

      {/* Botao scan */}
      <Button
        className="w-full"
        onClick={handleScan}
        disabled={scanning || selectedClusters.length === 0}
      >
        {scanning ? (
          <><Loader2 className="h-4 w-4 mr-2 animate-spin" /> Escaneando...</>
        ) : (
          <><Shield className="h-4 w-4 mr-2" /> Escanear</>
        )}
      </Button>

      {/* Resumo */}
      {scanResult && (
        <div className="space-y-2 pt-2 border-t border-border/50">
          <p className="text-xs text-muted-foreground">Resultado do Scan</p>
          <div className="grid grid-cols-2 gap-2">
            <div className="text-center p-2 rounded bg-green-500/10">
              <p className="text-lg font-bold text-green-400">{scanResult.summary.valid}</p>
              <p className="text-xs text-muted-foreground">Validos</p>
            </div>
            <div className="text-center p-2 rounded bg-yellow-500/10">
              <p className="text-lg font-bold text-yellow-400">{scanResult.summary.expiring}</p>
              <p className="text-xs text-muted-foreground">Expirando</p>
            </div>
            <div className="text-center p-2 rounded bg-red-500/10">
              <p className="text-lg font-bold text-red-400">{scanResult.summary.expired}</p>
              <p className="text-xs text-muted-foreground">Expirados</p>
            </div>
            <div className="text-center p-2 rounded bg-blue-500/10">
              <p className="text-lg font-bold text-blue-400">{scanResult.summary.total}</p>
              <p className="text-xs text-muted-foreground">Total</p>
            </div>
          </div>
        </div>
      )}

      {/* Botoes de acao */}
      {scanResult && (
        <div className="space-y-2">
          <Button
            variant="outline"
            size="sm"
            className="w-full"
            onClick={() => setReportModalOpen(true)}
          >
            <FileDown className="h-3 w-3 mr-2" />
            Exportar Relatorio
          </Button>
          <ProtectedAction>
            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={() => setUploadModalOpen(true)}
            >
              <Upload className="h-3 w-3 mr-2" />
              Upload Certificado
            </Button>
          </ProtectedAction>
        </div>
      )}
    </div>
  );

  // Right panel - Results (agrupados por cluster)
  const rightContent = (
    <div className="space-y-4">
      {/* Search bar */}
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-2 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Buscar por nome, namespace, subject, SAN..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-8 h-8 text-sm"
          />
          {searchQuery && (
            <button
              className="absolute right-2 top-1/2 transform -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => setSearchQuery("")}
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <Button variant="outline" size="sm" onClick={handleScan} disabled={scanning}>
          <RefreshCcw className={`h-4 w-4 ${scanning ? "animate-spin" : ""}`} />
        </Button>
      </div>

      {/* Estado vazio */}
      {!scanResult ? (
        <div className="flex flex-col items-center justify-center h-[400px] text-muted-foreground">
          <Shield className="h-16 w-16 mb-4 opacity-30" />
          <p className="text-lg font-medium">Nenhum scan realizado</p>
          <p className="text-sm">Selecione clusters e clique em "Escanear"</p>
        </div>
      ) : filteredCerts.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-[400px] text-muted-foreground">
          <ShieldCheck className="h-16 w-16 mb-4 opacity-30" />
          <p className="text-lg font-medium">Nenhum certificado encontrado</p>
          <p className="text-sm">Ajuste os filtros ou faca um novo scan</p>
        </div>
      ) : (
        <ScrollArea className="h-[calc(100vh-280px)]">
          <div className="space-y-2 pr-2">
            {/* Header da tabela */}
            <div className="grid grid-cols-12 gap-2 px-3 py-2 text-xs font-medium text-muted-foreground border-b border-border/50 sticky top-0 bg-background z-10">
              <button className="col-span-1 flex items-center gap-1 hover:text-foreground" onClick={() => handleSort("status")}>
                Status <ArrowUpDown className="h-3 w-3" />
              </button>
              <button className="col-span-3 flex items-center gap-1 hover:text-foreground text-left" onClick={() => handleSort("secretName")}>
                Secret <ArrowUpDown className="h-3 w-3" />
              </button>
              <button className="col-span-3 flex items-center gap-1 hover:text-foreground text-left" onClick={() => handleSort("namespace")}>
                Namespace <ArrowUpDown className="h-3 w-3" />
              </button>
              <button className="col-span-3 flex items-center gap-1 hover:text-foreground text-left" onClick={() => handleSort("subject")}>
                Subject <ArrowUpDown className="h-3 w-3" />
              </button>
              <button className="col-span-1 flex items-center gap-1 hover:text-foreground" onClick={() => handleSort("daysRemaining")}>
                Dias <ArrowUpDown className="h-3 w-3" />
              </button>
              <div className="col-span-1 text-center">Ing</div>
            </div>

            {/* Certificados agrupados por cluster */}
            {Array.from(certsByCluster.entries()).map(([clusterName, certs]) => {
              const isCollapsed = collapsedClusters.has(clusterName);
              const clusterExpired = certs.filter(c => c.status === "expired").length;
              const clusterExpiring = certs.filter(c => c.status === "expiring").length;

              return (
                <div key={clusterName}>
                  {/* Header do cluster */}
                  <button
                    className="w-full flex items-center gap-2 px-3 py-2 bg-accent/30 hover:bg-accent/50 rounded transition-colors text-left"
                    onClick={() => toggleClusterCollapse(clusterName)}
                  >
                    {isCollapsed ? (
                      <ChevronRight className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                    ) : (
                      <ChevronDown className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                    )}
                    <Server className="h-3 w-3 text-blue-400 flex-shrink-0" />
                    <span className="text-xs font-semibold truncate">{clusterName}</span>
                    <Badge variant="secondary" className="text-xs ml-auto">
                      {certs.length}
                    </Badge>
                    {clusterExpired > 0 && (
                      <Badge className="bg-red-500/20 text-red-400 border-red-500/30 text-xs">
                        {clusterExpired} expirados
                      </Badge>
                    )}
                    {clusterExpiring > 0 && (
                      <Badge className="bg-yellow-500/20 text-yellow-400 border-yellow-500/30 text-xs">
                        {clusterExpiring} expirando
                      </Badge>
                    )}
                  </button>

                  {/* Certificados do cluster */}
                  {!isCollapsed && (
                    <div className="space-y-0">
                      {certs.map((cert, idx) => (
                        <div
                          key={`${cert.cluster}-${cert.namespace}-${cert.secretName}-${idx}`}
                          className="grid grid-cols-12 gap-2 px-3 py-2 text-xs rounded cursor-pointer hover:bg-accent/50 transition-colors border-l-2 border-transparent hover:border-primary/30 ml-2"
                          onClick={() => handleCertClick(cert)}
                        >
                          <div className="col-span-1">{getStatusBadge(cert.status)}</div>
                          <div className="col-span-3 truncate font-medium" title={cert.secretName}>
                            {cert.secretName}
                          </div>
                          <div className="col-span-3 truncate text-muted-foreground" title={cert.namespace}>
                            {cert.namespace}
                          </div>
                          <div className="col-span-3 truncate text-muted-foreground" title={cert.subject}>
                            {cert.subject}
                          </div>
                          <div className={`col-span-1 text-center font-medium ${
                            cert.daysRemaining < 0 ? "text-red-400" :
                            cert.daysRemaining <= 30 ? "text-yellow-400" :
                            "text-green-400"
                          }`}>
                            {cert.daysRemaining}d
                          </div>
                          <div className="col-span-1 text-center text-muted-foreground">
                            {cert.usedByIngresses?.length || 0}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </ScrollArea>
      )}
    </div>
  );

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Certificados TLS",
          titleAction: scanResult ? (
            <Badge variant="secondary" className="text-xs">
              {scanResult.summary.total} certificados
            </Badge>
          ) : undefined,
          content: leftContent,
        }}
        rightPanel={{
          title: scanResult
            ? `Resultados (${filteredCerts.length})`
            : "Resultados",
          content: rightContent,
        }}
      />

      {/* Modal de Detalhes do Certificado */}
      <CertificateDetailModal
        open={detailModalOpen}
        onOpenChange={setDetailModalOpen}
        cert={selectedCert}
        footerExtra={
          <ProtectedAction>
            <Button
              variant="outline"
              onClick={() => {
                setCopyTargetClusters([]);
                setCopyTargetNamespaces("");
                setDetailModalOpen(false);
                setCopyModalOpen(true);
              }}
            >
              <Copy className="h-4 w-4 mr-2" />
              Copiar para...
            </Button>
          </ProtectedAction>
        }
      />

      {/* Modal de Copia */}
      <Dialog open={copyModalOpen} onOpenChange={setCopyModalOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Copiar Certificado</DialogTitle>
            <DialogDescription>
              Copiar {selectedCert?.secretName} de {selectedCert?.namespace}/{selectedCert?.cluster}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <Label className="text-sm">Clusters destino</Label>
              <ScrollArea className="h-[150px] mt-1 border rounded p-2">
                {clusterNames.map((cluster: string) => (
                  <div key={cluster} className="flex items-center gap-2 py-1">
                    <Checkbox
                      checked={copyTargetClusters.includes(cluster)}
                      onCheckedChange={() => {
                        setCopyTargetClusters(prev =>
                          prev.includes(cluster) ? prev.filter(c => c !== cluster) : [...prev, cluster]
                        );
                      }}
                    />
                    <Label className="text-xs cursor-pointer">{cluster}</Label>
                  </div>
                ))}
              </ScrollArea>
            </div>
            <div>
              <Label className="text-sm">Namespaces destino (separados por virgula)</Label>
              <Input
                value={copyTargetNamespaces}
                onChange={(e) => setCopyTargetNamespaces(e.target.value)}
                placeholder="default, production, staging"
                className="mt-1"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCopyModalOpen(false)}>
              Cancelar
            </Button>
            <Button
              onClick={handleCopy}
              disabled={isCopying || copyTargetClusters.length === 0 || !copyTargetNamespaces.trim()}
            >
              {isCopying ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Copy className="h-4 w-4 mr-2" />}
              Copiar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Upload */}
      <Dialog open={uploadModalOpen} onOpenChange={setUploadModalOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Upload de Certificado TLS</DialogTitle>
            <DialogDescription>
              Enviar certificado PEM para clusters e namespaces selecionados
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <Label className="text-sm">Nome do Secret</Label>
              <Input
                value={uploadName}
                onChange={(e) => setUploadName(e.target.value)}
                placeholder="meu-certificado-tls"
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-sm">Certificado (tls.crt - PEM)</Label>
              <textarea
                value={uploadCrt}
                onChange={(e) => setUploadCrt(e.target.value)}
                placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                className="w-full mt-1 h-32 p-2 text-xs font-mono bg-background border rounded resize-none"
              />
            </div>
            <div>
              <Label className="text-sm">Chave Privada (tls.key - PEM)</Label>
              <textarea
                value={uploadKey}
                onChange={(e) => setUploadKey(e.target.value)}
                placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                className="w-full mt-1 h-32 p-2 text-xs font-mono bg-background border rounded resize-none"
              />
            </div>
            <div>
              <Label className="text-sm">Clusters destino</Label>
              <ScrollArea className="h-[120px] mt-1 border rounded p-2">
                {clusterNames.map((cluster: string) => (
                  <div key={cluster} className="flex items-center gap-2 py-1">
                    <Checkbox
                      checked={uploadTargetClusters.includes(cluster)}
                      onCheckedChange={() => {
                        setUploadTargetClusters(prev =>
                          prev.includes(cluster) ? prev.filter(c => c !== cluster) : [...prev, cluster]
                        );
                      }}
                    />
                    <Label className="text-xs cursor-pointer">{cluster}</Label>
                  </div>
                ))}
              </ScrollArea>
            </div>
            <div>
              <Label className="text-sm">Namespaces destino (separados por virgula)</Label>
              <Input
                value={uploadTargetNamespaces}
                onChange={(e) => setUploadTargetNamespaces(e.target.value)}
                placeholder="default, production"
                className="mt-1"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setUploadModalOpen(false)}>
              Cancelar
            </Button>
            <Button
              onClick={handleUpload}
              disabled={isUploading || !uploadName || !uploadCrt || !uploadKey || uploadTargetClusters.length === 0 || !uploadTargetNamespaces.trim()}
            >
              {isUploading ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Upload className="h-4 w-4 mr-2" />}
              Upload
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal de Exportacao de Relatorio */}
      <Dialog open={reportModalOpen} onOpenChange={setReportModalOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Exportar Relatorio de Certificados TLS</DialogTitle>
            <DialogDescription>
              Relatorio agrupado por cluster com {filteredCerts.length} certificados.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="text-xs space-y-1 p-3 bg-muted/30 rounded">
              <p><strong>Clusters:</strong> {certsByCluster.size} no relatorio</p>
              <p><strong>Certificados:</strong> {filteredCerts.length} total</p>
              {scanResult && (
                <>
                  <p className="text-green-400">Validos: {scanResult.summary.valid}</p>
                  <p className="text-yellow-400">Expirando: {scanResult.summary.expiring}</p>
                  <p className="text-red-400">Expirados: {scanResult.summary.expired}</p>
                </>
              )}
            </div>
          </div>
          <DialogFooter className="flex gap-2 sm:gap-2">
            <Button variant="outline" onClick={() => setReportModalOpen(false)}>
              Cancelar
            </Button>
            <Button
              variant="outline"
              onClick={() => handleExportReport("csv")}
              disabled={isGeneratingReport}
            >
              {isGeneratingReport ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <Table className="h-4 w-4 mr-2" />
              )}
              CSV
            </Button>
            <Button
              variant="outline"
              onClick={() => handleExportReport("markdown")}
              disabled={isGeneratingReport}
            >
              {isGeneratingReport ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <FileText className="h-4 w-4 mr-2" />
              )}
              Markdown
            </Button>
            <Button
              onClick={() => handleExportReport("pdf")}
              disabled={isGeneratingReport}
            >
              {isGeneratingReport ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <Download className="h-4 w-4 mr-2" />
              )}
              PDF
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

