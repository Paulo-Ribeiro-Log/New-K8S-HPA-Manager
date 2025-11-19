import { useMemo } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Download, FileText, FileSpreadsheet, FileImage } from "lucide-react";
import type { HPA } from "@/lib/api/types";
import { toast } from "sonner";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";

// Extend jsPDF type to include autoTable
declare module "jspdf" {
  interface jsPDF {
    lastAutoTable: {
      finalY: number;
    };
  }
}

interface HPAExportButtonProps {
  hpas: HPA[];
}

export const HPAExportButton = ({ hpas }: HPAExportButtonProps) => {
  // Agrupar HPAs por namespace (mesma lógica do HPATableView)
  const hpasByNamespace = useMemo(() => {
    const grouped: Record<string, HPA[]> = {};

    hpas.forEach((hpa) => {
      if (!grouped[hpa.namespace]) {
        grouped[hpa.namespace] = [];
      }
      grouped[hpa.namespace].push(hpa);
    });

    return Object.keys(grouped)
      .sort()
      .map((namespace) => ({
        namespace,
        hpas: grouped[namespace].sort((a, b) => a.name.localeCompare(b.name)),
      }));
  }, [hpas]);

  const exportToCSV = () => {
    if (hpas.length === 0) {
      toast.error("Nenhum HPA para exportar");
      return;
    }

    let csvContent = "";

    hpasByNamespace.forEach(({ namespace, hpas: namespaceHPAs }) => {
      // Cabeçalho do namespace
      csvContent += `\nNamespace: ${namespace}\n`;

      // Cabeçalho da tabela
      csvContent += "Nome do HPA,Versão,Min Replicas,Max Replicas,Replicas,CPU Target (%),Memory Target (%)\n";

      // Dados
      namespaceHPAs.forEach((hpa) => {
        const row = [
          hpa.name,
          hpa.image_version || "-",
          hpa.min_replicas ?? 0,
          hpa.max_replicas ?? 1,
          hpa.current_replicas ?? 0,
          hpa.target_cpu !== null && hpa.target_cpu !== undefined ? `${hpa.target_cpu}%` : "-",
          hpa.target_memory !== null && hpa.target_memory !== undefined ? `${hpa.target_memory}%` : "-",
        ];
        csvContent += row.join(",") + "\n";
      });
    });

    // Download
    const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `hpa-export-${new Date().toISOString().split("T")[0]}.csv`;
    link.click();
    URL.revokeObjectURL(url);

    toast.success(`CSV exportado: ${hpas.length} HPAs em ${hpasByNamespace.length} namespaces`);
  };

  const exportToMarkdown = () => {
    if (hpas.length === 0) {
      toast.error("Nenhum HPA para exportar");
      return;
    }

    let mdContent = "# HPA Export Report\n\n";
    mdContent += `**Data de exportação:** ${new Date().toLocaleString("pt-BR")}\n\n`;
    mdContent += `**Total de HPAs:** ${hpas.length}\n`;
    mdContent += `**Namespaces:** ${hpasByNamespace.length}\n\n`;
    mdContent += "---\n\n";

    hpasByNamespace.forEach(({ namespace, hpas: namespaceHPAs }) => {
      mdContent += `## Namespace: ${namespace}\n\n`;

      // Tabela em Markdown
      mdContent += "| Nome do HPA | Versão | Min Replicas | Max Replicas | Replicas | CPU Target (%) | Memory Target (%) |\n";
      mdContent += "|-------------|--------|--------------|--------------|----------|----------------|-------------------|\n";

      namespaceHPAs.forEach((hpa) => {
        mdContent += `| ${hpa.name} | ${hpa.image_version || "-"} | ${hpa.min_replicas ?? 0} | ${hpa.max_replicas ?? 1} | ${hpa.current_replicas ?? 0} | ${
          hpa.target_cpu !== null && hpa.target_cpu !== undefined ? `${hpa.target_cpu}%` : "-"
        } | ${hpa.target_memory !== null && hpa.target_memory !== undefined ? `${hpa.target_memory}%` : "-"} |\n`;
      });

      mdContent += "\n";
    });

    // Download
    const blob = new Blob([mdContent], { type: "text/markdown;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `hpa-export-${new Date().toISOString().split("T")[0]}.md`;
    link.click();
    URL.revokeObjectURL(url);

    toast.success(`Markdown exportado: ${hpas.length} HPAs em ${hpasByNamespace.length} namespaces`);
  };

  const exportToPDF = () => {
    if (hpas.length === 0) {
      toast.error("Nenhum HPA para exportar");
      return;
    }

    const doc = new jsPDF();

    // Título
    doc.setFontSize(18);
    doc.setFont("helvetica", "bold");
    doc.text("HPA Export Report", 14, 20);

    // Informações gerais
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    doc.text(`Data: ${new Date().toLocaleString("pt-BR")}`, 14, 30);
    doc.text(`Total de HPAs: ${hpas.length}`, 14, 36);
    doc.text(`Namespaces: ${hpasByNamespace.length}`, 14, 42);

    let yPosition = 50;

    hpasByNamespace.forEach(({ namespace, hpas: namespaceHPAs }, index) => {
      // Adicionar nova página se necessário
      if (yPosition > 250) {
        doc.addPage();
        yPosition = 20;
      }

      // Namespace header
      doc.setFontSize(12);
      doc.setFont("helvetica", "bold");
      doc.text(`Namespace: ${namespace}`, 14, yPosition);
      yPosition += 8;

      // Tabela
      const tableData = namespaceHPAs.map((hpa) => [
        hpa.name,
        hpa.image_version || "-",
        String(hpa.min_replicas ?? 0),
        String(hpa.max_replicas ?? 1),
        String(hpa.current_replicas ?? 0),
        hpa.target_cpu !== null && hpa.target_cpu !== undefined ? `${hpa.target_cpu}%` : "-",
        hpa.target_memory !== null && hpa.target_memory !== undefined ? `${hpa.target_memory}%` : "-",
      ]);

      autoTable(doc, {
        startY: yPosition,
        head: [["Nome do HPA", "Versão", "Min", "Max", "Replicas", "CPU %", "Memory %"]],
        body: tableData,
        theme: "striped",
        headStyles: { fillColor: [59, 130, 246], fontStyle: "bold" },
        styles: { fontSize: 8 },
        columnStyles: {
          0: { cellWidth: 50 },
          1: { cellWidth: 30 },
          2: { cellWidth: 15, halign: "center" },
          3: { cellWidth: 15, halign: "center" },
          4: { cellWidth: 20, halign: "center" },
          5: { cellWidth: 20, halign: "center" },
          6: { cellWidth: 25, halign: "center" },
        },
      });

      // Atualizar yPosition para próximo namespace
      yPosition = doc.lastAutoTable.finalY + 12;
    });

    // Download
    doc.save(`hpa-export-${new Date().toISOString().split("T")[0]}.pdf`);

    toast.success(`PDF exportado: ${hpas.length} HPAs em ${hpasByNamespace.length} namespaces`);
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="gap-2">
          <Download className="w-4 h-4" />
          Exportar
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={exportToCSV}>
          <FileSpreadsheet className="w-4 h-4 mr-2" />
          Exportar como CSV
        </DropdownMenuItem>
        <DropdownMenuItem onClick={exportToMarkdown}>
          <FileText className="w-4 h-4 mr-2" />
          Exportar como Markdown
        </DropdownMenuItem>
        <DropdownMenuItem onClick={exportToPDF}>
          <FileImage className="w-4 h-4 mr-2" />
          Exportar como PDF
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
