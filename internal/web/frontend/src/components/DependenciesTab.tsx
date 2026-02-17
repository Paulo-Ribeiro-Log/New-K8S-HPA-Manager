import { useState, useMemo, useEffect, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { SplitView } from "@/components/SplitView";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import {
  Search,
  Loader2,
  RefreshCcw,
  Database,
  X,
  Layers,
  FileJson,
  FileSpreadsheet,
  FileText,
  FileType,
  Clock,
  AlertCircle,
  Download,
} from "lucide-react";
import { toast } from "sonner";
import { useClusters } from "@/hooks/useAPI";
import { apiClient } from "@/lib/api/client";
import { addLogoHeaderToPDF, getMarkdownHeader } from "@/lib/logoUtils";

// Tipos
interface DependencyRecord {
  id: number;
  service_name: string;
  service_type: string;
  topic_name?: string; // Tópico/Fila/Stream (para Kafka, EventHub, etc)
  cluster: string;
  namespace: string;
  deployment: string;
  source_type: string;
  source_name: string;
  source_key: string;
  first_seen: string;
  last_seen: string;
}

interface RegistryStats {
  total_dependencies: number;
  unique_services: number;
  unique_clusters: number;
  unique_namespaces: number;
  by_type: Record<string, number>;
  last_scan?: string;
}

// Cores por tipo de serviço
const SERVICE_TYPE_COLORS: Record<string, string> = {
  rds: "bg-blue-500/20 text-blue-600 border-blue-500/30",
  kafka: "bg-purple-500/20 text-purple-600 border-purple-500/30",
  eventhub: "bg-violet-500/20 text-violet-600 border-violet-500/30",
  redis: "bg-red-500/20 text-red-600 border-red-500/30",
  mongodb: "bg-green-500/20 text-green-600 border-green-500/30",
  sql: "bg-cyan-500/20 text-cyan-600 border-cyan-500/30",
  storage: "bg-amber-500/20 text-amber-600 border-amber-500/30",
  api: "bg-pink-500/20 text-pink-600 border-pink-500/30",
  other: "bg-gray-500/20 text-gray-600 border-gray-500/30",
};

const SERVICE_TYPE_LABELS: Record<string, string> = {
  rds: "RDS/Database",
  kafka: "Kafka",
  eventhub: "Event Hub",
  redis: "Redis/Cache",
  mongodb: "MongoDB",
  sql: "SQL Server",
  storage: "Storage",
  api: "API",
  other: "Outro",
};

// Chave do localStorage para controle de auto-scan
const LAST_SCAN_KEY = "dependencies-last-scan";
const SCAN_INTERVAL_MS = 24 * 60 * 60 * 1000; // 24 horas

export const DependenciesTab = () => {
  // Clusters disponíveis
  const { clusters: allClusters, loading: clustersLoading } = useClusters();

  // Estado de busca
  const [searchQuery, setSearchQuery] = useState("");
  const [reverseSearchQuery, setReverseSearchQuery] = useState("");
  const [filterType, setFilterType] = useState<string>("all");
  const [filterEnv, setFilterEnv] = useState<string>("all");
  const [filterEnvSearch, setFilterEnvSearch] = useState<string>("all");

  // Estado de auto-scan
  const [autoScanRunning, setAutoScanRunning] = useState(false);
  const [scanProgress, setScanProgress] = useState({ current: 0, total: 0 });

  // Tab ativa
  const [activeTab, setActiveTab] = useState<"registry" | "search">("registry");

  // Estado de exportação (painel esquerdo)
  const [exportCluster, setExportCluster] = useState<string>("all");

  // Estado de exportação (painel direito - visualização)
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportFormat, setExportFormat] = useState<"pdf" | "md" | "csv">("pdf");
  const [isExporting, setIsExporting] = useState(false);

  // Buscar clusters disponíveis no registry
  const { data: registryClusters } = useQuery({
    queryKey: ["dependencies-clusters"],
    queryFn: async () => {
      const response = await apiClient.get("/dependencies/clusters");
      if (response.success) return response.data as string[];
      return [];
    },
    staleTime: 60000,
  });

  // Buscar estatísticas do registry
  const { data: statsData, refetch: refetchStats } = useQuery({
    queryKey: ["dependencies-stats"],
    queryFn: async () => {
      const response = await apiClient.get("/dependencies/stats");
      if (response.success) return response.data as RegistryStats;
      throw new Error("Failed to load stats");
    },
    staleTime: 60000,
  });

  // Buscar todas dependências do registry
  const { data: registryData, isLoading: registryLoading, refetch: refetchRegistry } = useQuery({
    queryKey: ["dependencies-registry"],
    queryFn: async () => {
      const response = await apiClient.get("/dependencies/registry");
      if (response.success) return response.data;
      throw new Error("Failed to load registry");
    },
    staleTime: 60000,
  });

  // Busca reversa no registry
  const { data: searchResults, isLoading: isSearching, refetch: executeSearch } = useQuery({
    queryKey: ["dependencies-search", reverseSearchQuery],
    queryFn: async () => {
      if (!reverseSearchQuery.trim()) return null;
      const response = await apiClient.get(`/dependencies/search?q=${encodeURIComponent(reverseSearchQuery)}`);
      if (response.success) return response.data;
      throw new Error("Failed to search");
    },
    enabled: false, // Manual trigger
  });

  // Filtrar dependências do registry
  // Extrair ambiente do nome do cluster (ex: "akspriv-faturamento-prd-admin" → "prd")
  const extractEnv = (cluster: string): string => {
    const lower = cluster.toLowerCase();
    const envs = ["prd", "hlg", "dev", "sit", "stg"];
    for (const env of envs) {
      if (lower.includes(`-${env}`) || lower.includes(`_${env}`)) return env;
    }
    return "outro";
  };

  // Ambientes únicos disponíveis no registry
  const availableEnvs = useMemo(() => {
    if (!registryData?.dependencies) return [];
    const envSet = new Set<string>();
    (registryData.dependencies as DependencyRecord[]).forEach((dep) => {
      envSet.add(extractEnv(dep.cluster));
    });
    return Array.from(envSet).sort();
  }, [registryData]);

  // Ambientes únicos disponíveis nos resultados da busca reversa
  const availableEnvsSearch = useMemo(() => {
    if (!searchResults?.results) return [];
    const envSet = new Set<string>();
    (searchResults.results as DependencyRecord[]).forEach((dep) => {
      envSet.add(extractEnv(dep.cluster));
    });
    return Array.from(envSet).sort();
  }, [searchResults]);

  // Resultados da busca reversa filtrados por ambiente
  const filteredSearchResults = useMemo(() => {
    if (!searchResults?.results) return [];
    if (filterEnvSearch === "all") return searchResults.results as DependencyRecord[];
    return (searchResults.results as DependencyRecord[]).filter(
      (dep) => extractEnv(dep.cluster) === filterEnvSearch
    );
  }, [searchResults, filterEnvSearch]);

  const filteredDependencies = useMemo(() => {
    if (!registryData?.dependencies) return [];

    return (registryData.dependencies as DependencyRecord[]).filter((dep) => {
      // Filtro por tipo
      if (filterType !== "all" && dep.service_type !== filterType) return false;

      // Filtro por ambiente
      if (filterEnv !== "all" && extractEnv(dep.cluster) !== filterEnv) return false;

      // Filtro por busca
      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        return (
          dep.service_name.toLowerCase().includes(query) ||
          dep.service_type.toLowerCase().includes(query) ||
          dep.cluster.toLowerCase().includes(query) ||
          dep.namespace.toLowerCase().includes(query) ||
          dep.deployment?.toLowerCase().includes(query) ||
          dep.source_type?.toLowerCase().includes(query) ||
          dep.source_name?.toLowerCase().includes(query) ||
          dep.source_key?.toLowerCase().includes(query)
        );
      }

      return true;
    });
  }, [registryData, filterType, filterEnv, searchQuery]);

  // Agrupar por serviço
  const groupedByService = useMemo(() => {
    const grouped: Record<string, DependencyRecord[]> = {};
    for (const dep of filteredDependencies) {
      if (!grouped[dep.service_name]) {
        grouped[dep.service_name] = [];
      }
      grouped[dep.service_name].push(dep);
    }
    return grouped;
  }, [filteredDependencies]);

  // Auto-scan de todos os clusters (roda 1x a cada 24h)
  const runAutoScan = useCallback(async () => {
    if (!allClusters || allClusters.length === 0) return;

    // Verificar se já passou 24h desde o último scan
    const lastScan = localStorage.getItem(LAST_SCAN_KEY);
    if (lastScan) {
      const lastScanTime = parseInt(lastScan, 10);
      if (Date.now() - lastScanTime < SCAN_INTERVAL_MS) {
        console.log("[DependenciesTab] Skipping auto-scan (last scan was less than 24h ago)");
        return;
      }
    }

    console.log("[DependenciesTab] Starting auto-scan of all clusters");
    setAutoScanRunning(true);
    setScanProgress({ current: 0, total: allClusters.length });

    let successCount = 0;
    let errorCount = 0;

    for (let i = 0; i < allClusters.length; i++) {
      const cluster = allClusters[i];
      setScanProgress({ current: i + 1, total: allClusters.length });

      try {
        await apiClient.post(`/dependencies/scan/${cluster.context}`, {});
        successCount++;
      } catch (error) {
        console.error(`[DependenciesTab] Failed to scan cluster ${cluster.context}:`, error);
        errorCount++;
      }
    }

    // Salvar timestamp do scan
    localStorage.setItem(LAST_SCAN_KEY, Date.now().toString());

    setAutoScanRunning(false);
    setScanProgress({ current: 0, total: 0 });

    // Atualizar dados
    refetchStats();
    refetchRegistry();

    if (successCount > 0) {
      toast.success(`Auto-scan concluído: ${successCount} clusters escaneados`);
    }
    if (errorCount > 0) {
      toast.warning(`${errorCount} cluster(s) falharam no scan`);
    }
  }, [allClusters, refetchStats, refetchRegistry]);

  // Executar auto-scan ao montar componente
  useEffect(() => {
    if (!clustersLoading && allClusters && allClusters.length > 0) {
      runAutoScan();
    }
  }, [clustersLoading, allClusters, runAutoScan]);

  // Handler de busca reversa
  const handleReverseSearch = async () => {
    if (!reverseSearchQuery.trim()) {
      toast.error("Digite o nome do serviço para buscar");
      return;
    }
    executeSearch();
  };

  // Scan manual (força re-scan de todos os clusters)
  const handleManualScan = async () => {
    if (!allClusters || allClusters.length === 0) {
      toast.error("Nenhum cluster disponível");
      return;
    }

    // Limpar timestamp para forçar scan
    localStorage.removeItem(LAST_SCAN_KEY);
    await runAutoScan();
  };

  // Exportar
  const handleExport = async (format: "csv" | "json" | "md") => {
    try {
      const params = new URLSearchParams({ format });
      if (exportCluster && exportCluster !== "all") {
        params.append("cluster", exportCluster);
      }

      const response = await fetch(`/api/v1/dependencies/export?${params}`, {
        headers: {
          Authorization: `Bearer ${localStorage.getItem("auth_token") || "poc-token-123"}`,
        },
      });

      if (!response.ok) throw new Error("Export failed");

      const blob = await response.blob();
      const downloadUrl = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = downloadUrl;

      const ext = format === "md" ? "md" : format;
      const clusterSuffix = exportCluster && exportCluster !== "all" ? `_${exportCluster}` : "";
      a.download = `dependencies${clusterSuffix}_${new Date().toISOString().split("T")[0]}.${ext}`;

      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(downloadUrl);

      const formatLabel = format === "md" ? "Markdown" : format.toUpperCase();
      toast.success(`Exportado como ${formatLabel}`);
    } catch (error) {
      console.error("Erro ao exportar:", error);
      toast.error("Falha ao exportar dependências");
    }
  };

  // Exportar PDF usando jsPDF
  const handleExportPDF = async () => {
    try {
      // Buscar dados do registry
      const params = new URLSearchParams();
      if (exportCluster && exportCluster !== "all") {
        params.append("cluster", exportCluster);
      }

      const response = await apiClient.get(`/dependencies/registry?${params}`);
      if (!response.success) throw new Error("Failed to fetch data");

      const deps = response.data.dependencies as DependencyRecord[];
      const grouped = response.data.by_service as Record<string, DependencyRecord[]>;

      // Criar PDF
      const doc = new jsPDF({
        orientation: "portrait",
        unit: "mm",
        format: "a4",
      });

      const pageWidth = doc.internal.pageSize.getWidth();
      const clusterLabel = exportCluster && exportCluster !== "all" ? exportCluster : "Todos os Clusters";

      // Header com logo
      let yPos = await addLogoHeaderToPDF(
        doc,
        "RELATORIO DE DEPENDENCIAS EXTERNAS",
        `Cluster: ${clusterLabel} | Data: ${new Date().toLocaleDateString("pt-BR")}`
      );

      // Sumário
      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.text("Sumario", 14, yPos);
      yPos += 8;

      autoTable(doc, {
        startY: yPos,
        head: [["Metrica", "Valor"]],
        body: [
          ["Total de Dependencias", deps.length.toString()],
          ["Servicos Unicos", Object.keys(grouped).length.toString()],
          ["Cluster", clusterLabel],
        ],
        theme: "grid",
        headStyles: { fillColor: [41, 128, 185] },
        margin: { left: 14, right: 14 },
        styles: { fontSize: 10 },
      });

      yPos = (doc as jsPDF & { lastAutoTable: { finalY: number } }).lastAutoTable.finalY + 10;

      // Por tipo de serviço
      const byType: Record<string, number> = {};
      deps.forEach((dep) => {
        byType[dep.service_type] = (byType[dep.service_type] || 0) + 1;
      });

      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.text("Por Tipo de Servico", 14, yPos);
      yPos += 8;

      autoTable(doc, {
        startY: yPos,
        head: [["Tipo", "Quantidade"]],
        body: Object.entries(byType).map(([type, count]) => [
          SERVICE_TYPE_LABELS[type] || type,
          count.toString(),
        ]),
        theme: "grid",
        headStyles: { fillColor: [41, 128, 185] },
        margin: { left: 14, right: 14 },
        styles: { fontSize: 10 },
      });

      yPos = (doc as jsPDF & { lastAutoTable: { finalY: number } }).lastAutoTable.finalY + 10;

      // Detalhamento por serviço (nova página)
      doc.addPage();
      yPos = 20;

      doc.setFontSize(14);
      doc.setFont("helvetica", "bold");
      doc.text("Detalhamento por Servico", 14, yPos);
      yPos += 10;

      // Tabela com todas as dependências
      autoTable(doc, {
        startY: yPos,
        head: [["Servico", "Tipo", "Cluster", "Namespace", "Fonte"]],
        body: deps.map((dep) => [
          dep.service_name,
          dep.service_type,
          dep.cluster.replace("-admin", ""),
          dep.namespace,
          `${dep.source_type}/${dep.source_name}`,
        ]),
        theme: "striped",
        headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
        bodyStyles: { fontSize: 8 },
        margin: { left: 10, right: 10 },
        columnStyles: {
          0: { cellWidth: 45 },
          1: { cellWidth: 20 },
          2: { cellWidth: 35 },
          3: { cellWidth: 35 },
          4: { cellWidth: 45 },
        },
      });

      // Footer
      const pageCount = doc.getNumberOfPages();
      for (let i = 1; i <= pageCount; i++) {
        doc.setPage(i);
        doc.setFontSize(8);
        doc.setTextColor(128, 128, 128);
        doc.text(
          `Pagina ${i} de ${pageCount} | K8s HPA Manager`,
          pageWidth / 2,
          doc.internal.pageSize.getHeight() - 10,
          { align: "center" }
        );
      }

      // Download
      const clusterSuffix = exportCluster && exportCluster !== "all" ? `_${exportCluster}` : "";
      doc.save(`dependencies${clusterSuffix}_${new Date().toISOString().split("T")[0]}.pdf`);

      toast.success("Exportado como PDF");
    } catch (error) {
      console.error("Erro ao exportar PDF:", error);
      toast.error("Falha ao exportar PDF");
    }
  };

  // Formatar data relativa
  const formatRelativeDate = (dateStr: string) => {
    if (!dateStr) return "-";
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 60) return `${diffMins}m atrás`;
    if (diffHours < 24) return `${diffHours}h atrás`;
    return `${diffDays}d atrás`;
  };

  // Dados visíveis para exportação do painel direito
  const viewExportData = useMemo(() => {
    if (activeTab === "registry") return filteredDependencies;
    return filteredSearchResults;
  }, [activeTab, filteredDependencies, filteredSearchResults]);

  const viewExportHasData = viewExportData.length > 0;

  // Descrição dinâmica do modal de exportação
  const viewExportDescription = useMemo(() => {
    const tabLabel = activeTab === "registry" ? "Registry" : "Busca Reversa";
    const filters: string[] = [];
    if (activeTab === "registry") {
      if (filterEnv !== "all") filters.push(`Ambiente: ${filterEnv.toUpperCase()}`);
      if (filterType !== "all") filters.push(`Tipo: ${SERVICE_TYPE_LABELS[filterType] || filterType}`);
      if (searchQuery) filters.push(`Busca: "${searchQuery}"`);
    } else {
      if (filterEnvSearch !== "all") filters.push(`Ambiente: ${filterEnvSearch.toUpperCase()}`);
      if (reverseSearchQuery) filters.push(`Query: "${reverseSearchQuery}"`);
    }
    const filterStr = filters.length > 0 ? ` | ${filters.join(", ")}` : "";
    return `Aba: ${tabLabel}${filterStr} | ${viewExportData.length} registro(s)`;
  }, [activeTab, filterEnv, filterType, searchQuery, filterEnvSearch, reverseSearchQuery, viewExportData.length]);

  // Exportar dados filtrados do painel de visualização
  const handleViewExport = async () => {
    if (viewExportData.length === 0) return;
    setIsExporting(true);

    try {
      const isRegistry = activeTab === "registry";
      const titleLabel = isRegistry ? "REGISTRY DE DEPENDENCIAS" : "BUSCA REVERSA DE DEPENDENCIAS";
      const dateStr = new Date().toLocaleDateString("pt-BR");
      const filenameBase = isRegistry ? "dependencies-registry" : `dependencies-search-${reverseSearchQuery}`;
      const filename = `${filenameBase}_${new Date().toISOString().split("T")[0]}`;

      // Calcular breakdown por tipo
      const byType: Record<string, number> = {};
      const uniqueServices = new Set<string>();
      viewExportData.forEach((dep) => {
        byType[dep.service_type] = (byType[dep.service_type] || 0) + 1;
        uniqueServices.add(dep.service_name);
      });

      // Subtítulo com filtros
      const subtitleParts: string[] = [`Data: ${dateStr}`];
      if (isRegistry) {
        if (filterEnv !== "all") subtitleParts.push(`Ambiente: ${filterEnv.toUpperCase()}`);
        if (filterType !== "all") subtitleParts.push(`Tipo: ${SERVICE_TYPE_LABELS[filterType] || filterType}`);
        if (searchQuery) subtitleParts.push(`Filtro: "${searchQuery}"`);
      } else {
        if (reverseSearchQuery) subtitleParts.push(`Query: "${reverseSearchQuery}"`);
        if (filterEnvSearch !== "all") subtitleParts.push(`Ambiente: ${filterEnvSearch.toUpperCase()}`);
      }
      const subtitle = subtitleParts.join(" | ");

      if (exportFormat === "pdf") {
        const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
        const pageWidth = doc.internal.pageSize.getWidth();

        let yPos = await addLogoHeaderToPDF(doc, titleLabel, subtitle);

        // Sumário
        doc.setFontSize(14);
        doc.setFont("helvetica", "bold");
        doc.text("Sumario", 14, yPos);
        yPos += 8;

        autoTable(doc, {
          startY: yPos,
          head: [["Metrica", "Valor"]],
          body: [
            ["Total de Dependencias", viewExportData.length.toString()],
            ["Servicos Unicos", uniqueServices.size.toString()],
          ],
          theme: "grid",
          headStyles: { fillColor: [41, 128, 185] },
          margin: { left: 14, right: 14 },
          styles: { fontSize: 10 },
        });
        yPos = (doc as jsPDF & { lastAutoTable: { finalY: number } }).lastAutoTable.finalY + 10;

        // Por tipo
        doc.setFontSize(14);
        doc.setFont("helvetica", "bold");
        doc.text("Por Tipo de Servico", 14, yPos);
        yPos += 8;

        autoTable(doc, {
          startY: yPos,
          head: [["Tipo", "Quantidade"]],
          body: Object.entries(byType).map(([type, count]) => [
            SERVICE_TYPE_LABELS[type] || type,
            count.toString(),
          ]),
          theme: "grid",
          headStyles: { fillColor: [41, 128, 185] },
          margin: { left: 14, right: 14 },
          styles: { fontSize: 10 },
        });
        yPos = (doc as jsPDF & { lastAutoTable: { finalY: number } }).lastAutoTable.finalY + 10;

        // Detalhamento
        if (yPos > 220) {
          doc.addPage();
          yPos = 20;
        }
        doc.setFontSize(14);
        doc.setFont("helvetica", "bold");
        doc.text("Detalhamento", 14, yPos);
        yPos += 8;

        autoTable(doc, {
          startY: yPos,
          head: [["Servico", "Tipo", "Cluster", "Namespace", "Deployment", "Fonte"]],
          body: viewExportData.map((dep) => [
            dep.service_name,
            dep.service_type,
            dep.cluster.replace(/-admin$/, ""),
            dep.namespace,
            dep.deployment || "-",
            `${dep.source_type}/${dep.source_name}${dep.source_key ? ` [${dep.source_key}]` : ""}`,
          ]),
          theme: "striped",
          headStyles: { fillColor: [41, 128, 185], fontSize: 9 },
          bodyStyles: { fontSize: 8 },
          margin: { left: 10, right: 10 },
          columnStyles: {
            0: { cellWidth: 35 },
            1: { cellWidth: 18 },
            2: { cellWidth: 30 },
            3: { cellWidth: 25 },
            4: { cellWidth: 25 },
            5: { cellWidth: 45 },
          },
        });

        // Footer com paginação
        const pageCount = doc.getNumberOfPages();
        for (let i = 1; i <= pageCount; i++) {
          doc.setPage(i);
          doc.setFontSize(8);
          doc.setTextColor(128, 128, 128);
          doc.text(
            `Pagina ${i} de ${pageCount} | K8s HPA Manager`,
            pageWidth / 2,
            doc.internal.pageSize.getHeight() - 10,
            { align: "center" }
          );
        }

        doc.save(`${filename}.pdf`);
        toast.success("Exportado como PDF");
      } else if (exportFormat === "md") {
        let md = getMarkdownHeader(titleLabel, subtitle);
        md += "\n\n## Sumario\n\n";
        md += "| Metrica | Valor |\n|---|---|\n";
        md += `| Total de Dependencias | ${viewExportData.length} |\n`;
        md += `| Servicos Unicos | ${uniqueServices.size} |\n`;

        md += "\n## Por Tipo de Servico\n\n";
        md += "| Tipo | Quantidade |\n|---|---|\n";
        Object.entries(byType).forEach(([type, count]) => {
          md += `| ${SERVICE_TYPE_LABELS[type] || type} | ${count} |\n`;
        });

        md += "\n## Detalhamento\n\n";
        md += "| Servico | Tipo | Cluster | Namespace | Deployment | Fonte |\n";
        md += "|---|---|---|---|---|---|\n";
        viewExportData.forEach((dep) => {
          const fonte = `${dep.source_type}/${dep.source_name}${dep.source_key ? ` [${dep.source_key}]` : ""}`;
          md += `| ${dep.service_name} | ${dep.service_type} | ${dep.cluster.replace(/-admin$/, "")} | ${dep.namespace} | ${dep.deployment || "-"} | ${fonte} |\n`;
        });

        const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${filename}.md`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        toast.success("Exportado como Markdown");
      } else {
        // CSV
        const escape = (val: string) => `"${(val || "").replace(/"/g, '""')}"`;
        let csv = "Servico,Tipo,Cluster,Namespace,Deployment,Fonte_Tipo,Fonte_Nome,Fonte_Chave\n";
        viewExportData.forEach((dep) => {
          csv += [
            escape(dep.service_name),
            escape(dep.service_type),
            escape(dep.cluster.replace(/-admin$/, "")),
            escape(dep.namespace),
            escape(dep.deployment || ""),
            escape(dep.source_type),
            escape(dep.source_name),
            escape(dep.source_key || ""),
          ].join(",") + "\n";
        });

        const blob = new Blob(["\uFEFF" + csv], { type: "text/csv;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${filename}.csv`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        toast.success("Exportado como CSV");
      }

      setExportModalOpen(false);
    } catch (error) {
      console.error("Erro ao exportar:", error);
      toast.error("Falha ao exportar dados");
    } finally {
      setIsExporting(false);
    }
  };

  return (
    <>
    <SplitView
      leftPanel={{
        title: "Registry de Dependências",
        content: (
          <ScrollArea className="h-full">
            <div className="space-y-4">
              {/* Status do Registry */}
              <Card>
                <CardHeader className="pb-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <CardTitle className="text-sm font-medium">Status do Registry</CardTitle>
                      <CardDescription className="text-xs">
                        {statsData?.last_scan
                          ? `Último scan: ${formatRelativeDate(statsData.last_scan)}`
                          : "Nenhum scan realizado ainda"}
                      </CardDescription>
                    </div>
                    {autoScanRunning ? (
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        {scanProgress.current}/{scanProgress.total}
                      </div>
                    ) : (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleManualScan}
                        className="h-7 text-xs"
                      >
                        <RefreshCcw className="h-3.5 w-3.5 mr-1" />
                        Atualizar
                      </Button>
                    )}
                  </div>
                </CardHeader>
                <CardContent className="space-y-2 text-xs">
                  {statsData ? (
                    <>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Serviços únicos:</span>
                        <span className="font-medium">{statsData.unique_services}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Total de referências:</span>
                        <span className="font-medium">{statsData.total_dependencies}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Clusters:</span>
                        <span className="font-medium">{statsData.unique_clusters}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Namespaces:</span>
                        <span className="font-medium">{statsData.unique_namespaces}</span>
                      </div>

                      {/* Breakdown por tipo */}
                      {statsData.by_type && Object.keys(statsData.by_type).length > 0 && (
                        <div className="pt-2 border-t mt-2">
                          <span className="text-muted-foreground">Por tipo:</span>
                          <div className="mt-1 space-y-1">
                            {Object.entries(statsData.by_type).map(([type, count]) => (
                              <div key={type} className="flex items-center justify-between">
                                <Badge
                                  variant="outline"
                                  className={`text-[10px] ${SERVICE_TYPE_COLORS[type] || SERVICE_TYPE_COLORS.other}`}
                                >
                                  {SERVICE_TYPE_LABELS[type] || type}
                                </Badge>
                                <span className="font-medium">{count}</span>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="text-center py-4 text-muted-foreground">
                      {registryLoading ? (
                        <>
                          <Loader2 className="h-4 w-4 animate-spin inline mr-2" />
                          Carregando...
                        </>
                      ) : (
                        <>
                          <AlertCircle className="h-4 w-4 inline mr-2" />
                          Nenhum dado disponível
                        </>
                      )}
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* Ações de Export */}
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-sm font-medium">Exportar</CardTitle>
                  <CardDescription className="text-xs">
                    Selecione o cluster e o formato
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-3">
                  {/* Seletor de Cluster */}
                  <Select value={exportCluster} onValueChange={setExportCluster}>
                    <SelectTrigger className="h-8 text-xs">
                      <SelectValue placeholder="Selecione o cluster" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">Todos os Clusters</SelectItem>
                      {registryClusters?.map((cluster) => (
                        <SelectItem key={cluster} value={cluster}>
                          {cluster}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  {/* Botões de Formato */}
                  <div className="grid grid-cols-2 gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      onClick={() => handleExport("csv")}
                      disabled={!statsData?.total_dependencies}
                    >
                      <FileSpreadsheet className="mr-1 h-3 w-3" />
                      CSV
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      onClick={() => handleExport("json")}
                      disabled={!statsData?.total_dependencies}
                    >
                      <FileJson className="mr-1 h-3 w-3" />
                      JSON
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      onClick={() => handleExport("md")}
                      disabled={!statsData?.total_dependencies}
                    >
                      <FileText className="mr-1 h-3 w-3" />
                      Markdown
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      onClick={handleExportPDF}
                      disabled={!statsData?.total_dependencies}
                    >
                      <FileType className="mr-1 h-3 w-3" />
                      PDF
                    </Button>
                  </div>
                </CardContent>
              </Card>

              {/* Info sobre Auto-Scan */}
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-sm font-medium flex items-center gap-2">
                    <Clock className="h-4 w-4" />
                    Auto-Scan
                  </CardTitle>
                </CardHeader>
                <CardContent className="text-xs text-muted-foreground">
                  <p>O sistema escaneia automaticamente todos os clusters a cada 24 horas.</p>
                  <p className="mt-1">Os dados são persistidos no SQLite para buscas rápidas.</p>
                </CardContent>
              </Card>
            </div>
          </ScrollArea>
        ),
      }}
      rightPanel={{
        title: "",
        content: (
          <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as "registry" | "search")} className="h-full flex flex-col">
            {/* Tabs Header */}
            <div className="flex items-center justify-between px-4 pt-2 pb-0 border-b">
              <TabsList className="h-9">
                <TabsTrigger value="registry" className="text-xs gap-1.5">
                  <Layers className="h-3.5 w-3.5" />
                  Registry
                  {statsData && (
                    <Badge variant="secondary" className="ml-1 h-4 px-1 text-[10px]">
                      {statsData.unique_services}
                    </Badge>
                  )}
                </TabsTrigger>
                <TabsTrigger value="search" className="text-xs gap-1.5">
                  <Search className="h-3.5 w-3.5" />
                  Busca Reversa
                </TabsTrigger>
              </TabsList>

              {/* Botão exportar + Filtros */}
              <div className="flex items-center gap-2">
                {viewExportHasData && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs gap-1"
                    onClick={() => setExportModalOpen(true)}
                    title="Exportar dados filtrados"
                  >
                    <Download className="h-3.5 w-3.5" />
                    Exportar
                  </Button>
                )}

              {/* Filtros (apenas na aba registry) */}
              {activeTab === "registry" && registryData && (
                <>
                  <Input
                    placeholder="Filtrar..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="h-7 w-48 text-xs"
                  />
                  {searchQuery && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0"
                      onClick={() => setSearchQuery("")}
                    >
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  )}
                  <Select value={filterEnv} onValueChange={setFilterEnv}>
                    <SelectTrigger className="h-7 w-32 text-xs">
                      <SelectValue placeholder="Ambiente" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">Todos ambientes</SelectItem>
                      {availableEnvs.map((env) => (
                        <SelectItem key={env} value={env}>
                          {env.toUpperCase()}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Select value={filterType} onValueChange={setFilterType}>
                    <SelectTrigger className="h-7 w-32 text-xs">
                      <SelectValue placeholder="Tipo" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">Todos os tipos</SelectItem>
                      {Object.entries(SERVICE_TYPE_LABELS).map(([key, label]) => (
                        <SelectItem key={key} value={key}>
                          {label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </>
              )}
              </div>
            </div>

            {/* Tab: Registry */}
            <TabsContent value="registry" className="flex-1 m-0 overflow-hidden">
              <ScrollArea className="h-full">
                {registryLoading ? (
                  <div className="flex flex-col items-center justify-center h-96 text-muted-foreground">
                    <Loader2 className="h-12 w-12 mb-4 animate-spin opacity-50" />
                    <p className="text-sm">Carregando registry...</p>
                  </div>
                ) : !registryData || filteredDependencies.length === 0 ? (
                  <div className="flex flex-col items-center justify-center h-96 text-muted-foreground">
                    <Database className="h-12 w-12 mb-4 opacity-50" />
                    <p className="text-sm">
                      {registryData ? "Nenhuma dependência encontrada" : "Registry vazio"}
                    </p>
                    <p className="text-xs mt-1">
                      {registryData
                        ? "Tente ajustar os filtros"
                        : "Clique em 'Atualizar' para escanear os clusters"}
                    </p>
                  </div>
                ) : (
                  <div className="p-4">
                    {/* Tabela agrupada por serviço */}
                    <div className="space-y-4">
                      {Object.entries(groupedByService).map(([serviceName, deps]) => (
                        <Card key={serviceName}>
                          <CardHeader className="pb-2">
                            <div className="flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <Database className="h-4 w-4 text-muted-foreground" />
                                <CardTitle className="text-sm font-mono">
                                  {serviceName}
                                </CardTitle>
                                <Badge
                                  variant="outline"
                                  className={`text-[10px] ${SERVICE_TYPE_COLORS[deps[0].service_type] || SERVICE_TYPE_COLORS.other}`}
                                >
                                  {SERVICE_TYPE_LABELS[deps[0].service_type] || deps[0].service_type}
                                </Badge>
                              </div>
                              <Badge variant="secondary" className="text-xs">
                                {deps.length} uso(s)
                              </Badge>
                            </div>
                          </CardHeader>
                          <CardContent>
                            <Table>
                              <TableHeader>
                                <TableRow>
                                  <TableHead className="text-xs">Cluster</TableHead>
                                  <TableHead className="text-xs">Namespace</TableHead>
                                  <TableHead className="text-xs">Deployment</TableHead>
                                  <TableHead className="text-xs">Tópico/Fila</TableHead>
                                  <TableHead className="text-xs">Fonte</TableHead>
                                  <TableHead className="text-xs">Visto</TableHead>
                                </TableRow>
                              </TableHeader>
                              <TableBody>
                                {deps.map((dep) => (
                                  <TableRow key={`${dep.cluster}-${dep.namespace}-${dep.source_name}-${dep.id}`}>
                                    <TableCell className="text-xs font-mono">
                                      {dep.cluster.replace(/-admin$/, "")}
                                    </TableCell>
                                    <TableCell className="text-xs">{dep.namespace}</TableCell>
                                    <TableCell className="text-xs">
                                      {dep.deployment || "-"}
                                    </TableCell>
                                    <TableCell className="text-xs">
                                      {dep.topic_name ? (
                                        <Badge variant="secondary" className="font-mono text-[10px]">
                                          {dep.topic_name}
                                        </Badge>
                                      ) : (
                                        <span className="text-muted-foreground">-</span>
                                      )}
                                    </TableCell>
                                    <TableCell className="text-xs">
                                      <span className="text-muted-foreground">
                                        {dep.source_type}: {dep.source_name}
                                      </span>
                                      {dep.source_key && (
                                        <span className="text-muted-foreground ml-1">
                                          [{dep.source_key}]
                                        </span>
                                      )}
                                    </TableCell>
                                    <TableCell className="text-xs text-muted-foreground">
                                      {formatRelativeDate(dep.last_seen)}
                                    </TableCell>
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          </CardContent>
                        </Card>
                      ))}
                    </div>
                  </div>
                )}
              </ScrollArea>
            </TabsContent>

            {/* Tab: Busca Reversa */}
            <TabsContent value="search" className="flex-1 m-0 overflow-hidden">
              <div className="p-4 space-y-4 h-full flex flex-col">
                {/* Input de busca */}
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">Busca por Serviço</CardTitle>
                    <CardDescription className="text-xs">
                      Digite o nome do serviço para encontrar onde ele é usado. Use * como coringa (ex: rds*, *kafka*, evh-*)
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="flex gap-2">
                      <Input
                        placeholder="Ex: rdsh-regional01, rds*, *kafka*, evh-mensagens..."
                        value={reverseSearchQuery}
                        onChange={(e) => setReverseSearchQuery(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && handleReverseSearch()}
                        className="flex-1"
                      />
                      <Select value={filterEnvSearch} onValueChange={setFilterEnvSearch}>
                        <SelectTrigger className="w-[130px]">
                          <SelectValue placeholder="Ambiente" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="all">Todos Ambientes</SelectItem>
                          {availableEnvsSearch.map((env) => (
                            <SelectItem key={env} value={env}>
                              {env.toUpperCase()}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Button onClick={handleReverseSearch} disabled={isSearching}>
                        {isSearching ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          <Search className="h-4 w-4" />
                        )}
                      </Button>
                    </div>
                    <p className="text-xs text-muted-foreground mt-2">
                      Busca no registry local (SQLite). Use * como coringa: rds* (inicia com), *kafka* (contém), evh-prd* (prefixo)
                    </p>
                  </CardContent>
                </Card>

                {/* Resultados da busca */}
                <ScrollArea className="flex-1">
                  {!searchResults ? (
                    <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                      <Search className="h-12 w-12 mb-4 opacity-50" />
                      <p className="text-sm">Nenhuma busca realizada</p>
                      <p className="text-xs mt-1">
                        Digite o nome de um serviço e clique em buscar
                      </p>
                    </div>
                  ) : searchResults.total_found === 0 ? (
                    <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                      <AlertCircle className="h-12 w-12 mb-4 opacity-50" />
                      <p className="text-sm">Nenhum uso encontrado</p>
                      <p className="text-xs mt-1">
                        O serviço "{reverseSearchQuery}" não foi encontrado no registry
                      </p>
                    </div>
                  ) : (
                    <div className="space-y-2">
                      <div className="text-xs text-muted-foreground mb-2">
                        {filteredSearchResults.length} resultado(s){filterEnvSearch !== "all" ? ` (filtro: ${filterEnvSearch.toUpperCase()})` : ""} encontrado(s) para "{reverseSearchQuery}"
                      </div>
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead className="text-xs">Serviço</TableHead>
                            <TableHead className="text-xs">Tópico/Fila</TableHead>
                            <TableHead className="text-xs">Cluster</TableHead>
                            <TableHead className="text-xs">Namespace</TableHead>
                            <TableHead className="text-xs">Deployment</TableHead>
                            <TableHead className="text-xs">Fonte</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {filteredSearchResults.map((dep) => (
                            <TableRow key={`${dep.cluster}-${dep.namespace}-${dep.source_name}-${dep.id}`}>
                              <TableCell className="text-xs font-mono">
                                <div className="flex items-center gap-1">
                                  <Badge
                                    variant="outline"
                                    className={`text-[9px] ${SERVICE_TYPE_COLORS[dep.service_type] || SERVICE_TYPE_COLORS.other}`}
                                  >
                                    {dep.service_type}
                                  </Badge>
                                  {dep.service_name}
                                </div>
                              </TableCell>
                              <TableCell className="text-xs">
                                {dep.topic_name ? (
                                  <Badge variant="secondary" className="font-mono text-[10px]">
                                    {dep.topic_name}
                                  </Badge>
                                ) : (
                                  <span className="text-muted-foreground">-</span>
                                )}
                              </TableCell>
                              <TableCell className="text-xs">
                                {dep.cluster.replace(/-admin$/, "")}
                              </TableCell>
                              <TableCell className="text-xs">{dep.namespace}</TableCell>
                              <TableCell className="text-xs">{dep.deployment || "-"}</TableCell>
                              <TableCell className="text-xs text-muted-foreground">
                                {dep.source_type}: {dep.source_name}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  )}
                </ScrollArea>
              </div>
            </TabsContent>
          </Tabs>
        ),
      }}
    />

    {/* Modal de Exportação do Painel de Visualização */}
    <Dialog open={exportModalOpen} onOpenChange={setExportModalOpen}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Exportar Dependencias</DialogTitle>
          <DialogDescription className="text-xs">
            {viewExportDescription}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4">
          <RadioGroup value={exportFormat} onValueChange={(v) => setExportFormat(v as "pdf" | "md" | "csv")}>
            <div className="flex items-center space-x-2 mb-3">
              <RadioGroupItem value="pdf" id="fmt-pdf" />
              <Label htmlFor="fmt-pdf" className="text-sm cursor-pointer">
                <span className="font-medium">PDF</span>
                <span className="text-xs text-muted-foreground ml-2">com logo e tabelas formatadas</span>
              </Label>
            </div>
            <div className="flex items-center space-x-2 mb-3">
              <RadioGroupItem value="md" id="fmt-md" />
              <Label htmlFor="fmt-md" className="text-sm cursor-pointer">
                <span className="font-medium">Markdown</span>
                <span className="text-xs text-muted-foreground ml-2">com header e tabelas</span>
              </Label>
            </div>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="csv" id="fmt-csv" />
              <Label htmlFor="fmt-csv" className="text-sm cursor-pointer">
                <span className="font-medium">CSV</span>
                <span className="text-xs text-muted-foreground ml-2">para Excel e ferramentas de BI</span>
              </Label>
            </div>
          </RadioGroup>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => setExportModalOpen(false)}>
            Cancelar
          </Button>
          <Button size="sm" onClick={handleViewExport} disabled={isExporting}>
            {isExporting ? (
              <>
                <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />
                Exportando...
              </>
            ) : (
              <>
                <Download className="h-3.5 w-3.5 mr-1" />
                Exportar
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    </>
  );
};
