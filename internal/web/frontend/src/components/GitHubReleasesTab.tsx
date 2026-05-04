import { useState, useEffect, useRef } from "react";
import * as React from "react";
import { SplitView } from "@/components/SplitView";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Info,
  Search,
  GitCompare,
  Database,
  Users,
  FileText,
  Clock,
  GitBranch,
  AlertCircle,
  Settings,
  Play,

  Plus,
  Trash2,
  CheckCircle2,
  XCircle,
  Loader2,
  RefreshCw,
  ExternalLink,
  ShieldAlert,
  ShieldCheck,
  Download,
  Pencil,
  Check
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CredentialRedirectDialog } from "./profile/CredentialRedirectDialog";
import { DeploymentScanModal } from "./DeploymentScanModal";
import { ServiceNowImportModal } from "./ServiceNowImportModal";
import { SreApprovalButton } from "./SreApprovalButton";
import { useClusters } from "@/hooks/useAPI";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface ProductionDeployment {
  id: number;
  deployment_name: string;
  namespace: string;
  cluster: string;
  version: string;
  squad: string;
  servicenow_task: string;
  last_seen: string;
  age: string;
}

interface GitHubCommit {
  sha: string;
  message: string;
  author: string;
  date: string;
  url: string;
}

interface GitHubFile {
  filename: string;
  status: string;
  additions: number;
  deletions: number;
  changes: number;
  patch?: string;
}

interface ComparisonResult {
  production_deployment: ProductionDeployment;
  base_tag: string;
  head_tag: string;
  commits: GitHubCommit[];
  files_changed: GitHubFile[];  // ✅ Backend retorna files_changed, não files
  total_commits: number;
  total_files: number;
  repository_url: string;
  compare_url: string;
}

interface DeploymentRecord {
  id: number;
  deployment_name: string;
  namespace: string;
  cluster: string;
  version: string;
  squad: string;
  servicenow_task: string;
  last_seen: string;
  age: string;
  resource_kind?: string;
}

interface ComparisonItem {
  id: string;
  deploymentName?: string;  // Opcional - apenas para exibição
  githubRepo: string;
  productionTag: string;
  newTag: string;
  chgNumber?: string;  // Número da mudança ServiceNow
  approvalUrl?: string; // URL de aprovação SRE (capturada do Teams/Mr.ViaBot)
  approvalStatus?: 'checking' | 'pending' | 'approved' | 'finalized' | 'error'; // status pré-verificado no devstartcd
  approverEmail?: string; // email do aprovador (preenchido após pré-verificação)
  status: 'pending' | 'loading' | 'success' | 'error';
  result?: ComparisonResult;
  error?: string;
  errorType?: 'saml_authorization_required' | 'not_found' | 'unknown';
}

// Interface para erros SAML
interface SAMLError {
  error: string;
  error_type: 'saml_authorization_required';
  message: string;
  instructions: string[];
  github_settings_url: string;
  github_web_url: string;
}

