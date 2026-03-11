import { useEffect, useMemo, useState, useCallback, useRef } from "react";
import { SplitView } from "@/components/SplitView";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Search, RefreshCcw, Eye, EyeOff, CheckCircle2, TriangleAlert, ChevronDown, ChevronRight, PanelLeftClose, PanelLeftOpen, FileDiff, Loader2, Undo2, Redo2, Maximize2, Minimize2, Lock, Unlock, X, FileText, Plus, MoreVertical, Trash2, SplitSquareHorizontal, AlertCircle, Copy, Shield } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import * as yaml from "js-yaml";

import type {
  Namespace,
  SecretSummary,
  SecretManifest,
} from "@/lib/api/types";
import { useSecrets } from "@/hooks/useAPI";
import { useCertificates } from "@/hooks/useCertificates";
import { apiClient } from "@/lib/api/client";
import { CertificateDetailModal } from "@/components/CertificateDetailModal";
import type { CertificateInfo } from "@/types/certificates";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import { html as diff2html } from "diff2html";
import "diff2html/bundles/css/diff2html.min.css";
import "@/styles/diff2html-dark.css";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ProtectedAction } from "@/components/rbac";
import { CreateSecretModal } from "@/components/CreateSecretModal";
import { usePersistedTabState } from "@/hooks/usePersistedTabState";

interface SecretsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
  onOpenCompare?: (initial: { type: "secret"; namespace: string; name: string }) => void;
}