export const GitHubReleasesTab = () => {
  const queryClient = useQueryClient();
  const { clusters: availableClusters } = useClusters();

  const [deploymentName, setDeploymentName] = useState("");  // Nome do deployment na base
  const [githubRepo, setGithubRepo] = useState("");           // Nome do repositório no GitHub
  const [productionTag, setProductionTag] = useState("");     // Tag atual em produção
  const [newTag, setNewTag] = useState("");                   // Tag da nova release
  const [showAllFiles, setShowAllFiles] = useState(false);
  const [searchTriggered, setSearchTriggered] = useState(false);
  const [showTokenModal, setShowTokenModal] = useState(false);
  const [showScanModal, setShowScanModal] = useState(false);
  const [showServiceNowModal, setShowServiceNowModal] = useState(false);

  // Detecta largura disponível no header do painel esquerdo para modo compacto
  const titleBarRef = useRef<HTMLDivElement>(null);
  const [compactButtons, setCompactButtons] = useState(false);
  useEffect(() => {
    const el = titleBarRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      setCompactButtons(entry.contentRect.width < 270);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const [showSuggestions, setShowSuggestions] = useState(false);  // Mostrar dropdown de sugestões
  const [deploymentSelected, setDeploymentSelected] = useState(false);  // ✅ Rastreia se usuário selecionou do dropdown
  const [autoScanRunning, setAutoScanRunning] = useState(false);
  const autoScanTriggeredRef = React.useRef(false);

  // ✨ NOVO: Estado para lote de comparações (com persistência em localStorage)
  const [comparisonBatch, setComparisonBatch] = useState<ComparisonItem[]>(() => {
    const saved = localStorage.getItem('github-comparison-batch');
    return saved ? JSON.parse(saved) : [];
  });
  const [selectedComparison, setSelectedComparison] = useState<string | null>(() => {
    return localStorage.getItem('github-selected-comparison') || null;
  });
  const [serviceNowCHG, setServiceNowCHG] = useState<string | null>(null); // CHG do ServiceNow
  const [editingProductionTag, setEditingProductionTag] = useState<Record<string, string>>({}); // Edição inline da productionTag
  const [approvalUrlInput, setApprovalUrlInput] = useState(""); // Input manual de URL devstartcd
  const [showApprovalInput, setShowApprovalInput] = useState(false); // Exibir campo de input manual

  // ✅ Persistir lote no localStorage sempre que mudar
  React.useEffect(() => {
    localStorage.setItem('github-comparison-batch', JSON.stringify(comparisonBatch));
  }, [comparisonBatch]);

  // ✅ Persistir comparação selecionada no localStorage
  React.useEffect(() => {
    if (selectedComparison) {
      localStorage.setItem('github-selected-comparison', selectedComparison);
    } else {
      localStorage.removeItem('github-selected-comparison');
    }
  }, [selectedComparison]);

  // ✅ Identificar todos os clusters disponíveis (utilizados no scan automático)
  const scanClusters = React.useMemo(() => {
    return availableClusters.map((cluster) => cluster.context).filter(Boolean);
  }, [availableClusters]);

  // Executar scan automático (1x por dia) ao abrir a aba
  const runAutoScan = React.useCallback(async () => {
    if (scanClusters.length === 0) {
      return;
    }

    // Marcar timestamp ANTES de iniciar — evita re-disparo se a aba for fechada e reaberta
    // durante o scan (erro de VPN em clusters HLG não deve anular o registro do dia)
    localStorage.setItem("github-deployments-last-scan", Date.now().toString());

    setAutoScanRunning(true);
    const toastId = "github-auto-scan";

    toast.loading(`Executando scan automático (${scanClusters.length} cluster(s))...`, { id: toastId });

    const failures: string[] = [];

    for (const clusterName of scanClusters) {
      try {
        const response = await fetch(`/api/v1/github/deployments/scan?cluster=${encodeURIComponent(clusterName)}`, {
          method: "POST",
          headers: {
            'Authorization': `Bearer ${localStorage.getItem("auth_token")}`,
          },
        });

        if (!response.ok) {
          const details = await response.json().catch(() => ({}));
          throw new Error(details?.error || `HTTP ${response.status}`);
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : "Erro desconhecido";
        failures.push(`${clusterName}: ${message}`);
      }
    }

    if (failures.length === 0) {
      toast.success("Scan automático concluído com sucesso", { id: toastId });
    } else {
      toast.warning(`Scan automático: ${failures.length} cluster(s) inacessível(is) (VPN?)`, {
        id: toastId,
        description: failures.slice(0, 3).join(" • "),
      });
    }

    queryClient.invalidateQueries({ queryKey: ['github-deployments-all'] });
    setAutoScanRunning(false);
  }, [scanClusters, queryClient]);

  React.useEffect(() => {
    if (autoScanRunning) {
      return;
    }

    if (!scanClusters.length) {
      return;
    }

    if (autoScanTriggeredRef.current) {
      return;
    }

    const lastScan = localStorage.getItem("github-deployments-last-scan");
    const lastScanTime = lastScan ? parseInt(lastScan, 10) : 0;
    const now = Date.now();
    const twentyFourHours = 24 * 60 * 60 * 1000;

    if (!lastScanTime || now - lastScanTime >= twentyFourHours) {
      autoScanTriggeredRef.current = true;
      runAutoScan().catch((error) => {
        console.error("[GitHubReleasesTab] Auto scan failed:", error);
        toast.error("Falha ao executar scan automático");
        setAutoScanRunning(false);
      });
    }
  }, [scanClusters, autoScanRunning, runAutoScan]);

  // ✅ Query para buscar versão em produção assim que digitar o nome do deployment
  const { data: productionData, isLoading: isLoadingProduction, error: productionError, refetch: refetchProduction } = useQuery<{
    deployment: string;
    namespace: string;
    cluster: string;
    version: string;
    image: string;
    status: string;
    created_at: string;      // ✅ Data de criação real do deployment
    last_seen: string;       // Data do último scan
    squad: string;
    servicenow_task: string;
  }>({
    queryKey: ['github-production-version', deploymentName],
    queryFn: async () => {
      const response = await fetch(
        `/api/v1/github/deployments/production?app_name=${encodeURIComponent(deploymentName)}`,
        {
          headers: {
            'Authorization': `Bearer ${localStorage.getItem("auth_token")}`
          }
        }
      );

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      return response.json();
    },
    // ✅ CORRIGIDO: Só busca se deployment foi SELECIONADO do dropdown (não enquanto digita)
    enabled: deploymentName.length >= 3 && deploymentSelected,
  });

  // ✅ Query para buscar deployments na base (busca geral)
  const { data: searchResults, isLoading: isSearching, refetch: refetchDeployments } = useQuery<{ deployments: DeploymentRecord[] }>({
    queryKey: ['github-deployments-all'],
    queryFn: async () => {
      const response = await fetch(
        `/api/v1/github/deployments/registry`,
        {
          headers: {
            'Authorization': `Bearer ${localStorage.getItem("auth_token")}`
          }
        }
      );

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      return response.json();
    },
    staleTime: 60000, // Cache por 1 minuto
  });

  // Query para comparar releases no GitHub
  const { data: comparisonData, isLoading: isComparing, error: compareError, refetch: refetchComparison } = useQuery<ComparisonResult>({
    queryKey: ['github-compare', githubRepo, productionTag, newTag],
    queryFn: async () => {
      // Construir URL de comparação no formato: <org>/<repo>/compare/<base>...<head>
      const owner = apiClient.getGitHubOrg();
      const base = productionTag.replace(/^v/, ''); // Remove 'v' se existir
      const head = newTag.replace(/^v/, '');
      
      const githubEmail = localStorage.getItem('github_email');
      const headers: Record<string, string> = {
        'Authorization': `Bearer ${localStorage.getItem("auth_token")}`
      };

      // Adicionar X-GitHub-Email se existir
      if (githubEmail) {
        headers['X-GitHub-Email'] = githubEmail;
      }

      const response = await fetch(
        `/api/v1/github/repos/${owner}/${githubRepo}/compare/${base}...${head}`,
        { headers }
      );

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`HTTP ${response.status}: ${errorText}`);
      }

      const data = await response.json();
      console.log('[GitHubReleasesTab] Raw response from API:', data);
      console.log('[GitHubReleasesTab] files_changed:', data.files_changed);
      console.log('[GitHubReleasesTab] commits:', data.commits);
      return data;
    },
    enabled: searchTriggered && !!githubRepo && !!productionTag && !!newTag,
  });

  const handleSearch = () => {
    if (githubRepo && productionTag && newTag) {
      setSearchTriggered(true);
      refetchComparison();
    }
  };

  // ✨ NOVO: Adicionar comparação ao lote
  const handleAddToBatch = () => {
    // ✅ CORRIGIDO: deploymentName é opcional (apenas busca), não obrigatório
    if (!githubRepo || !productionTag || !newTag) {
      return;
    }

    // ✨ Coleta dual de CHG: API + ServiceNow
    let chgNumber: string | undefined = undefined;

    // Prioridade 1: CHG do ServiceNow modal (mais recente)
    if (serviceNowCHG) {
      chgNumber = serviceNowCHG;
      console.log('[GitHubReleasesTab] CHG do ServiceNow:', serviceNowCHG);
    }
    // Prioridade 2: CHG da API de produção
    else if (productionData?.servicenow_task) {
      chgNumber = productionData.servicenow_task;
      console.log('[GitHubReleasesTab] CHG da API:', productionData.servicenow_task);
    }

    console.log('[GitHubReleasesTab] Debug CHG final:', {
      serviceNowCHG: serviceNowCHG,
      apiCHG: productionData?.servicenow_task,
      finalCHG: chgNumber,
      deploymentName: deploymentName
    });

    const newItem: ComparisonItem = {
      id: `${Date.now()}-${githubRepo}`,
      deploymentName: deploymentName || undefined,  // Opcional
      githubRepo,
      productionTag,
      newTag,
      chgNumber: chgNumber,  // CHG coletado de fonte dual
      status: 'pending',
    };

    console.log('[GitHubReleasesTab] New item with CHG:', newItem);

    setComparisonBatch(prev => [...prev, newItem]);

    // Limpar campos após adicionar
    setDeploymentName("");
    setGithubRepo("");
    setProductionTag("");
    setNewTag("");
    setDeploymentSelected(false);  // ✅ Reset estado de seleção

    // ✅ Invalidar cache da query de produção para permitir nova busca
    queryClient.invalidateQueries({ queryKey: ['github-production-version'] });
  };

  // ✨ NOVO: Remover comparação do lote
  const handleRemoveFromBatch = (id: string) => {
    setComparisonBatch(prev => prev.filter(item => item.id !== id));
    if (selectedComparison === id) {
      setSelectedComparison(null);
    }
  };

  // ✨ NOVO: Executar uma comparação específica
  const handleExecuteComparison = async (item: ComparisonItem) => {
    // Atualizar status para loading
    setComparisonBatch(prev => prev.map(i =>
      i.id === item.id ? { ...i, status: 'loading', error: undefined, errorType: undefined } : i
    ));

    try {
      const owner = apiClient.getGitHubOrg();
      const base = item.productionTag.replace(/^v/, '');
      const head = item.newTag.replace(/^v/, '');

      if (!base) {
        setComparisonBatch(prev => prev.map(i =>
          i.id === item.id ? {
            ...i,
            status: 'error',
            error: 'Versão em produção não identificada. Clique no ícone de edição para informar a versão atual.',
            errorType: 'unknown'
          } : i
        ));
        toast.error('Versão em produção não identificada', {
          description: 'Informe a versão atual em produção clicando no campo de edição no item do lote.',
        });
        return;
      }

      const githubEmail = localStorage.getItem('github_email');
      const headers: Record<string, string> = {
        'Authorization': `Bearer ${localStorage.getItem("auth_token")}`
      };

      if (githubEmail) {
        headers['X-GitHub-Email'] = githubEmail;
      }

      const response = await fetch(
        `/api/v1/github/repos/${owner}/${item.githubRepo}/compare/${base}...${head}`,
        { headers }
      );

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));

        // ✅ Detectar erro SAML específico
        if (errorData.error_type === 'saml_authorization_required') {
          const samlError = errorData as SAMLError;

          // Mostrar notificação especial para SAML
          toast.error('Token precisa ser re-autorizado para SAML SSO', {
            description: samlError.message,
            duration: 10000,
            action: {
              label: 'Abrir GitHub',
              onClick: () => window.open(samlError.github_settings_url, '_blank'),
            },
          });

          setComparisonBatch(prev => prev.map(i =>
            i.id === item.id ? {
              ...i,
              status: 'error',
              error: samlError.message,
              errorType: 'saml_authorization_required'
            } : i
          ));
          return;
        }

        throw new Error(errorData.error || `HTTP ${response.status}`);
      }

      const data = await response.json();

      // Atualizar com sucesso
      setComparisonBatch(prev => prev.map(i =>
        i.id === item.id ? { ...i, status: 'success', result: data, error: undefined, errorType: undefined } : i
      ));

      // Selecionar automaticamente
      setSelectedComparison(item.id);

      toast.success(`Comparação concluída: ${item.githubRepo}`);
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Erro desconhecido';

      // Atualizar com erro
      setComparisonBatch(prev => prev.map(i =>
        i.id === item.id ? {
          ...i,
          status: 'error',
          error: errorMessage,
          errorType: 'unknown'
        } : i
      ));

      toast.error(`Erro ao comparar: ${item.githubRepo}`, {
        description: errorMessage,
      });
    }
  };

  // ✨ NOVO: Executar todas as comparações
  const handleExecuteAll = async () => {
    for (const item of comparisonBatch.filter(i => i.status === 'pending' || i.status === 'error')) {
      await handleExecuteComparison(item);
    }
  };

  // ✨ NOVO: Limpar lote
  const handleClearBatch = () => {
    setComparisonBatch([]);
    setSelectedComparison(null);
  };

  // Calcular age do deployment
  const calculateAge = (dateString: string): string => {
    if (!dateString) return '-';

    const date = new Date(dateString);
    const now = new Date();

    // ✅ Validar se data é válida (não é 1970-01-01, não é inválida, não é futura)
    if (isNaN(date.getTime()) || date.getFullYear() < 2020 || date > now) {
      return '-';
    }

    const diffMs = now.getTime() - date.getTime();
    const totalDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    // ✅ Formatar com anos, meses e dias
    if (totalDays >= 365) {
      const years = Math.floor(totalDays / 365);
      const remainingDays = totalDays % 365;
      const months = Math.floor(remainingDays / 30);
      if (months > 0) {
        return `${years}a ${months}m`;
      }
      return `${years}a`;
    }

    if (totalDays >= 30) {
      const months = Math.floor(totalDays / 30);
      const days = totalDays % 30;
      if (days > 0) {
        return `${months}m ${days}d`;
      }
      return `${months}m`;
    }

    if (totalDays > 0) {
      const hours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
      if (hours > 0) {
        return `${totalDays}d ${hours}h`;
      }
      return `${totalDays}d`;
    }

    const hours = Math.floor(diffMs / (1000 * 60 * 60));
    if (hours > 0) {
      const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
      if (minutes > 0) {
        return `${hours}h ${minutes}m`;
      }
      return `${hours}h`;
    }

    const minutes = Math.floor(diffMs / (1000 * 60));
    return `${minutes}m`;
  };

  // ✅ CORRIGIDO: Função para buscar dados (refetch silencioso)
  const handleRefetch = async () => {
    toast.loading('Atualizando dados...', { id: 'refetch-toast' });

    try {
      // Atualizar lista de deployments
      await refetchDeployments();

      // Atualizar dados de produção (se deployment selecionado)
      if (deploymentName && deploymentSelected) {
        await refetchProduction();
      }

      // Atualizar comparação (se houver)
      if (searchTriggered && githubRepo && productionTag && newTag) {
        await refetchComparison();
      }

      toast.success('Dados atualizados com sucesso', { id: 'refetch-toast' });
    } catch (error) {
      toast.error('Erro ao atualizar dados', { id: 'refetch-toast' });
    }
  };

  // Handler para dados importados do ServiceNow → adiciona direto às comparações
  const handleServiceNowImport = async (data: {
    deploymentName: string;
    githubRepo: string;
    newVersion: string;
    xlReleaseUrl?: string;
    changeNumber?: string;
    approvalUrl?: string;
  }) => {
    // Buscar tag de produção via API
    let prodTag = '';
    if (data.deploymentName) {
      try {
        const response = await fetch(
          `/api/v1/github/deployments/production?app_name=${encodeURIComponent(data.deploymentName)}`,
          {
            headers: {
              'Authorization': `Bearer ${localStorage.getItem("auth_token")}`
            }
          }
        );
        if (response.ok) {
          const prodData = await response.json();
          prodTag = prodData.version || '';
        }
      } catch {
        // Se falhar, adiciona sem tag de produção (usuário pode editar depois)
      }
    }

    // ✨ Capturar CHG do ServiceNow para uso posterior
    if (data.changeNumber) {
      setServiceNowCHG(data.changeNumber);
      console.log('[GitHubReleasesTab] CHG capturado do ServiceNow:', data.changeNumber);
    }

    // Fallback automático: quando ServiceNow não extrai o repo, usa o deploymentName
    const resolvedRepo = data.githubRepo || data.deploymentName || '';

    const newItem: ComparisonItem = {
      id: `${Date.now()}-${resolvedRepo || 'snow'}`,
      deploymentName: data.deploymentName || undefined,
      githubRepo: resolvedRepo,
      productionTag: prodTag,
      newTag: data.newVersion,
      chgNumber: data.changeNumber || undefined,
      approvalUrl: data.approvalUrl || undefined,
      approvalStatus: data.approvalUrl ? 'checking' : undefined,
      status: 'pending',
    };

    setComparisonBatch(prev => [...prev, newItem]);

    // Pré-verificar status de aprovação no devstartcd assim que a CHG é importada
    if (newItem.approvalUrl) {
      const itemId = newItem.id;
      const url = newItem.approvalUrl;
      apiClient.getSreApprovalInfo(url).then(res => {
        if (!res.success || !res.approval_info) {
          // Backend retornou erro (URL inválida, devstartcd inacessível, etc.)
          setComparisonBatch(prev => prev.map(i =>
            i.id === itemId ? { ...i, approvalStatus: 'error' } : i
          ));
          return;
        }
        const info = res.approval_info;
        let approvalStatus: ComparisonItem['approvalStatus'] = 'pending';
        let approverEmail = '';
        if (info.is_finalized || info.status === 'FINALIZED') {
          approvalStatus = 'finalized';
          approverEmail = info.approver_email || '';
        } else if (info.status === 'APPROVED') {
          approvalStatus = 'approved';
          approverEmail = info.approver_email || '';
        }
        setComparisonBatch(prev => prev.map(i =>
          i.id === itemId ? { ...i, approvalStatus, approverEmail } : i
        ));
      }).catch(() => {
        setComparisonBatch(prev => prev.map(i =>
          i.id === itemId ? { ...i, approvalStatus: 'error' } : i
        ));
      });
    }
  };

  // ✅ CORRIGIDO: Auto-preencher tag em produção quando encontrar o deployment
  // Só preenche se o campo estiver vazio (evita repreencher após limpar)
  React.useEffect(() => {
    if (productionData?.version && !productionTag) {
      setProductionTag(productionData.version);
    }
  }, [productionData, productionTag]);

  // Re-verificar aprovação para itens já no batch com approvalUrl mas sem status definitivo.
  // Cobre itens persistidos no localStorage antes desta funcionalidade existir.
  React.useEffect(() => {
    // Re-verificar tudo que não está definitivamente resolvido (inclui 'pending' do código antigo)
    const toCheck = comparisonBatch.filter(i =>
      i.approvalUrl && i.approvalStatus !== 'finalized' && i.approvalStatus !== 'approved'
    );
    if (toCheck.length === 0) return;

    // Marcar como checking
    setComparisonBatch(prev => prev.map(i =>
      toCheck.some(c => c.id === i.id) ? { ...i, approvalStatus: 'checking' } : i
    ));

    // Verificar em paralelo (com pequeno delay entre cada para não sobrecarregar o backend)
    toCheck.forEach((item, idx) => {
      const itemId = item.id;
      const url = item.approvalUrl!;
      setTimeout(() => {
        apiClient.getSreApprovalInfo(url).then(res => {
          if (!res.success || !res.approval_info) {
            setComparisonBatch(prev => prev.map(i =>
              i.id === itemId ? { ...i, approvalStatus: 'error' } : i
            ));
            return;
          }
          const info = res.approval_info;
          let approvalStatus: ComparisonItem['approvalStatus'] = 'pending';
          let approverEmail = '';
          if (info.is_finalized || info.status === 'FINALIZED') {
            approvalStatus = 'finalized';
            approverEmail = info.approver_email || '';
          } else if (info.status === 'APPROVED') {
            approvalStatus = 'approved';
            approverEmail = info.approver_email || '';
          }
          setComparisonBatch(prev => prev.map(i =>
            i.id === itemId ? { ...i, approvalStatus, approverEmail } : i
          ));
        }).catch(() => {
          setComparisonBatch(prev => prev.map(i =>
            i.id === itemId ? { ...i, approvalStatus: 'error' } : i
          ));
        });
      }, idx * 300); // 300ms entre cada chamada
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // só na montagem do componente

  // ✨ NOVO: Determinar qual resultado mostrar (lote ou comparação individual)
  const displayedComparison = React.useMemo(() => {
    if (selectedComparison) {
      const batchItem = comparisonBatch.find(i => i.id === selectedComparison);
      return batchItem?.result || null;
    }
    return comparisonData || null;
  }, [selectedComparison, comparisonBatch, comparisonData]);

  // ✨ NOVO: Item selecionado do lote (para verificar erros)
  const selectedBatchItem = React.useMemo(() => {
    if (selectedComparison) {
      return comparisonBatch.find(i => i.id === selectedComparison) || null;
    }
    return null;
  }, [selectedComparison, comparisonBatch]);

  // Reset do input manual de URL de aprovação ao trocar de item
  React.useEffect(() => {
    setShowApprovalInput(false);
    setApprovalUrlInput("");
  }, [selectedComparison]);

  const saveApprovalUrl = () => {
    if (!approvalUrlInput.trim() || !selectedBatchItem) return;
    setComparisonBatch(prev => prev.map(i =>
      i.id === selectedBatchItem.id ? { ...i, approvalUrl: approvalUrlInput.trim() } : i
    ));
    setShowApprovalInput(false);
    setApprovalUrlInput("");
  };

  // ✨ Normalizar versão de x-x-x-x para x.x.x-x (formato semver)
  const normalizeVersion = (version: string): string => {
    if (!version) return version;
    // Remove prefixo "v" se existir
    let v = version.replace(/^v/, '');
    // Contar hífens
    const hyphens = (v.match(/-/g) || []).length;
    if (hyphens >= 3) {
      // Formato: x-x-x-x → x.x.x-x
      const parts = v.split('-');
      if (parts.length >= 4) {
        return `${parts[0]}.${parts[1]}.${parts[2]}-${parts.slice(3).join('-')}`;
      }
    } else if (hyphens === 2 && !v.includes('.')) {
      // Formato: x-x-x → x.x.x
      v = v.replace(/-/g, '.');
    }
    return v;
  };

  // ✨ Filtrar sugestões de deployments baseado no texto digitado
  // ✅ CORRIGIDO: Busca apenas por substring contígua (não divide em termos)
  const filteredSuggestions = React.useMemo(() => {
    if (!deploymentName || deploymentName.trim().length === 0) return [];
    if (!searchResults?.deployments) return [];

    const searchLower = deploymentName.toLowerCase().trim();

    return searchResults.deployments.filter((deployment) => {
      const nameLower = deployment.deployment_name.toLowerCase();
      // ✅ Busca APENAS por substring contígua - "order-api" só encontra nomes com "order-api"
      return nameLower.includes(searchLower);
    });
  }, [deploymentName, searchResults?.deployments]);

  // Filtrar arquivos por extensão
  const filteredFiles = React.useMemo(() => {
    const files = displayedComparison?.files_changed || [];
    console.log('[GitHubReleasesTab] Total files from API:', files.length);
    console.log('[GitHubReleasesTab] Show all files:', showAllFiles);

    const filtered = files.filter(file => {
      if (showAllFiles) return true;

      const filename = file.filename.toLowerCase();
      return (
        filename.endsWith('.yml') ||
        filename.endsWith('.yaml') ||
        filename.endsWith('.md') ||
        filename.includes('dockerfile')
      );
    });

    console.log('[GitHubReleasesTab] Filtered files:', filtered.length);
    return filtered;
  }, [displayedComparison?.files_changed, showAllFiles]);

  return (
    <>
      <SplitView
        leftPanel={{
          title: "Configuração",
          titleAction: (
            <div ref={titleBarRef} className="flex gap-1.5 min-w-0">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowServiceNowModal(true)}
                title="Importar dados de uma CHG do ServiceNow"
                className={compactButtons ? "h-7 w-7 p-0" : "h-7"}
              >
                <Download className="h-3.5 w-3.5 flex-shrink-0" />
                {!compactButtons && <span className="ml-1.5">ServiceNow</span>}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleRefetch}
                title="Recarregar dados da base"
                className={compactButtons ? "h-7 w-7 p-0" : "h-7"}
              >
                <Search className="h-3.5 w-3.5 flex-shrink-0" />
                {!compactButtons && <span className="ml-1.5">Refetch</span>}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowScanModal(true)}
                title="Escanear repositórios"
                className={compactButtons ? "h-7 w-7 p-0" : "h-7"}
              >
                <Play className="h-3.5 w-3.5 flex-shrink-0" />
                {!compactButtons && <span className="ml-1.5">Scan</span>}
              </Button>
            </div>
          ),
          content: (
            <div className="h-full flex flex-col space-y-4 p-4">
              {/* Campo 1: Nome do Deployment na Base - COM AUTOCOMPLETE */}
              <div className="space-y-2">
                <Label htmlFor="deployment-name">Nome do Deployment (Base de Dados)</Label>
                <div className="relative">
                  <Input
                    id="deployment-name"
                    placeholder="Ex: vv-geolocalizacao-api"
                    value={deploymentName}
                    onChange={(e) => {
                      setDeploymentName(e.target.value);
                      setShowSuggestions(true);
                      // ✅ Reset seleção quando usuário digita (força selecionar do dropdown)
                      setDeploymentSelected(false);
                      setProductionTag("");  // Limpa tag auto-preenchida
                    }}
                    onFocus={() => setShowSuggestions(true)}
                    onBlur={() => {
                      // Delay para permitir click na sugestão
                      setTimeout(() => setShowSuggestions(false), 200);
                    }}
                    autoComplete="off"
                  />
                  {/* Dropdown de sugestões */}
                  {showSuggestions && filteredSuggestions.length > 0 && (
                    <div className="absolute z-50 w-full mt-1 bg-popover border border-border rounded-md shadow-lg max-h-[300px] overflow-auto">
                      <div className="p-1">
                        <p className="text-[10px] text-muted-foreground px-2 py-1 border-b mb-1">
                          {filteredSuggestions.length} workload(s) encontrado(s)
                        </p>
                        {filteredSuggestions.map((suggestion, index) => {
                          const kind = suggestion.resource_kind || "Deployment";
                          const kindColors: Record<string, string> = {
                            Deployment: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
                            CronJob: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
                            StatefulSet: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
                            DaemonSet: "bg-orange-100 text-orange-700 dark:bg-orange-900 dark:text-orange-300",
                          };
                          return (
                          <div
                            key={`${suggestion.deployment_name}-${index}`}
                            className="flex items-center justify-between px-2 py-1.5 text-sm hover:bg-muted rounded cursor-pointer"
                            onClick={() => {
                              setDeploymentName(suggestion.deployment_name);
                              setShowSuggestions(false);
                              // ✅ Marca que deployment foi selecionado explicitamente
                              setDeploymentSelected(true);
                            }}
                          >
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-1.5">
                                <p className="font-medium truncate">{suggestion.deployment_name}</p>
                                <span className={`text-[9px] px-1 py-0.5 rounded font-medium shrink-0 ${kindColors[kind] || kindColors["Deployment"]}`}>
                                  {kind}
                                </span>
                              </div>
                              <p className="text-[10px] text-muted-foreground truncate">
                                {suggestion.cluster} / {suggestion.namespace}
                              </p>
                            </div>
                            <Badge variant="secondary" className="ml-2 text-[10px] shrink-0">
                              {normalizeVersion(suggestion.version)}
                            </Badge>
                          </div>
                          );
                        })}
                      </div>
                    </div>
                  )}
                  {/* Indicador de carregamento dos deployments */}
                  {isSearching && (
                    <div className="absolute right-3 top-1/2 -translate-y-1/2">
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                    </div>
                  )}
                </div>
                {isLoadingProduction && deploymentName.length >= 3 && deploymentSelected && (
                  <p className="text-xs text-muted-foreground flex items-center gap-2">
                    <Search className="h-3 w-3 animate-pulse" />
                    Buscando na base de dados...
                  </p>
                )}
                {productionError && deploymentName.length >= 3 && deploymentSelected && (
                  <Alert variant="destructive" className="py-2">
                    <AlertCircle className="h-4 w-4" />
                    <AlertTitle className="text-sm">Deployment não encontrado</AlertTitle>
                    <AlertDescription className="text-xs">
                      Não foi encontrado deployment "{deploymentName}" em produção.
                      Execute o Scan primeiro.
                    </AlertDescription>
                  </Alert>
                )}
                {/* ✅ Mensagem quando há sugestões e usuário ainda não selecionou */}
                {filteredSuggestions.length > 0 && !deploymentSelected && deploymentName.length >= 2 && (
                  <p className="text-xs text-blue-600 dark:text-blue-400 flex items-center gap-1">
                    <Info className="h-3 w-3" />
                    {filteredSuggestions.length} workload(s) encontrado(s) - selecione um da lista acima
                  </p>
                )}
                {/* ✅ Mensagem quando não há sugestões e não foi selecionado */}
                {filteredSuggestions.length === 0 && !deploymentSelected && deploymentName.length >= 3 && !isSearching && (
                  <p className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1">
                    <AlertCircle className="h-3 w-3" />
                    Nenhum workload encontrado com "{deploymentName}". Execute o Scan.
                  </p>
                )}
              </div>

              {/* ✅ Painel de Informações da Release em Produção - Só mostra quando selecionado do dropdown */}
              {productionData && deploymentSelected && (
                <>
                  <Card className="border-green-500/50 bg-green-50 dark:bg-green-950/40">
                    <CardHeader className="pb-3">
                      <CardTitle className="text-sm flex items-center gap-2 text-green-700 dark:text-green-400">
                        <Database className="h-4 w-4" />
                        Release em Produção
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2 text-xs">
                      <div className="grid grid-cols-2 gap-x-4 gap-y-2">
                        <div>
                          <span className="text-muted-foreground">Cluster:</span>
                          <p className="font-mono font-medium mt-0.5 text-foreground">{productionData.cluster}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Namespace:</span>
                          <p className="font-mono font-medium mt-0.5 text-foreground">{productionData.namespace}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Deployment:</span>
                          <p className="font-medium mt-0.5 text-foreground">{productionData.deployment}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Versão Atual:</span>
                          <Badge variant="secondary" className="font-mono mt-0.5">
                            {productionData.version}
                          </Badge>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Squad:</span>
                          <p className="mt-0.5 text-foreground">{productionData.squad || '-'}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">ServiceNow CHG:</span>
                          <p className="font-mono mt-0.5 text-foreground">{productionData.servicenow_task || '-'}</p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Criado em:</span>
                          <p className="mt-0.5 text-[10px] text-foreground">
                            {productionData.created_at ? new Date(productionData.created_at).toLocaleString('pt-BR', {
                              day: '2-digit',
                              month: '2-digit',
                              year: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit'
                            }) : '-'}
                          </p>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Age (Tempo de Vida):</span>
                          <Badge variant="outline" className="mt-0.5">
                            {productionData.created_at ? calculateAge(productionData.created_at) : '-'}
                          </Badge>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Último Scan:</span>
                          <p className="mt-0.5 text-[10px] text-foreground">
                            {new Date(productionData.last_seen).toLocaleString('pt-BR', {
                              day: '2-digit',
                              month: '2-digit',
                              year: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit'
                            })}
                          </p>
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                  <Separator />
                </>
              )}

              {/* Campo 2: Nome do Repositório GitHub */}
              <div>
                <Label htmlFor="github-repo">Repositório GitHub</Label>
                <Input
                  id="github-repo"
                  placeholder="Ex: vv-retira-geolocalizacao"
                  value={githubRepo}
                  onChange={(e) => setGithubRepo(e.target.value)}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Nome do repositório em github.com/{apiClient.getGitHubOrg()}/
                </p>
              </div>

              <Separator />

              {/* Campo 3: Tag em Produção (auto-preenchido) */}
              <div>
                <Label htmlFor="production-tag">Tag em Produção</Label>
                <Input
                  id="production-tag"
                  placeholder="Ex: 2.5.5-2"
                  value={productionTag}
                  onChange={(e) => setProductionTag(e.target.value)}
                />
                {productionData?.version && (
                  <p className="text-xs text-muted-foreground mt-1">
                    ✅ Auto-preenchido com a versão encontrada
                  </p>
                )}
              </div>

              {/* Campo 4: Tag da Nova Release */}
              <div>
                <Label htmlFor="new-tag">Tag da Nova Release</Label>
                <Input
                  id="new-tag"
                  placeholder="Ex: 2.5.8-1"
                  value={newTag}
                  onChange={(e) => setNewTag(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                />
              </div>

              {/* Botões de ação */}
              <div className="flex gap-2">
                <Button
                  onClick={handleSearch}
                  disabled={!githubRepo || !productionTag || !newTag || isComparing}
                  className="flex-1"
                  variant="default"
                >
                  <GitCompare className="h-4 w-4 mr-2" />
                  {isComparing ? 'Comparando...' : 'Comparar Agora'}
                </Button>
                <Button
                  onClick={handleAddToBatch}
                  disabled={!githubRepo || !productionTag || !newTag}
                  variant="outline"
                  title="Adicionar ao lote de comparações"
                >
                  <Plus className="h-4 w-4" />
                </Button>
              </div>

              {/* Alerta informativo */}
              <Alert>
                <Info className="h-4 w-4" />
                <AlertTitle>Como usar</AlertTitle>
                <AlertDescription className="text-xs space-y-2">
                  <ol className="list-decimal list-inside space-y-1 text-muted-foreground">
                    <li>Configure seu token GitHub (botão Token)</li>
                    <li>Execute Scan dos clusters (botão Scan)</li>
                    <li>Digite o nome do deployment (ex: vv-geolocalizacao-api)</li>
                    <li>Digite o nome do repositório GitHub (ex: vv-retira-geolocalizacao)</li>
                    <li>Confirme/ajuste a tag em produção</li>
                    <li>Digite a nova tag e clique em <strong>Comparar Agora</strong> ou <strong>+ (Adicionar ao Lote)</strong></li>
                  </ol>
                </AlertDescription>
              </Alert>

              {/* ✨ NOVO: Lote de Comparações */}
              {comparisonBatch.length > 0 && (
                <>
                  <Separator />
                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label className="text-sm font-semibold">
                        Lote de Comparações ({comparisonBatch.length})
                      </Label>
                      <div className="flex gap-2">
                        <Button
                          size="sm"
                          variant="default"
                          onClick={handleExecuteAll}
                          disabled={comparisonBatch.every(i => i.status === 'success')}
                        >
                          <Play className="h-3 w-3 mr-1" />
                          Executar Todas
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={handleClearBatch}
                        >
                          <Trash2 className="h-3 w-3 mr-1" />
                          Limpar
                        </Button>
                      </div>
                    </div>

                    <ScrollArea className="h-[300px]">
                      <div className="space-y-2">
                        {comparisonBatch.map((item) => {
                          console.log('[GitHubReleasesTab] Rendering item:', { id: item.id, chgNumber: item.chgNumber });
                          return (
                          <Card
                            key={item.id}
                            className={`cursor-pointer transition-colors ${
                              selectedComparison === item.id
                                ? 'border-primary bg-primary/5'
                                : 'hover:bg-muted/50'
                            }`}
                            onClick={() => setSelectedComparison(item.id)}
                          >
                            <CardContent className="p-3">
                              <div className="flex items-start justify-between gap-2">
                                <div className="flex-1 min-w-0">
                                  <p className="text-sm font-medium truncate">
                                    {item.deploymentName}
                                  </p>
                                  <div className="flex items-center justify-between gap-3 flex-wrap">
                                    <p className="text-xs text-muted-foreground truncate flex-shrink-0">
                                      {item.githubRepo}
                                    </p>
                                    {item.chgNumber && (
                                      <div className="flex items-center gap-1 flex-shrink-0">
                                        <span className="text-[10px] font-mono text-white bg-blue-600 dark:bg-blue-700 px-2 py-1 rounded-md border border-blue-700 dark:border-blue-600">
                                          CHG: {item.chgNumber}
                                        </span>
                                        {item.approvalUrl && (
                                          <>
                                            {item.approvalStatus === 'checking' && (
                                              <span title="Verificando aprovação...">
                                                <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                                              </span>
                                            )}
                                            {(item.approvalStatus === 'finalized' || item.approvalStatus === 'approved') && (
                                              <span title={item.approverEmail ? `Já aprovado por: ${item.approverEmail}` : 'Já aprovado'}>
                                                <ShieldCheck className="h-3.5 w-3.5 text-green-500" />
                                              </span>
                                            )}
                                            {item.approvalStatus === 'pending' && (
                                              <span title="Aguardando aprovação SRE">
                                                <ShieldAlert className="h-3.5 w-3.5 text-yellow-500" />
                                              </span>
                                            )}
                                            {item.approvalStatus === 'error' && (
                                              <span title="Erro ao verificar aprovação">
                                                <ShieldAlert className="h-3.5 w-3.5 text-red-400" />
                                              </span>
                                            )}
                                          </>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                  <div className="flex items-center gap-2 mt-1">
                                    {editingProductionTag[item.id] !== undefined ? (
                                      <div className="flex items-center gap-1">
                                        <Input
                                          className="h-5 text-[10px] w-24 py-0 px-1"
                                          value={editingProductionTag[item.id]}
                                          onChange={(e) => setEditingProductionTag(prev => ({ ...prev, [item.id]: e.target.value }))}
                                          onKeyDown={(e) => {
                                            if (e.key === 'Enter') {
                                              const val = editingProductionTag[item.id].trim();
                                              if (val) {
                                                setComparisonBatch(prev => prev.map(i =>
                                                  i.id === item.id ? { ...i, productionTag: val, status: 'pending', error: undefined, errorType: undefined } : i
                                                ));
                                              }
                                              setEditingProductionTag(prev => { const n = {...prev}; delete n[item.id]; return n; });
                                            } else if (e.key === 'Escape') {
                                              setEditingProductionTag(prev => { const n = {...prev}; delete n[item.id]; return n; });
                                            }
                                          }}
                                          onClick={(e) => e.stopPropagation()}
                                          autoFocus
                                          placeholder="ex: 1.4.58-1"
                                        />
                                        <Button
                                          size="sm" variant="ghost"
                                          className="h-5 w-5 p-0 text-green-600"
                                          onClick={(e) => {
                                            e.stopPropagation();
                                            const val = editingProductionTag[item.id].trim();
                                            if (val) {
                                              setComparisonBatch(prev => prev.map(i =>
                                                i.id === item.id ? { ...i, productionTag: val, status: 'pending', error: undefined, errorType: undefined } : i
                                              ));
                                            }
                                            setEditingProductionTag(prev => { const n = {...prev}; delete n[item.id]; return n; });
                                          }}
                                        >
                                          <Check className="h-3 w-3" />
                                        </Button>
                                      </div>
                                    ) : (
                                      <div className="flex items-center gap-1">
                                        <Badge
                                          variant={item.productionTag ? "outline" : "destructive"}
                                          className="text-[10px] py-0"
                                          title={item.productionTag ? undefined : "Versão atual em PRD não encontrada no registry de deployments.\nEscaneie os clusters na aba GitHub Releases para atualizar.\nOu clique no lápis para informar manualmente."}
                                        >
                                          {item.productionTag || "não encontrada em PRD"}
                                        </Badge>
                                        <Button
                                          size="sm" variant="ghost"
                                          className="h-4 w-4 p-0 text-muted-foreground hover:text-blue-600"
                                          title="Informar versão atual em produção manualmente"
                                          onClick={(e) => {
                                            e.stopPropagation();
                                            setEditingProductionTag(prev => ({ ...prev, [item.id]: item.productionTag }));
                                          }}
                                        >
                                          <Pencil className="h-2.5 w-2.5" />
                                        </Button>
                                      </div>
                                    )}
                                    <span className="text-xs text-muted-foreground">→</span>
                                    <Badge variant="secondary" className="text-[10px] py-0">
                                      {item.newTag}
                                    </Badge>
                                  </div>
                                </div>
                                <div className="flex flex-col items-end gap-1">
                                  {item.status === 'pending' && (
                                    <Button
                                      size="sm"
                                      variant="ghost"
                                      onClick={(e) => {
                                        e.stopPropagation();
                                        handleExecuteComparison(item);
                                      }}
                                      className="h-6 w-6 p-0"
                                      title="Executar comparação"
                                    >
                                      <Play className="h-3 w-3" />
                                    </Button>
                                  )}
                                  {item.status === 'loading' && (
                                    <Loader2 className="h-4 w-4 animate-spin text-blue-600" />
                                  )}
                                  {item.status === 'success' && (
                                    <div className="flex items-center gap-1">
                                      <CheckCircle2 className="h-4 w-4 text-green-600" />
                                      <Button
                                        size="sm"
                                        variant="ghost"
                                        onClick={(e) => {
                                          e.stopPropagation();
                                          handleExecuteComparison(item);
                                        }}
                                        className="h-6 w-6 p-0 text-muted-foreground hover:text-blue-600"
                                        title="Refazer comparação"
                                      >
                                        <RefreshCw className="h-3 w-3" />
                                      </Button>
                                    </div>
                                  )}
                                  {item.status === 'error' && (
                                    <div className="flex items-center gap-1">
                                      <span title={item.errorType === 'saml_authorization_required' ? "Erro SAML - Re-autorizar token" : "Erro na comparação"}>
                                        {item.errorType === 'saml_authorization_required' ? (
                                          <ShieldAlert className="h-4 w-4 text-amber-600" />
                                        ) : (
                                          <XCircle className="h-4 w-4 text-red-600" />
                                        )}
                                      </span>
                                      <Button
                                        size="sm"
                                        variant="ghost"
                                        onClick={(e) => {
                                          e.stopPropagation();
                                          handleExecuteComparison(item);
                                        }}
                                        className="h-6 w-6 p-0 text-blue-600 hover:text-blue-800"
                                        title="Tentar novamente"
                                      >
                                        <RefreshCw className="h-3 w-3" />
                                      </Button>
                                    </div>
                                  )}
                                  <Button
                                    size="sm"
                                    variant="ghost"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      handleRemoveFromBatch(item.id);
                                    }}
                                    className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive"
                                    title="Remover do lote"
                                  >
                                    <Trash2 className="h-3 w-3" />
                                  </Button>
                                </div>
                              </div>
                            </CardContent>
                          </Card>
                          );
                        })}
                      </div>
                    </ScrollArea>
                  </div>
                </>
              )}
            </div>
          ),
        }}
        rightPanel={{
          title: selectedComparison
            ? (() => {
                const comparison = comparisonBatch.find(i => i.id === selectedComparison);
                const deploymentName = comparison?.deploymentName || '';
                const chgNumber = comparison?.chgNumber;
                return `Comparação: ${deploymentName}${chgNumber ? ` (CHG: ${chgNumber})` : ''}`;
              })()
            : "Comparação",
          titleSuffix: selectedBatchItem?.approvalUrl ? (
            <SreApprovalButton
              approvalUrl={selectedBatchItem.approvalUrl}
              chgNumber={selectedBatchItem.chgNumber}
              preCheckedStatus={
                selectedBatchItem.approvalStatus === 'finalized' ? 'finalized' :
                selectedBatchItem.approvalStatus === 'approved' ? 'approved' :
                selectedBatchItem.approvalStatus === 'pending' ? 'pending' : undefined
              }
              preCheckedApproverEmail={selectedBatchItem.approverEmail}
            />
          ) : selectedBatchItem?.chgNumber ? (
            showApprovalInput ? (
              <div className="flex items-center gap-1">
                <Input
                  className="h-6 text-xs w-56 px-2"
                  placeholder="https://devstartcd.via.com.br/..."
                  value={approvalUrlInput}
                  onChange={e => setApprovalUrlInput(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === "Enter") saveApprovalUrl();
                    if (e.key === "Escape") { setShowApprovalInput(false); setApprovalUrlInput(""); }
                  }}
                  autoFocus
                />
                <Button size="icon" variant="ghost" className="h-6 w-6" onClick={saveApprovalUrl}
                  disabled={!approvalUrlInput.includes("devstartcd")}>
                  <Check className="h-3 w-3" />
                </Button>
                <Button size="icon" variant="ghost" className="h-6 w-6"
                  onClick={() => { setShowApprovalInput(false); setApprovalUrlInput(""); }}>
                  <XCircle className="h-3 w-3" />
                </Button>
              </div>
            ) : (
              <Button size="sm" variant="ghost"
                className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground gap-1"
                onClick={() => setShowApprovalInput(true)}
                title="Colar link de aprovação SRE (devstartcd)">
                <ExternalLink className="h-3 w-3" />
                Link SRE
              </Button>
            )
          ) : undefined,
          titleAction: displayedComparison ? (
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <GitCompare className="h-4 w-4" />
                <span>{displayedComparison?.commits?.length || 0} commit(s)</span>
                <span className="text-muted-foreground">•</span>
                <span>{filteredFiles.length} arquivo(s)</span>
              </div>
              <div className="flex items-center gap-2">
                <Label htmlFor="show-all" className="text-xs cursor-pointer">
                  Mostrar todos
                </Label>
                <Switch
                  id="show-all"
                  checked={showAllFiles}
                  onCheckedChange={setShowAllFiles}
                />
              </div>
            </div>
          ) : undefined,
          content: (
            <div className="h-full flex flex-col">
              {!searchTriggered && !displayedComparison && !isComparing && (
                <div className="flex-1 flex items-center justify-center text-center space-y-4 p-8">
                  <div>
                    <GitCompare className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
                    <p className="text-muted-foreground">
                      Digite o nome da release e a nova tag para comparar
                    </p>
                  </div>
                </div>
              )}

              {isComparing && (
                <div className="flex-1 flex items-center justify-center">
                  <div className="text-center space-y-4">
                    <Search className="h-8 w-8 animate-pulse mx-auto text-muted-foreground" />
                    <p className="text-sm text-muted-foreground">Buscando e comparando versões...</p>
                  </div>
                </div>
              )}

              {/* ✅ Erro SAML do item selecionado no lote */}
              {selectedBatchItem?.status === 'error' && selectedBatchItem.errorType === 'saml_authorization_required' && (
                <div className="flex-1 flex items-center justify-center p-8">
                  <Card className="max-w-lg border-amber-500/50 bg-amber-50 dark:bg-amber-950/40">
                    <CardHeader className="pb-3">
                      <CardTitle className="text-sm flex items-center gap-2 text-amber-700 dark:text-amber-400">
                        <ShieldAlert className="h-5 w-5" />
                        Token precisa ser re-autorizado (SAML SSO)
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4">
                      <p className="text-sm text-foreground">
                        Seu token GitHub está válido, mas precisa ser re-autorizado para a organização <strong>{apiClient.getGitHubOrg()}</strong> que usa SAML SSO.
                      </p>
                      <Alert className="border-amber-300 bg-amber-100 dark:bg-amber-900/50">
                        <Info className="h-4 w-4 text-amber-600" />
                        <AlertDescription className="text-xs space-y-1">
                          <p><strong>1.</strong> Acesse as configurações de tokens do GitHub</p>
                          <p><strong>2.</strong> Encontre seu token e clique em "Configure SSO"</p>
                          <p><strong>3.</strong> Procure "{apiClient.getGitHubOrg()}" e clique em "Authorize"</p>
                          <p><strong>4.</strong> Complete a autenticação SSO</p>
                          <p><strong>5.</strong> Volte aqui e clique em "Tentar Novamente"</p>
                        </AlertDescription>
                      </Alert>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => window.open('https://github.com/settings/tokens', '_blank')}
                          className="flex-1"
                        >
                          <ExternalLink className="h-4 w-4 mr-2" />
                          Abrir GitHub Settings
                        </Button>
                        <Button
                          variant="default"
                          size="sm"
                          onClick={() => selectedBatchItem && handleExecuteComparison(selectedBatchItem)}
                          className="flex-1"
                        >
                          <RefreshCw className="h-4 w-4 mr-2" />
                          Tentar Novamente
                        </Button>
                      </div>
                    </CardContent>
                  </Card>
                </div>
              )}

              {/* ✅ Erro genérico do item selecionado no lote */}
              {selectedBatchItem?.status === 'error' && selectedBatchItem.errorType !== 'saml_authorization_required' && (
                <div className="flex-1 flex items-center justify-center p-8">
                  <Card className="max-w-lg">
                    <CardContent className="pt-6 space-y-4">
                      <Alert variant="destructive">
                        <AlertCircle className="h-4 w-4" />
                        <AlertTitle>Erro ao comparar releases</AlertTitle>
                        <AlertDescription className="text-xs">
                          {selectedBatchItem.error || 'Erro desconhecido'}
                        </AlertDescription>
                      </Alert>
                      <Button
                        variant="default"
                        size="sm"
                        onClick={() => handleExecuteComparison(selectedBatchItem)}
                        className="w-full"
                      >
                        <RefreshCw className="h-4 w-4 mr-2" />
                        Tentar Novamente
                      </Button>
                    </CardContent>
                  </Card>
                </div>
              )}

              {/* ✅ Erro da comparação direta (não do lote) */}
              {compareError && !selectedBatchItem && (
                <div className="flex-1 flex items-center justify-center p-8">
                  <Card className="max-w-lg">
                    <CardContent className="pt-6 space-y-4">
                      <Alert variant="destructive">
                        <AlertCircle className="h-4 w-4" />
                        <AlertTitle>Erro ao comparar releases</AlertTitle>
                        <AlertDescription className="text-xs">
                          {compareError instanceof Error ? compareError.message : 'Erro desconhecido'}
                        </AlertDescription>
                      </Alert>
                      <Button
                        variant="default"
                        size="sm"
                        onClick={() => refetchComparison()}
                        className="w-full"
                      >
                        <RefreshCw className="h-4 w-4 mr-2" />
                        Tentar Novamente
                      </Button>
                    </CardContent>
                  </Card>
                </div>
              )}

              {!isComparing && !compareError && displayedComparison && (
                <div className="flex-1 flex flex-col h-full">
                  {/* Barra de comparação compacta */}
                  <div className="px-3 py-1.5 border-b bg-muted/30 flex items-center gap-3 flex-wrap">
                    <span className="text-[11px] font-medium text-muted-foreground flex items-center gap-1 flex-shrink-0">
                      <GitBranch className="h-3 w-3" />
                      Comparando
                    </span>
                    <div className="flex items-center gap-1.5 flex-shrink-0">
                      <Badge variant="outline" className="text-[11px] py-0 px-1.5 h-5">
                        {displayedComparison.base_tag}
                      </Badge>
                      <span className="text-muted-foreground text-[11px]">→</span>
                      <Badge variant="default" className="text-[11px] py-0 px-1.5 h-5">
                        {displayedComparison.head_tag}
                      </Badge>
                    </div>
                    {selectedBatchItem?.chgNumber && (
                      <Badge variant="outline" className="font-mono text-[11px] py-0 px-1.5 h-5 bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-300 border-blue-200 dark:border-blue-800 flex-shrink-0">
                        {selectedBatchItem.chgNumber}
                      </Badge>
                    )}
                    <Separator orientation="vertical" className="h-4 flex-shrink-0" />
                    <div className="flex items-center gap-3 text-[11px] flex-shrink-0">
                      <span className="text-muted-foreground">
                        <span className="font-semibold text-foreground">{displayedComparison?.commits?.length || 0}</span> commit(s)
                      </span>
                      <span className="text-muted-foreground">
                        <span className="font-semibold text-foreground">{displayedComparison?.files_changed?.length || 0}</span> arquivo(s)
                      </span>
                      <span className="text-green-600 font-semibold">
                        +{(displayedComparison?.files_changed || []).reduce((sum, f) => sum + f.additions, 0)}
                      </span>
                      <span className="text-red-600 font-semibold">
                        -{(displayedComparison?.files_changed || []).reduce((sum, f) => sum + f.deletions, 0)}
                      </span>
                    </div>
                    <a
                      href={displayedComparison.compare_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="ml-auto text-[10px] text-blue-600 hover:underline flex items-center gap-0.5 flex-shrink-0"
                    >
                      <GitCompare className="h-3 w-3" />
                      Ver no GitHub
                    </a>
                  </div>

                  {/* Painel de diff visual */}
                  <ScrollArea className="flex-1">
                    <div className="p-4 space-y-4">
                      {/* Seção de commits (colapsável) */}
                      {displayedComparison?.commits && displayedComparison.commits.length > 0 && (
                        <Card>
                          <CardHeader className="cursor-pointer hover:bg-muted/50" onClick={() => {
                            const content = document.getElementById('commits-content');
                            if (content) {
                              content.style.display = content.style.display === 'none' ? 'block' : 'none';
                            }
                          }}>
                            <CardTitle className="text-sm flex items-center justify-between">
                              <span>Commits ({displayedComparison.commits.length})</span>
                              <Clock className="h-4 w-4 text-muted-foreground" />
                            </CardTitle>
                          </CardHeader>
                          <div id="commits-content" style={{ display: 'none' }}>
                            <CardContent className="space-y-2 pt-0">
                              {displayedComparison.commits.map((commit) => (
                                <div key={commit.sha} className="border-l-2 border-blue-500 pl-3 py-2 hover:bg-muted/50 rounded-r">
                                  <p className="text-sm font-medium">{commit.message.split('\n')[0]}</p>
                                  <p className="text-xs text-muted-foreground mt-1">
                                    {commit.author} • {new Date(commit.date).toLocaleString('pt-BR')}
                                    <code className="ml-2 bg-muted px-1.5 py-0.5 rounded text-[10px]">
                                      {commit.sha.substring(0, 7)}
                                    </code>
                                  </p>
                                </div>
                              ))}
                            </CardContent>
                          </div>
                        </Card>
                      )}

                      {/* Seção de arquivos alterados com diff visual */}
                      {filteredFiles.length > 0 ? (
                        filteredFiles.map((file, index) => (
                          <Card key={index} className="overflow-hidden">
                            <CardHeader className="pb-3 bg-muted/30">
                              <div className="flex items-center justify-between">
                                <div className="flex-1">
                                  <p className="text-sm font-mono font-medium">{file.filename}</p>
                                  <div className="flex items-center gap-3 mt-1">
                                    <Badge
                                      variant={
                                        file.status === 'added' ? 'default' :
                                        file.status === 'removed' ? 'destructive' :
                                        'secondary'
                                      }
                                      className="text-xs"
                                    >
                                      {file.status}
                                    </Badge>
                                    <span className="text-xs text-green-600 font-medium">+{file.additions}</span>
                                    <span className="text-xs text-red-600 font-medium">-{file.deletions}</span>
                                  </div>
                                </div>
                                <FileText className="h-4 w-4 text-muted-foreground" />
                              </div>
                            </CardHeader>
                            {file.patch && (
                              <CardContent className="p-0">
                                <div className="bg-black/5 dark:bg-white/5 font-mono text-xs">
                                  {file.patch.split('\n').map((line, lineIndex) => {
                                    // Determinar estilo da linha baseado no formato unified diff
                                    let bgClass = '';
                                    let textClass = 'text-foreground';
                                    let linePrefix = '';

                                    if (line.startsWith('@@')) {
                                      // Header de contexto (azul)
                                      bgClass = 'bg-blue-500/10';
                                      textClass = 'text-blue-600 dark:text-blue-400';
                                      linePrefix = '';
                                    } else if (line.startsWith('+')) {
                                      // Adição (verde)
                                      bgClass = 'bg-green-500/10';
                                      textClass = 'text-green-700 dark:text-green-400';
                                      linePrefix = '+';
                                    } else if (line.startsWith('-')) {
                                      // Remoção (vermelho)
                                      bgClass = 'bg-red-500/10';
                                      textClass = 'text-red-700 dark:text-red-400';
                                      linePrefix = '-';
                                    } else {
                                      // Contexto (sem mudança)
                                      bgClass = '';
                                      textClass = 'text-muted-foreground';
                                      linePrefix = ' ';
                                    }

                                    return (
                                      <div
                                        key={lineIndex}
                                        className={`px-4 py-0.5 ${bgClass} ${textClass} hover:bg-muted/50`}
                                      >
                                        <span className="select-none mr-4 inline-block w-4 text-right">
                                          {linePrefix}
                                        </span>
                                        <span className="whitespace-pre-wrap break-all">
                                          {line.substring(1) || ' '}
                                        </span>
                                      </div>
                                    );
                                  })}
                                </div>
                              </CardContent>
                            )}
                            {!file.patch && (
                              <CardContent className="py-6">
                                <p className="text-xs text-muted-foreground text-center">
                                  Diff não disponível para este arquivo
                                </p>
                              </CardContent>
                            )}
                          </Card>
                        ))
                      ) : (
                        <>
                          {(displayedComparison?.files_changed?.length || 0) > 0 ? (
                            <Alert className="border-blue-500/50 bg-blue-50 dark:bg-blue-950/40">
                              <Info className="h-4 w-4 text-blue-600" />
                              <AlertTitle className="text-blue-700 dark:text-blue-400">
                                Arquivos ocultos pelo filtro
                              </AlertTitle>
                              <AlertDescription className="text-xs space-y-2">
                                <p className="text-foreground">
                                  Foram encontrados <strong>{displayedComparison?.files_changed?.length || 0} arquivos</strong> alterados,
                                  mas nenhum corresponde aos filtros atuais (apenas .yml, .yaml, .md, Dockerfile).
                                </p>
                                <p className="text-blue-600 dark:text-blue-400 font-semibold flex items-center gap-2 mt-2">
                                  <span>💡</span>
                                  <span>
                                    Ative o switch "Mostrar todos" no topo da página para visualizar todos os arquivos.
                                  </span>
                                </p>
                              </AlertDescription>
                            </Alert>
                          ) : (
                            <Alert>
                              <Info className="h-4 w-4" />
                              <AlertTitle>Nenhuma alteração</AlertTitle>
                              <AlertDescription className="text-xs">
                                Nenhum arquivo foi alterado nesta comparação entre as duas releases.
                              </AlertDescription>
                            </Alert>
                          )}
                        </>
                      )}
                    </div>
                  </ScrollArea>
                </div>
              )}
            </div>
          ),
        }}
      />

      {/* Dialog de redirecionamento para configuração de token */}
      <CredentialRedirectDialog
        open={showTokenModal}
        onOpenChange={setShowTokenModal}
        credentialName="GitHub Token"
      />

      {/* Modal de scan de deployments */}
      <DeploymentScanModal open={showScanModal} onOpenChange={setShowScanModal} />

      {/* Modal de importação do ServiceNow */}
      <ServiceNowImportModal
        open={showServiceNowModal}
        onClose={() => setShowServiceNowModal(false)}
        onImportSuccess={handleServiceNowImport}
      />
    </>
  );
};