export const SecretsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
  onOpenCompare,
}: SecretsTabProps) => {

  // ✅ Estados com persistência entre trocas de aba
  const [searchQuery, setSearchQuery] = usePersistedTabState<string>('secrets', 'searchQuery', "");
  const [selectedSecret, setSelectedSecret] = usePersistedTabState<SecretSummary | null>('secrets', 'selectedSecret', null);
  const [showLabels, setShowLabels] = usePersistedTabState<boolean>('secrets', 'showLabels', false);
  const [isSidebarCollapsed, setIsSidebarCollapsed] = usePersistedTabState<boolean>('secrets', 'isSidebarCollapsed', false);
  const [viewMode, setViewMode] = usePersistedTabState<"editor" | "diff">('secrets', 'viewMode', "editor");
  const [isDecoded, setIsDecoded] = usePersistedTabState<boolean>('secrets', 'isDecoded', false);

  // Estados locais (não persistidos)
  const [manifest, setManifest] = useState<SecretManifest | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [editorValue, setEditorValue] = useState("");
  const [originalYaml, setOriginalYaml] = useState("");
  const [isValidating, setIsValidating] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const [diffModalOpen, setDiffModalOpen] = useState(false);
  const [diffHtml, setDiffHtml] = useState("");
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffFullScreen, setDiffFullScreen] = useState(false);
  const [applyConfirmOpen, setApplyConfirmOpen] = useState(false);
  const [editorFullScreen, setEditorFullScreen] = useState(false);
  const [describeModalOpen, setDescribeModalOpen] = useState(false);
  const [describeContent, setDescribeContent] = useState("");
  const [describeLoading, setDescribeLoading] = useState(false);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // Visualização do certificado TLS (quando type === "kubernetes.io/tls")
  const [tlsCertModalOpen, setTlsCertModalOpen] = useState(false);
  const [tlsCertInfo, setTlsCertInfo] = useState<CertificateInfo | null>(null);
  const [tlsCertLoading, setTlsCertLoading] = useState(false);

  // Mapa de validade TLS para indicador na lista (apenas expirando/expirado, atualizado a cada 1h)
  const [tlsCertMap, setTlsCertMap] = useState<Map<string, CertificateInfo>>(new Map());

  // Error Dialog para exibir erros de apply de forma mais proeminente
  const [errorDialogOpen, setErrorDialogOpen] = useState(false);
  const [errorTitle, setErrorTitle] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  // Undo/Redo history with persistent cache
  const historyCache = useRef<Map<string, { history: string[], index: number }>>(new Map());
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);

  const filteredNamespaces = useMemo(() => {
    if (!namespaces || !Array.isArray(namespaces)) return [];
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  useEffect(() => {
    if (!selectedNamespace) return;
    const exists = filteredNamespaces.some((ns) => ns.name === selectedNamespace);
    if (!exists) {
      onNamespaceChange("");
    }
  }, [filteredNamespaces, onNamespaceChange, selectedNamespace]);

  const namespaceFilter = selectedNamespace ? [selectedNamespace] : undefined;
  const { secrets, loading, error, refetch } = useSecrets(
    cluster,
    namespaceFilter,
    showSystemNamespaces
  );

  const { getCertificateDetails, scanCertificates } = useCertificates();

  // Scan TLS ao montar e a cada 1h — popula o mapa de validade para indicadores na lista
  useEffect(() => {
    if (!cluster) return;
    const run = () => {
      scanCertificates({ clusters: [cluster] })
        .then((result) => {
          const map = new Map<string, CertificateInfo>();
          result.certificates.forEach((cert) => {
            if (cert.status === "expiring" || cert.status === "expired") {
              map.set(`${cert.namespace}/${cert.secretName}`, cert);
            }
          });
          setTlsCertMap(map);
        })
        .catch(() => {});
    };
    run();
    const id = setInterval(run, 60 * 60 * 1000); // 1 hora
    return () => clearInterval(id);
  }, [cluster]);

  const handleViewTlsCert = useCallback(async () => {
    if (!selectedSecret) return;
    setTlsCertLoading(true);
    try {
      const info = await getCertificateDetails(selectedSecret.cluster, selectedSecret.namespace, selectedSecret.name);
      setTlsCertInfo(info);
      setTlsCertModalOpen(true);
    } catch {
      toast.error("Erro ao obter dados do certificado TLS");
    } finally {
      setTlsCertLoading(false);
    }
  }, [selectedSecret, getCertificateDetails]);

  useEffect(() => {
    if (error) {
      toast.error("Erro ao carregar Secrets", {
        description: error,
      });
    }
  }, [error]);

  useEffect(() => {
    setSelectedSecret(null);
    setManifest(null);
    setEditorValue("");
    setOriginalYaml("");
    setShowLabels(false);
    setViewMode("editor");
    setHistory([]);
    setHistoryIndex(-1);
    setIsDecoded(false);
  }, [cluster, selectedNamespace]);

  const filteredSecrets = useMemo(() => {
    let filtered = secrets;
    
    // Filtrar secrets de sistema quando Sistema está desligado
    if (!showSystemNamespaces) {
      filtered = filtered.filter((secret) => 
        !secret.name.includes("sh.helm.release.") && 
        !secret.name.startsWith("default-") &&
        !secret.name.includes("alertmanager") &&
        !secret.name.includes("prometheus")
      );
    }
    
    // Aplicar busca por query
    if (!searchQuery) return filtered;
    const query = searchQuery.toLowerCase();
    return filtered.filter((cm) => {
      return (
        cm.name.toLowerCase().includes(query) ||
        cm.namespace.toLowerCase().includes(query) ||
        Object.entries(cm.labels || {}).some(([key, value]) =>
          `${key}=${value}`.toLowerCase().includes(query)
        )
      );
    });
  }, [secrets, searchQuery, showSystemNamespaces]);

  // ===== Funções Utilitárias para Base64 =====

  // Função para validar se uma string é base64 válida
  const isValidBase64 = (str: string): boolean => {
    if (!str || str.trim() === '') return false;
    // Regex para validar base64: só permite caracteres A-Z, a-z, 0-9, +, /, = (padding)
    const base64Regex = /^[A-Za-z0-9+/]*={0,2}$/;
    if (!base64Regex.test(str)) return false;

    try {
      // Tentar decodificar - se falhar, não é base64 válido
      const decoded = atob(str);
      // Re-encodar e comparar para verificar se é base64 legítimo
      return btoa(decoded) === str;
    } catch {
      return false;
    }
  };

  // Funções para encode/decode UTF-8 seguro
  const base64EncodeUtf8 = (str: string): string => {
    // Converter string para UTF-8 bytes e depois para base64
    return btoa(encodeURIComponent(str).replace(/%([0-9A-F]{2})/g, (match, p1) => {
      return String.fromCharCode(parseInt(p1, 16));
    }));
  };

  const base64DecodeUtf8 = (str: string): string => {
    // Decodificar base64 e depois converter UTF-8 bytes para string
    return decodeURIComponent(Array.prototype.map.call(atob(str), (c: string) => {
      return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
    }).join(''));
  };

  const handleSelectSecret = async (summary: SecretSummary) => {
    // Salvar histórico atual no cache antes de trocar
    if (selectedSecret && history.length > 0) {
      const cacheKey = `${selectedSecret.namespace}/${selectedSecret.name}`;
      historyCache.current.set(cacheKey, { history: [...history], index: historyIndex });
    }

    setSelectedSecret(summary);
    setManifestLoading(true);
    setManifest(null);

    try {
      const detail = await apiClient.getSecret(
        summary.cluster,
        summary.namespace,
        summary.name
      );
      setManifest(detail);
      const initialYaml = detail.yaml || "";
      setEditorValue(initialYaml);
      setOriginalYaml(initialYaml);
      setShowLabels(false);
      setViewMode("editor");
      
      // Restaurar histórico do cache se existir
      const cacheKey = `${summary.namespace}/${summary.name}`;
      const cached = historyCache.current.get(cacheKey);
      if (cached) {
        setHistory(cached.history);
        setHistoryIndex(cached.index);
        // Atualizar editor com valor do histórico atual
        if (cached.index >= 0 && cached.index < cached.history.length) {
          setEditorValue(cached.history[cached.index]);
        }
      } else {
        // Inicializar histórico com valor inicial
        setHistory([initialYaml]);
        setHistoryIndex(0);
      }
    } catch (err) {
      toast.error("Erro ao carregar manifesto", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  // Atualizar histórico quando o editor muda (com debounce)
  const handleEditorChange = useCallback((value: string) => {
    setEditorValue(value);
  }, []);

  // Adicionar ao histórico manualmente quando necessário
  const addToHistory = useCallback((value: string) => {
    setHistoryIndex((currentIndex) => {
      setHistory((prev) => {
        // Remover itens futuros se estamos no meio do histórico
        const newHistory = prev.slice(0, currentIndex + 1);
        
        // Só adicionar se for diferente do último item
        if (newHistory.length === 0 || newHistory[newHistory.length - 1] !== value) {
          newHistory.push(value);
          // Limitar histórico a 50 itens
          if (newHistory.length > 50) {
            newHistory.shift();
            return newHistory;
          }
        }
        return newHistory;
      });
      return Math.min(currentIndex + 1, 49);
    });
  }, []);

  // Undo
  const handleUndo = useCallback(() => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  // Redo
  const handleRedo = useCallback(() => {
    if (historyIndex < history.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditorValue(history[newIndex]);
    }
  }, [history, historyIndex]);

  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;

  const refreshSecrets = () => {
    if (!cluster) return;
    refetch();
  };

  const refreshManifest = async () => {
    if (!selectedSecret) return;
    
    setManifestLoading(true);
    try {
      // Buscar secret atualizado do servidor
      const detail = await apiClient.getSecret(
        selectedSecret.cluster,
        selectedSecret.namespace,
        selectedSecret.name
      );
      setManifest(detail);
      const freshYaml = detail.yaml || "";
      
      // Atualizar com YAML fresco do servidor (ignorar cache)
      setOriginalYaml(freshYaml);
      setEditorValue(freshYaml);
      
      // Resetar histórico com o novo YAML
      setHistory([freshYaml]);
      setHistoryIndex(0);
      
      // Resetar estado de decode
      setIsDecoded(false);
      
      toast.success("Manifest recarregado do servidor");
    } catch (err) {
      toast.error("Falha ao recarregar", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setManifestLoading(false);
    }
  };

  const handleToggleView = (mode: "editor" | "diff") => {
    if (mode === "diff" && !hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setViewMode(mode);
  };

  const handleToggleDecode = () => {
    // Guard: Verificar se há um Secret selecionado e se o editor tem conteúdo
    if (!selectedSecret) {
      toast.warning("Selecione um Secret primeiro");
      return;
    }

    if (!editorValue || editorValue.trim() === '') {
      toast.warning("Editor vazio - carregue um Secret primeiro");
      return;
    }

    // Função para detectar se é um certificado ou chave que não deve ser processado
    const isCertificateOrKey = (key: string, value: string): boolean => {
      // Verificar se o nome da chave sugere certificado ou chave
      const keyLower = key.toLowerCase();
      const certKeyPatterns = ['.crt', '.cert', '.pem', '.key', 'tls.crt', 'tls.key', 'ca.crt', 'ca.key', 'certificate', 'private', 'public'];
      
      if (certKeyPatterns.some(pattern => keyLower.includes(pattern))) {
        return true;
      }

      // Se o valor for muito longo (certificados geralmente são longos)
      // e for base64 válido, verificar se contém marcadores de certificado
      if (value.length > 500 && isValidBase64(value)) {
        try {
          const decoded = atob(value);
          // Verificar se contém marcadores típicos de certificados/chaves
          const certMarkers = [
            '-----BEGIN CERTIFICATE-----',
            '-----BEGIN PRIVATE KEY-----',
            '-----BEGIN RSA PRIVATE KEY-----',
            '-----BEGIN PUBLIC KEY-----',
            '-----BEGIN EC PRIVATE KEY-----',
            'BEGIN CERTIFICATE',
            'BEGIN PRIVATE KEY',
            'BEGIN RSA PRIVATE KEY',
            'subject=',
            'issuer=',
          ];
          return certMarkers.some(marker => decoded.includes(marker));
        } catch {
          return false;
        }
      }
      
      return false;
    };

    try {
      const yamlObj = yaml.load(editorValue) as any;

      if (!yamlObj || !yamlObj.data) {
        toast.error("YAML inválido ou sem seção 'data'");
        return;
      }

      // Preservar formatação original alterando apenas os valores
      let newYaml = editorValue;
      let processedCount = 0;
      let skippedCount = 0;

      if (isDecoded) {
        // RE-ENCODE: texto plano → Base64
        // Processar linha por linha para preservar valores exatos
        const lines = newYaml.split('\n');
        for (let i = 0; i < lines.length; i++) {
          const line = lines[i];
          // Verificar se é uma linha de data: com valor decodificado
          const match = line.match(/^(\s+)([^:]+):\s+(.+)$/);
          if (match) {
            const [, indent, key, value] = match;
            // Verificar se essa key existe no yamlObj.data
            if (yamlObj.data && key in yamlObj.data) {
              // Pegar o valor atual da linha (pode ser texto plano)
              const currentValue = value.trim();
              
              // Ignorar certificados e chaves
              if (isCertificateOrKey(key, currentValue)) {
                skippedCount++;
                continue;
              }
              
              // Verificar se já é base64 - se for, não encodificar novamente
              if (isValidBase64(currentValue)) {
                // Já está em base64, ignorar
                skippedCount++;
              } else {
                try {
                  // Re-encodificar para Base64 apenas se não for base64 (com suporte UTF-8)
                  const encoded = base64EncodeUtf8(currentValue);
                  lines[i] = `${indent}${key}: ${encoded}`;
                  processedCount++;
                } catch (err) {
                  // Se falhar, manter original
                  console.error(`Erro ao encodificar ${key}:`, err);
                  skippedCount++;
                }
              }
            }
          }
        }
        newYaml = lines.join('\n');
        
        if (processedCount > 0) {
          toast.success(`${processedCount} valor(es) re-encodificado(s)${skippedCount > 0 ? ` (${skippedCount} já estava(m) em base64)` : ''}`);
        } else if (skippedCount > 0) {
          toast.warning(`Todos os valores já estão em base64 (${skippedCount} valor(es))`);
          return; // Não mudar o estado se nada foi processado
        } else {
          toast.warning("Nenhum valor foi re-encodificado");
          return;
        }
      } else {
        // DECODE: Base64 → texto plano
        // Processar linha por linha para preservar valores exatos
        const lines = newYaml.split('\n');
        for (let i = 0; i < lines.length; i++) {
          const line = lines[i];
          // Verificar se é uma linha de data: com valor base64
          const match = line.match(/^(\s+)([^:]+):\s+(.+)$/);
          if (match) {
            const [, indent, key, value] = match;
            // Verificar se essa key existe no yamlObj.data
            if (yamlObj.data && key in yamlObj.data) {
              // Pegar o valor atual da linha (deve ser base64)
              const currentValue = value.trim();
              
              // Ignorar certificados e chaves
              if (isCertificateOrKey(key, currentValue)) {
                skippedCount++;
                continue;
              }
              
              // Validar se é realmente base64 antes de decodificar
              if (isValidBase64(currentValue)) {
                try {
                  // Decodificar de Base64 para texto plano (com suporte UTF-8)
                  const decoded = base64DecodeUtf8(currentValue);
                  lines[i] = `${indent}${key}: ${decoded}`;
                  processedCount++;
                } catch (err) {
                  // Se não for Base64 válido, mantém original e conta como ignorado
                  console.error(`Erro ao decodificar ${key}:`, err);
                  skippedCount++;
                }
              } else {
                // Não é base64 válido, ignorar
                skippedCount++;
              }
            }
          }
        }
        newYaml = lines.join('\n');
        
        if (processedCount > 0) {
          toast.success(`${processedCount} valor(es) decodificado(s)${skippedCount > 0 ? ` (${skippedCount} ignorado(s) - não são base64)` : ''}`);
        } else if (skippedCount > 0) {
          toast.warning(`Nenhum valor base64 válido encontrado (${skippedCount} valor(es) ignorado(s))`);
          return; // Não mudar o estado se nada foi processado
        } else {
          toast.warning("Nenhum valor foi decodificado");
          return;
        }
      }

      setEditorValue(newYaml);
      setIsDecoded(!isDecoded);
      
      // Não adicionar ao histórico se voltou ao estado original
      if (newYaml !== originalYaml) {
        addToHistory(newYaml);
      }
    } catch (err) {
      toast.error("Erro ao processar YAML", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    }
  };

  const handleShowDiffModal = async (fullscreen = false) => {
    if (!selectedSecret) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para comparar");
      return;
    }
    setIsDiffLoading(true);
    try {
      const diffResponse = await apiClient.diffSecret(originalYaml, editorValue, selectedSecret?.name);
      const unifiedDiff = diffResponse.unifiedDiff || "";
      const html = diff2html(unifiedDiff, {
        drawFileList: false,
        matching: "lines",
        outputFormat: "side-by-side",
      });
      setDiffHtml(html);
      setDiffFullScreen(fullscreen);
      setDiffModalOpen(true);
    } catch (error) {
      toast.error("Erro ao gerar diff visual", {
        description: error instanceof Error ? error.message : "Erro desconhecido",
      });
    } finally {
      setIsDiffLoading(false);
    }
  };

  const handleDiffModalChange = (open: boolean) => {
    setDiffModalOpen(open);
    if (!open) {
      setDiffFullScreen(false);
    }
  };

  const toggleDiffFullScreen = () => {
    setDiffFullScreen((prev) => !prev);
  };

  const handleValidate = async () => {
    if (!selectedSecret) return;
    setIsValidating(true);
    try {
      await apiClient.validateSecret({
        cluster: selectedSecret.cluster,
        namespace: selectedSecret.namespace,
        yaml: editorValue,
        fieldManager: "web-configmap-editor",
      });
      toast.success("Dry-run bem-sucedido", {
        description: `${selectedSecret.namespace}/${selectedSecret.name}`,
      });
    } catch (err) {
      toast.error("Dry-run falhou", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsValidating(false);
    }
  };

  const handleCancel = () => {
    // Restaurar o YAML original, descartando alterações
    setEditorValue(originalYaml);
    setIsDecoded(false);
    setViewMode("editor");
    setEditorFullScreen(false);
    toast.info("Alterações descartadas");
  };

  const handleApply = async () => {
    if (!selectedSecret) return;

    // VALIDAÇÃO: Previne aplicação com valores decodificados
    if (isDecoded) {
      toast.error("Não é possível aplicar com valores decodificados", {
        description: "Clique no botão 'Encoded' para re-encodificar antes de aplicar",
      });
      return;
    }

    setIsApplying(true);
    try {
      await apiClient.applySecret(
        selectedSecret.cluster,
        selectedSecret.namespace,
        selectedSecret.name,
        {
          yaml: editorValue,
          fieldManager: "web-secret-editor",
          dryRun: false,
          force: true,
        }
      );
      toast.success("Secret aplicado", {
        description: `${selectedSecret.namespace}/${selectedSecret.name}`,
      });
      
      // Recarregar manifest do servidor após aplicar
      await refreshManifest();
    } catch (err) {
      // Exibir erro em modal dedicado ao invés de toast
      const errorMsg = err instanceof Error ? err.message : String(err);
      const resourceFullName = `Secret ${selectedNamespace}/${selectedSecret.name}`;

      setErrorTitle(`Falha ao aplicar ${resourceFullName}`);
      setErrorMessage(errorMsg);
      setErrorDialogOpen(true);

      // Toast como fallback visual rápido
      toast.error("Falha ao aplicar Secret", { description: "Verifique os detalhes no modal de erro" });
    } finally {
      setIsApplying(false);
    }
  };

  const openApplyConfirm = () => {
    if (!selectedSecret) return;
    if (!hasChanges) {
      toast.info("Nenhuma alteração para aplicar");
      return;
    }
    setApplyConfirmOpen(true);
  };

  const confirmApplyChanges = async () => {
    setApplyConfirmOpen(false);
    await handleApply();
  };

  const fetchDescribe = async () => {
    if (!selectedSecret) return;

    setDescribeLoading(true);
    try {
      const result = await apiClient.describeSecret(selectedSecret.cluster, selectedSecret.namespace, selectedSecret.name);
      setDescribeContent(result.describe);
    } catch (err) {
      toast.error("Erro ao buscar describe", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
      setDescribeContent("Error loading describe");
    } finally {
      setDescribeLoading(false);
    }
  };

  const handleViewDescribe = async () => {
    if (!selectedSecret) return;
    setDescribeModalOpen(true);
    await fetchDescribe();
  };

  const handleRefreshDescribe = async () => {
    await fetchDescribe();
  };

  const namespaceSelector = (
    <Select
      value={selectedNamespace || "__all__"}
      onValueChange={(value) => onNamespaceChange(value === "__all__" ? "" : value)}
      disabled={!cluster || filteredNamespaces.length === 0}
    >
      <SelectTrigger className="w-[140px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="__all__">Todos</SelectItem>
        {filteredNamespaces.map((ns) => (
          <SelectItem key={ns.name} value={ns.name}>
            {ns.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );

  const leftTitleAction = (
    <div className="flex items-center gap-2 flex-wrap">
      {namespaceSelector}
      <Button
        variant={showSystemNamespaces ? "secondary" : "outline"}
        size="sm"
        onClick={onToggleSystemNamespaces}
        title={showSystemNamespaces ? "Mostrar namespaces e secrets de sistema (incluindo Helm)" : "Ocultar namespaces e secrets de sistema (incluindo Helm)"}
      >
        {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
      </Button>
      <Button variant="outline" size="sm" onClick={refreshSecrets} disabled={!cluster || loading}>
        <RefreshCcw className="w-4 h-4" />
      </Button>
    </div>
  );

  const handleDeleteSecret = async () => {
    if (!selectedSecret) return;
    setIsDeleting(true);
    try {
      const response = await fetch(
        `/api/v1/secrets/${selectedSecret.cluster}/${selectedSecret.namespace}/${selectedSecret.name}`,
        { method: "DELETE", headers: { Authorization: `Bearer ${localStorage.getItem("auth_token")}` } }
      );
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error?.message || `HTTP ${response.status}`);
      }
      toast.success("Secret deletado com sucesso!", { description: `${selectedSecret.namespace}/${selectedSecret.name}` });
      setSelectedSecret(null);
      setManifest(null);
      setEditorValue("");
      setOriginalYaml("");
      setDeleteConfirmOpen(false);
      await refetch();
    } catch (err) {
      toast.error("Erro ao deletar Secret", { description: err instanceof Error ? err.message : "Erro desconhecido" });
    } finally {
      setIsDeleting(false);
    }
  };

  const rightTitleAction = (
    <div className="flex items-center gap-2">
      {selectedSecret && onOpenCompare && (
        <Button
          variant="ghost"
          size="sm"
          title="Abrir em Edição Lado a Lado"
          onClick={() => onOpenCompare({ type: "secret", namespace: selectedSecret.namespace, name: selectedSecret.name })}
        >
          <SplitSquareHorizontal className="w-4 h-4" />
        </Button>
      )}
      {selectedSecret && (
        <Button
          variant="outline"
          size="sm"
          onClick={handleViewDescribe}
          disabled={describeLoading}
        >
          {describeLoading ? (
            <Loader2 className="w-4 h-4 mr-1 animate-spin" />
          ) : (
            <FileText className="w-4 h-4 mr-1" />
          )}
          Describe
        </Button>
      )}
      <Button
        variant="default"
        size="sm"
        onClick={() => setCreateModalOpen(true)}
        disabled={!cluster}
      >
        <Plus className="w-4 h-4 mr-2" />
        Criar Secret
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={refreshManifest}
        disabled={!selectedSecret || manifestLoading}
      >
        <RefreshCcw className="w-4 h-4 mr-2" />
        Recarregar YAML
      </Button>
      {selectedSecret && (
        <ProtectedAction>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={manifestLoading}>
                <MoreVertical className="w-4 h-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => setDeleteConfirmOpen(true)}
                disabled={isDeleting}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="w-4 h-4 mr-2" />
                Deletar Secret
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ProtectedAction>
      )}
    </div>
  );

  const renderSecretList = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para listar Secrets
        </div>
      );
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Carregando Secrets...
        </div>
      );
    }

    if (filteredSecrets.length === 0) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          {secrets.length === 0
            ? "Nenhum Secret encontrado"
            : "Nenhum Secret corresponde à busca"}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {filteredSecrets.map((cm) => {
          const isSelected =
            selectedSecret?.name === cm.name &&
            selectedSecret?.namespace === cm.namespace;
          return (
            <button
              key={`${cm.cluster}-${cm.namespace}-${cm.name}`}
              onClick={() => handleSelectSecret(cm)}
              className={`w-full text-left p-3 rounded-lg border transition-colors ${
                isSelected
                  ? "border-primary bg-primary/10 text-primary-foreground"
                  : "border-border/60 hover:border-primary/40"
              }`}
            >
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-sm truncate">{cm.name}</span>
                {(() => {
                  const cert = tlsCertMap.get(`${cm.namespace}/${cm.name}`);
                  if (!cert) return null;
                  return cert.status === "expired" ? (
                    <span className="flex-shrink-0 text-[10px] font-medium px-1 py-0.5 rounded bg-red-500/20 text-red-400 border border-red-500/30">
                      Expirado
                    </span>
                  ) : (
                    <span className="flex-shrink-0 text-[10px] font-medium px-1 py-0.5 rounded bg-yellow-500/20 text-yellow-400 border border-yellow-500/30">
                      {cert.daysRemaining}d
                    </span>
                  );
                })()}
              </div>
              <div className="text-xs text-muted-foreground">{cm.namespace}</div>
              {cm.serviceClusterIPs && cm.serviceClusterIPs.length > 0 && (
                <div className="text-[10px] font-mono mt-0.5 flex items-center gap-1">
                  <span className="text-muted-foreground/60 uppercase tracking-wide">CIP</span>
                  <span className="text-blue-400/80">{cm.serviceClusterIPs.join(" · ")}</span>
                </div>
              )}
              {cm.serviceExternalIPs && cm.serviceExternalIPs.length > 0 && (
                <div className="text-[10px] font-mono mt-0.5 flex items-center gap-1">
                  <span className="text-muted-foreground/60 uppercase tracking-wide">EXT</span>
                  <span className="text-green-400/80">{cm.serviceExternalIPs.join(" · ")}</span>
                </div>
              )}
              <div className="text-[11px] text-muted-foreground mt-1">
                {cm.dataKeys.length} {cm.dataKeys.length === 1 ? 'key' : 'keys'}
              </div>
            </button>
          );
        })}
      </div>
    );
  };

  // Função para normalizar YAML e verificar se há mudanças reais
  const hasRealChanges = useMemo(() => {
    if (!editorValue || !originalYaml) return false;
    if (editorValue === originalYaml) return false;

    try {
      // Carregar ambos os YAMLs como objetos
      const currentObj = yaml.load(editorValue) as any;
      const originalObj = yaml.load(originalYaml) as any;

      // Comparar estruturas (exceto data que precisa de comparação especial)
      const currentCopy = { ...currentObj };
      const originalCopy = { ...originalObj };
      
      const currentData = currentCopy.data;
      const originalData = originalCopy.data;
      
      delete currentCopy.data;
      delete originalCopy.data;

      // Comparar metadados (tudo exceto data)
      if (JSON.stringify(currentCopy) !== JSON.stringify(originalCopy)) {
        return true;
      }

      // Comparar dados normalizando base64
      if (!currentData && !originalData) return false;
      if (!currentData || !originalData) return true;

      const currentKeys = Object.keys(currentData).sort();
      const originalKeys = Object.keys(originalData).sort();

      if (JSON.stringify(currentKeys) !== JSON.stringify(originalKeys)) {
        return true;
      }

      // Comparar cada valor, normalizando base64
      for (const key of currentKeys) {
        const currentValue = currentData[key];
        const originalValue = originalData[key];

        if (currentValue === originalValue) continue;

        // Tentar comparar como base64 (pode estar decoded em um e encoded no outro)
        try {
          // Se um for base64 e outro texto plano, tentar normalizar
          const isCurrentBase64 = /^[A-Za-z0-9+/]*={0,2}$/.test(currentValue);
          const isOriginalBase64 = /^[A-Za-z0-9+/]*={0,2}$/.test(originalValue);

          if (isCurrentBase64 && isOriginalBase64) {
            // Ambos base64, comparar decodificados
            const currentDecoded = base64DecodeUtf8(currentValue);
            const originalDecoded = base64DecodeUtf8(originalValue);
            if (currentDecoded !== originalDecoded) return true;
          } else if (isCurrentBase64 && !isOriginalBase64) {
            // Current é base64, original é texto plano
            const currentDecoded = base64DecodeUtf8(currentValue);
            if (currentDecoded !== originalValue) return true;
          } else if (!isCurrentBase64 && isOriginalBase64) {
            // Original é base64, current é texto plano
            const originalDecoded = base64DecodeUtf8(originalValue);
            if (currentValue !== originalDecoded) return true;
          } else {
            // Ambos texto plano
            if (currentValue !== originalValue) return true;
          }
        } catch {
          // Se falhar a comparação normalizada, comparar direto
          if (currentValue !== originalValue) return true;
        }
      }

      return false;
    } catch {
      // Se falhar o parse, comparar strings direto
      return editorValue !== originalYaml;
    }
  }, [editorValue, originalYaml]);

  const hasChanges = hasRealChanges;

  const renderManifestPanel = () => {
    if (!cluster) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Selecione um cluster para visualizar Secrets
        </div>
      );
    }

    if (!selectedSecret) {
      return (
        <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
          Escolha um Secret para visualizar o manifesto
        </div>
      );
    }

    const updatedAt = selectedSecret.updatedAt
      ? new Date(selectedSecret.updatedAt).toLocaleString()
      : "--";

    // Keyboard shortcuts for normal editor
    const handleEditorKeyDown = (e: React.KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 'z' && !e.shiftKey) {
          e.preventDefault();
          handleUndo();
        } else if ((e.key === 'y') || (e.key === 'z' && e.shiftKey)) {
          e.preventDefault();
          handleRedo();
        } else if (e.key === 's') {
          e.preventDefault();
          // Ctrl+S: Salvar checkpoint local (não aplica)
          if (hasChanges) {
            // Adicionar checkpoint ao histórico
            addToHistory(editorValue);
            toast.success("Checkpoint salvo localmente", {
              description: "Alterações mantidas no histórico local. Use 'Aplicar' para confirmar no cluster.",
              style: {
                background: '#dcfce7',
                border: '1px solid #86efac',
                color: '#166534',
              },
            });
          }
        }
      } else if (e.key === 'Escape') {
        handleCancel();
      }
    };

    // Extrair versão dos labels (app.kubernetes.io/version ou version)
    const appVersion = selectedSecret.labels?.["app.kubernetes.io/version"] ||
                       selectedSecret.labels?.["version"] ||
                       selectedSecret.labels?.["app.version"];

    return (
      <div className="space-y-3" onKeyDown={handleEditorKeyDown} tabIndex={-1}>
        <div className="flex items-start gap-4 text-xs border-b border-border/50 pb-2">
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Cluster</span>
            <span className="font-medium">{selectedSecret.cluster}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Namespace</span>
            <span className="font-medium">{selectedSecret.namespace}</span>
          </div>
          {appVersion && (
            <div className="flex flex-col">
              <span className="text-muted-foreground uppercase mb-0.5">Versão</span>
              <span className="font-mono text-primary">{appVersion}</span>
            </div>
          )}
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">ResourceVersion</span>
            <span className="font-mono">{selectedSecret.resourceVersion || "--"}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-muted-foreground uppercase mb-0.5">Atualizado</span>
            <span className="font-medium">{updatedAt}</span>
          </div>
          {selectedSecret.labels && Object.keys(selectedSecret.labels).length > 0 && (
            <div className="flex flex-col">
              <button
                type="button"
                onClick={() => setShowLabels((prev) => !prev)}
                className="flex items-center gap-1 text-muted-foreground uppercase mb-0.5 hover:text-foreground"
              >
                {showLabels ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
                <span>Labels</span>
              </button>
              {showLabels && (
                <div className="flex flex-wrap gap-1 mt-1">
                  {Object.entries(selectedSecret.labels).map(([key, value]) => (
                    <span
                      key={`${key}-${value}`}
                      className="px-1.5 py-0.5 bg-secondary/60 rounded text-[10px] font-mono"
                    >
                      {key}={value}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Botão de visualização do certificado TLS */}
          {selectedSecret.type === "kubernetes.io/tls" && (
            <div className="mx-4 flex-shrink-0">
              <Button
                variant="default"
                size="sm"
                className="h-7 text-xs gap-1.5 bg-blue-600 hover:bg-blue-700 text-white border-0"
                onClick={handleViewTlsCert}
                disabled={tlsCertLoading}
                title="Visualizar detalhes do certificado TLS (Subject, Issuer, SANs, validade)"
              >
                {tlsCertLoading
                  ? <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  : <Shield className="w-3.5 h-3.5" />
                }
                Ver Certificado TLS
              </Button>
            </div>
          )}
        </div>

        <div className="space-y-3">
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <p className="text-sm font-medium">Manifesto YAML</p>
              <div className="flex items-center gap-2">
                {manifestLoading && (
                  <span className="text-xs text-muted-foreground">Carregando...</span>
                )}
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={handleUndo}
                    disabled={!canUndo}
                    className={`px-2 py-1 text-xs font-medium ${
                      canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Desfazer (Ctrl+Z)"
                  >
                    <Undo2 className="w-3.5 h-3.5" />
                  </button>
                  <button
                    type="button"
                    onClick={handleRedo}
                    disabled={!canRedo}
                    className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                      canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Refazer (Ctrl+Y)"
                  >
                    <Redo2 className="w-3.5 h-3.5" />
                  </button>
                </div>
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={() => handleToggleView("editor")}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    }`}
                  >
                    Editor
                  </button>
                  <button
                    type="button"
                    onClick={() => handleToggleView("diff")}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                    disabled={!hasChanges}
                  >
                    Diff
                  </button>
                </div>
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={handleToggleDecode}
                    className={`px-3 py-1 text-xs font-medium flex items-center gap-1.5 ${
                      isDecoded
                        ? "bg-yellow-500/20 text-yellow-600 dark:text-yellow-400 border-yellow-500/30"
                        : "bg-green-500/20 text-green-600 dark:text-green-400 border-green-500/30"
                    }`}
                    title={isDecoded ? "Re-encodificar para Base64" : "Decodificar para texto plano"}
                  >
                    {isDecoded ? (
                      <>
                        <Unlock className="w-3.5 h-3.5" />
                        Decoded
                      </>
                    ) : (
                      <>
                        <Lock className="w-3.5 h-3.5" />
                        Encoded
                      </>
                    )}
                  </button>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditorFullScreen(true)}
                  title="Abrir editor em tela cheia"
                  disabled={!selectedSecret}
                >
                  <Maximize2 className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
            {viewMode === "editor" && (
              <MonacoYamlEditor
                value={editorValue}
                onChange={handleEditorChange}
                height={600}
              />
            )}
            {viewMode === "diff" && (
              <MonacoYamlEditor
                mode="diff"
                originalValue={originalYaml}
                value={editorValue}
                height={600}
                readOnly
              />
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleShowDiffModal(false)}
              disabled={!selectedSecret || !hasChanges || isDiffLoading}
            >
              {isDiffLoading ? (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              ) : (
                <FileDiff className="w-4 h-4 mr-2" />
              )}
              Visualizar diff
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleShowDiffModal(true)}
              disabled={!selectedSecret || !hasChanges || isDiffLoading}
              className="gap-2"
              title="Abrir diff ocupando toda a tela"
            >
              <Maximize2 className="w-4 h-4" />
              Tela cheia
            </Button>
            <Button
              variant="secondary"
              size="sm"
              onClick={handleValidate}
              disabled={!selectedSecret || isValidating}
            >
              <CheckCircle2 className="w-4 h-4 mr-2" /> Validar (Dry-run)
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={!selectedSecret || !hasChanges}
            >
              <X className="w-4 h-4 mr-2" /> Cancelar
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={openApplyConfirm}
              disabled={!selectedSecret || isApplying || !hasChanges}
            >
              <TriangleAlert className="w-4 h-4 mr-2" /> Aplicar
            </Button>
          </div>

        </div>
      </div>
    );
  };

  const leftContent = (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        {searchQuery && (
          <button
            type="button"
            onClick={() => setSearchQuery("")}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            aria-label="Limpar busca"
          >
            ×
          </button>
        )}
        <Input
          placeholder="Buscar por nome ou label..."
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
          className="pl-10 pr-8"
        />
      </div>

      {renderSecretList()}
    </div>
  );

  const collapseButton = (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => setIsSidebarCollapsed((prev) => !prev)}
      title={isSidebarCollapsed ? "Mostrar painel de Secrets" : "Ocultar painel de Secrets"}
    >
      {isSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
    </Button>
  );

  const renderDiffDialog = () => {
    const dialogSizeClass = diffFullScreen
      ? "w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none"
      : "max-w-6xl max-h-[85vh]";
    const scrollAreaHeight = diffFullScreen ? "h-[calc(100vh-8rem)]" : "h-[calc(85vh-8rem)]";

    return (
      <Dialog open={diffModalOpen} onOpenChange={handleDiffModalChange}>
        <DialogContent className={`bg-background border-border ${dialogSizeClass}`}>
          <DialogHeader className="border-b border-border pb-4 pr-12">
            <div className="flex items-start justify-between gap-4">
              <div>
                <DialogTitle className="text-xl font-semibold text-primary">
                  Diff Visual
                </DialogTitle>
                <DialogDescription className="text-sm text-muted-foreground">
                  Comparação lado a lado entre o YAML original e a versão editada
                  {selectedSecret && ` • ${selectedSecret.namespace}/${selectedSecret.name}`}
                </DialogDescription>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={toggleDiffFullScreen}
                title={diffFullScreen ? "Sair de tela cheia" : "Tela cheia"}
                aria-label={diffFullScreen ? "Sair de tela cheia" : "Tela cheia"}
                className="gap-2"
              >
                {diffFullScreen ? (
                  <>
                    <Minimize2 className="w-4 h-4" />
                    <span>Sair de tela cheia</span>
                  </>
                ) : (
                  <>
                    <Maximize2 className="w-4 h-4" />
                    <span>Tela cheia</span>
                  </>
                )}
              </Button>
            </div>
          </DialogHeader>
          <ScrollArea className={`${scrollAreaHeight} w-full`}>
            <div className="p-4">
              {diffHtml ? (
                <div className="diff2html-dark" dangerouslySetInnerHTML={{ __html: diffHtml }} />
              ) : (
                <div className="flex items-center justify-center h-32 text-muted-foreground">
                  <p>Nenhum diff disponível.</p>
                </div>
              )}
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    );
  };

  const renderEditorFullScreen = () => {
    if (!selectedSecret) return null;

    // Keyboard shortcuts
    const handleKeyDown = (e: React.KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 'z' && !e.shiftKey) {
          e.preventDefault();
          handleUndo();
        } else if ((e.key === 'y') || (e.key === 'z' && e.shiftKey)) {
          e.preventDefault();
          handleRedo();
        } else if (e.key === 's') {
          e.preventDefault();
          // Ctrl+S: Salvar checkpoint local (não aplica)
          if (hasChanges) {
            // Adicionar checkpoint ao histórico
            addToHistory(editorValue);
            toast.success("Checkpoint salvo localmente", {
              description: "Alterações mantidas no histórico local. Use 'Aplicar' para confirmar no cluster.",
              style: {
                background: '#dcfce7',
                border: '1px solid #86efac',
                color: '#166534',
              },
            });
          }
        }
      } else if (e.key === 'Escape') {
        handleCancel();
      }
    };

    return (
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent 
          className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none p-0"
          onKeyDown={handleKeyDown}
        >
          <div className="h-full flex flex-col">
            <DialogHeader className="border-b border-border px-6 py-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <DialogTitle className="text-xl font-semibold text-primary">
                    Editor YAML - Tela Cheia
                  </DialogTitle>
                  <DialogDescription className="text-sm text-muted-foreground">
                    {selectedSecret.namespace}/{selectedSecret.name}
                  </DialogDescription>
                </div>
                <div className="flex items-center gap-2 flex-wrap">
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={handleUndo}
                      disabled={!canUndo}
                      className={`px-2 py-1 text-xs font-medium ${
                        canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                      }`}
                      title="Desfazer (Ctrl+Z)"
                    >
                      <Undo2 className="w-3.5 h-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={handleRedo}
                      disabled={!canRedo}
                      className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                        canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                      }`}
                      title="Refazer (Ctrl+Y)"
                    >
                      <Redo2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={() => handleToggleView("editor")}
                      className={`px-3 py-1 text-xs font-medium ${
                        viewMode === "editor" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                      }`}
                    >
                      Editor
                    </button>
                    <button
                      type="button"
                      onClick={() => handleToggleView("diff")}
                      className={`px-3 py-1 text-xs font-medium ${
                        viewMode === "diff" ? "bg-primary text-white" : "bg-background text-muted-foreground"
                      } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                      disabled={!hasChanges}
                    >
                      Diff
                    </button>
                  </div>
                  <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                    <button
                      type="button"
                      onClick={handleToggleDecode}
                      className={`px-3 py-1 text-xs font-medium flex items-center gap-1.5 ${
                        isDecoded
                          ? "bg-yellow-500/20 text-yellow-600 dark:text-yellow-400 border-yellow-500/30"
                          : "bg-green-500/20 text-green-600 dark:text-green-400 border-green-500/30"
                      }`}
                      title={isDecoded ? "Re-encodificar para Base64" : "Decodificar para texto plano"}
                    >
                      {isDecoded ? (
                        <>
                          <Unlock className="w-3.5 h-3.5" />
                          Decoded
                        </>
                      ) : (
                        <>
                          <Lock className="w-3.5 h-3.5" />
                          Encoded
                        </>
                      )}
                    </button>
                  </div>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={handleValidate}
                    disabled={!selectedSecret || isValidating}
                  >
                    {isValidating ? (
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                      <CheckCircle2 className="w-4 h-4 mr-2" />
                    )}
                    Dry-run
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleCancel}
                    title="Descartar alterações e sair (Esc)"
                  >
                    Cancelar
                  </Button>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => {
                      openApplyConfirm();
                      setEditorFullScreen(false);
                    }}
                    disabled={!selectedSecret || isApplying || !hasChanges}
                  >
                    <TriangleAlert className="w-4 h-4 mr-2" />
                    Aplicar
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setEditorFullScreen(false)}
                    title="Minimizar tela cheia (Esc)"
                  >
                    <Minimize2 className="w-4 h-4" />
                  </Button>
                </div>
              </div>
            </DialogHeader>
            <div className="flex-1 p-4">
              {viewMode === "editor" && (
                <MonacoYamlEditor
                  value={editorValue}
                  onChange={handleEditorChange}
                  height="calc(100vh - 140px)"
                />
              )}
              {viewMode === "diff" && (
                <MonacoYamlEditor
                  mode="diff"
                  originalValue={originalYaml}
                  value={editorValue}
                  height="calc(100vh - 140px)"
                  readOnly
                />
              )}
            </div>
          </div>
        </DialogContent>
      </Dialog>
    );
  };

  const renderApplyConfirmDialog = () => {
    if (!selectedSecret) return null;

    // Gerar diff compacto apenas com mudanças
    const generateCompactDiff = () => {
      try {
        const originalObj = yaml.load(originalYaml) as any;
        const updatedObj = yaml.load(editorValue) as any;
        
        const changes: Array<{ path: string; before: any; after: any }> = [];
        
        const compareObjects = (obj1: any, obj2: any, path: string = '') => {
          const allKeys = new Set([...Object.keys(obj1 || {}), ...Object.keys(obj2 || {})]);
          
          for (const key of allKeys) {
            const currentPath = path ? `${path}.${key}` : key;
            const val1 = obj1?.[key];
            const val2 = obj2?.[key];
            
            if (val1 === val2) continue;
            
            if (typeof val1 === 'object' && typeof val2 === 'object' && val1 !== null && val2 !== null && !Array.isArray(val1) && !Array.isArray(val2)) {
              compareObjects(val1, val2, currentPath);
            } else if (JSON.stringify(val1) !== JSON.stringify(val2)) {
              changes.push({
                path: currentPath,
                before: val1 === undefined ? '(não existe)' : typeof val1 === 'object' ? JSON.stringify(val1, null, 2) : String(val1),
                after: val2 === undefined ? '(removido)' : typeof val2 === 'object' ? JSON.stringify(val2, null, 2) : String(val2)
              });
            }
          }
        };
        
        compareObjects(originalObj, updatedObj);
        
        return changes;
      } catch {
        return [];
      }
    };
    
    const changes = generateCompactDiff();

    return (
      <Dialog open={applyConfirmOpen} onOpenChange={setApplyConfirmOpen}>
        <DialogContent className="max-w-4xl max-h-[90vh] bg-background border-border">
          <DialogHeader>
            <DialogTitle className="text-xl font-semibold text-primary">
              Confirmar aplicação
            </DialogTitle>
            <DialogDescription>
              Essa ação vai aplicar o Secret diretamente no cluster selecionado.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <p><span className="text-muted-foreground">Cluster:</span> {selectedSecret.cluster}</p>
              <p><span className="text-muted-foreground">Namespace:</span> {selectedSecret.namespace}</p>
              <p><span className="text-muted-foreground">Secret:</span> {selectedSecret.name}</p>
            </div>
            
            {changes.length > 0 && (
              <div className="space-y-2">
                <p className="font-semibold text-sm">Mudanças detectadas ({changes.length}):</p>
                <div className="max-h-[400px] overflow-y-auto space-y-2 border rounded-lg p-3 bg-muted/10">
                  {changes.map((change, idx) => (
                    <div key={idx} className="border-l-2 border-blue-500 pl-3 py-2 bg-background/50 rounded-r text-xs">
                      <p className="font-mono font-semibold text-blue-400 mb-2">{change.path}</p>
                      <div className="grid grid-cols-2 gap-2">
                        <div className="bg-red-500/10 border border-red-500/30 rounded p-2">
                          <p className="text-red-400 font-semibold mb-1">Antes:</p>
                          <pre className="whitespace-pre-wrap break-all text-[11px] text-red-300">{change.before}</pre>
                        </div>
                        <div className="bg-green-500/10 border border-green-500/30 rounded p-2">
                          <p className="text-green-400 font-semibold mb-1">Depois:</p>
                          <pre className="whitespace-pre-wrap break-all text-[11px] text-green-300">{change.after}</pre>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
            
            <p className="text-muted-foreground">
              Esta operação não possui rollback automático. Confirme que as mudanças estão corretas.
            </p>
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Button
              variant="ghost"
              onClick={() => setApplyConfirmOpen(false)}
            >
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={confirmApplyChanges}
              disabled={isApplying}
            >
              {isApplying ? (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              ) : (
                <TriangleAlert className="w-4 h-4 mr-2" />
              )}
              Confirmar
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    );
  };

  if (isSidebarCollapsed) {
    return (
      <>
        <div className="p-4 h-full">
          <div className="grid grid-cols-1 h-full">
            <div className="p-4 bg-gradient-card border-border/50 rounded-xl flex flex-col min-h-0">
              <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary">
                <div className="flex items-center gap-3 flex-wrap">
                  {collapseButton}
                  <p className="text-base font-semibold text-primary">Visualização</p>
                </div>
                {rightTitleAction}
              </div>
              <div className="flex-1 overflow-auto min-h-0">
                {renderManifestPanel()}
              </div>
            </div>
          </div>
        </div>

        {renderDiffDialog()}
        {renderEditorFullScreen()}
        {renderApplyConfirmDialog()}
      </>
    );
  }

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Secrets",
          titleAction: (
            <div className="flex items-center gap-2">
              {namespaceSelector}
              <Button
                variant={showSystemNamespaces ? "secondary" : "outline"}
                size="sm"
                onClick={onToggleSystemNamespaces}
                title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
              >
                {showSystemNamespaces ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
              </Button>
              <Button variant="outline" size="sm" onClick={refreshSecrets} disabled={!cluster || loading}>
                <RefreshCcw className="w-4 h-4" />
              </Button>
              {collapseButton}
            </div>
          ),
          content: leftContent,
        }}
        rightPanel={{
          title: "Visualização",
          titleAction: rightTitleAction,
          content: renderManifestPanel(),
        }}
      />

      {renderDiffDialog()}
      {renderEditorFullScreen()}
      {renderApplyConfirmDialog()}

      {/* Modal Create Secret */}
      <CreateSecretModal
        open={createModalOpen}
        onOpenChange={setCreateModalOpen}
        cluster={cluster}
        namespaces={filteredNamespaces}
        onSuccess={() => {
          refetch();
        }}
      />

      {/* Modal de visualização do certificado TLS */}
      <CertificateDetailModal
        open={tlsCertModalOpen}
        onOpenChange={setTlsCertModalOpen}
        cert={tlsCertInfo}
      />

      {/* Modal Delete Confirm */}
      <Dialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <Trash2 className="w-5 h-5" />
              Deletar Secret
            </DialogTitle>
            <DialogDescription>
              Esta acao e permanente e nao pode ser desfeita.
            </DialogDescription>
          </DialogHeader>
          <div className="bg-destructive/10 border border-destructive/20 rounded-md p-4 my-4">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Cluster:</span>
                <span className="text-sm text-muted-foreground">{selectedSecret?.cluster}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Namespace:</span>
                <span className="text-sm text-muted-foreground">{selectedSecret?.namespace}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Secret:</span>
                <span className="text-sm font-semibold">{selectedSecret?.name}</span>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmOpen(false)} disabled={isDeleting}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={handleDeleteSecret} disabled={isDeleting}>
              {isDeleting ? (<><Loader2 className="w-4 h-4 mr-2 animate-spin" />Deletando...</>) : (<><Trash2 className="w-4 h-4 mr-2" />Deletar Secret</>)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal Describe */}
      <Dialog open={describeModalOpen} onOpenChange={setDescribeModalOpen}>
        <DialogContent className="max-w-6xl max-h-[90vh]">
          <DialogHeader>
            <DialogTitle>Kubectl Describe - {selectedSecret?.name}</DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {selectedSecret?.namespace}/{selectedSecret?.name}
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className="h-[70vh]">
            {describeLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="w-6 h-6 animate-spin" />
              </div>
            ) : (
              <pre className="text-xs font-mono bg-muted p-4 rounded whitespace-pre-wrap">{describeContent}</pre>
            )}
          </ScrollArea>

          <DialogFooter className="gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleRefreshDescribe}
              disabled={describeLoading}
              className="text-xs"
            >
              {describeLoading ? (
                <Loader2 className="w-3 h-3 animate-spin mr-1" />
              ) : (
                <RefreshCcw className="w-3 h-3 mr-1" />
              )}
              Atualizar
            </Button>

            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(describeContent);
                toast.success("Describe copiado para a área de transferência!");
              }}
              disabled={!describeContent || describeLoading}
              className="text-xs"
            >
              <Copy className="w-3 h-3 mr-1" />
              Copiar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Modal de erro (destaque para analistas) ───────── */}
      <Dialog open={errorDialogOpen} onOpenChange={setErrorDialogOpen}>
        <DialogContent className="max-w-3xl max-h-[85vh] bg-background border-destructive/50 border-2">
          <DialogHeader className="border-b border-destructive/30 pb-4">
            <div className="flex items-start gap-3">
              <div className="shrink-0 w-10 h-10 rounded-full bg-destructive/10 flex items-center justify-center">
                <AlertCircle className="w-6 h-6 text-destructive" />
              </div>
              <div className="flex-1 min-w-0">
                <DialogTitle className="text-destructive text-lg font-semibold">
                  {errorTitle}
                </DialogTitle>
                <DialogDescription className="text-muted-foreground text-sm mt-1">
                  Detalhes técnicos do erro abaixo. Verifique conflitos de field managers ou validação de YAML.
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <ScrollArea className="max-h-[60vh] pr-4">
            <div className="space-y-4">
              {/* Mensagem de erro formatada */}
              <div className="bg-destructive/5 border border-destructive/20 rounded-lg p-4">
                <div className="flex items-center gap-2 mb-3">
                  <TriangleAlert className="w-4 h-4 text-destructive" />
                  <span className="text-sm font-semibold text-destructive">Erro Detalhado</span>
                </div>
                <pre className="text-xs font-mono text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">
                  {errorMessage}
                </pre>
              </div>

              {/* Dicas de resolução (se erro contiver palavras-chave conhecidas) */}
              {errorMessage.includes("conflicts with") && (
                <div className="bg-blue-500/5 border border-blue-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <AlertCircle className="w-4 h-4 text-blue-400" />
                    <span className="text-sm font-semibold text-blue-400">Sugestão de Resolução</span>
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Este erro indica conflito de <strong>field manager</strong> (Server-Side Apply).
                    O recurso foi previamente aplicado com <code className="bg-muted px-1 rounded">kubectl apply</code> (client-side)
                    e agora está sendo aplicado via SSA com field manager diferente.
                  </p>
                  <p className="text-xs text-muted-foreground mt-2 leading-relaxed">
                    <strong>Ação recomendada:</strong> O apply usa <code className="bg-muted px-1 rounded">force=true</code> para assumir ownership dos campos.
                    Se o erro persistir, verifique se o YAML está válido ou tente recarregar o recurso antes de editar.
                  </p>
                </div>
              )}

              {errorMessage.includes("illegal base64") && (
                <div className="bg-yellow-500/5 border border-yellow-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <AlertCircle className="w-4 h-4 text-yellow-400" />
                    <span className="text-sm font-semibold text-yellow-400">Sugestão de Resolução</span>
                  </div>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Erro de validação de base64 no campo <code className="bg-muted px-1 rounded">data</code> do Secret.
                    Verifique se os valores não foram editados manualmente ou se há caracteres inválidos (espaços, quebras de linha).
                  </p>
                </div>
              )}
            </div>
          </ScrollArea>

          <DialogFooter className="border-t border-border/50 pt-4">
            <Button variant="outline" onClick={() => setErrorDialogOpen(false)}>
              Fechar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
