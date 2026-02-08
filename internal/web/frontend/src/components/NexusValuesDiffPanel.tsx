import { useEffect, useState, useCallback, useRef, useMemo, type ChangeEvent } from 'react';
import { useNexusConfig, useNexusValues } from '../hooks/useNexus';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Loader2, AlertCircle, GitCompare, Download, Settings as SettingsIcon, X, Check } from 'lucide-react';
import { Alert, AlertDescription } from './ui/alert';
import { MonacoYamlEditor } from './MonacoYamlEditor';
import { CredentialRedirectDialog } from './profile/CredentialRedirectDialog';
import { ValuesFileRequest, BrowseItem } from '../types/nexus';
import { NexusProvider, useNexusStore } from '../store/nexusStore';
import { cn } from '../lib/utils';

// Componente Input com busca debounced e sugestões dropdown
interface NexusSearchInputProps {
  value: string;
  onChange: (value: string) => void;
  onSelect?: (value: string) => void;
  suggestions: BrowseItem[];
  loading: boolean;
  onSearch: (query: string) => void;
  placeholder: string;
  label: string;
  id: string;
  disabled?: boolean;
  helpText?: string;
}

const NexusSearchInput = ({ value, onChange, onSelect, suggestions, loading, onSearch, placeholder, label, id, disabled, helpText }: NexusSearchInputProps) => {
  const [showSuggestions, setShowSuggestions] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const handleInputChange = (e: ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    onChange(val);

    // Debounce: buscar após 500ms de pausa
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (val.length >= 2) {
      debounceRef.current = setTimeout(() => {
        onSearch(val);
        setShowSuggestions(true);
      }, 500);
    } else {
      setShowSuggestions(false);
    }
  };

  const handleSelect = (name: string) => {
    if (onSelect) {
      onSelect(name);
    } else {
      onChange(name);
    }
    setShowSuggestions(false);
  };

  // Fechar sugestões ao clicar fora
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setShowSuggestions(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const isInList = suggestions.some(item => item.name === value);

  return (
    <div className="space-y-2" ref={containerRef}>
      <Label htmlFor={id}>{label}</Label>
      <div className="relative">
        <Input
          id={id}
          value={value}
          onChange={handleInputChange}
          onFocus={() => { if (suggestions.length > 0 && value.length >= 2) setShowSuggestions(true); }}
          placeholder={placeholder}
          disabled={disabled}
          className={cn(isInList && "border-green-500/50")}
        />
        <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
          {loading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          {value && isInList && <Check className="h-3 w-3 text-green-500" />}
        </div>

        {/* Dropdown de sugestões */}
        {showSuggestions && (
          <div className="absolute z-50 w-full mt-1 bg-popover border rounded-md shadow-lg max-h-48 overflow-y-auto">
            {loading ? (
              <div className="flex items-center justify-center py-4">
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
                <span className="text-sm text-muted-foreground">Buscando no Nexus...</span>
              </div>
            ) : suggestions.length === 0 ? (
              <div className="py-3 px-4 text-sm text-muted-foreground">
                Nenhum resultado para "{value}"
              </div>
            ) : (
              suggestions.map((item) => (
                <button
                  key={item.name}
                  type="button"
                  className={cn(
                    "w-full text-left px-4 py-2 text-sm hover:bg-accent hover:text-accent-foreground cursor-pointer",
                    value === item.name && "bg-accent"
                  )}
                  onClick={() => handleSelect(item.name)}
                >
                  {item.name}
                </button>
              ))
            )}
          </div>
        )}
      </div>
      {helpText && <p className="text-xs text-muted-foreground">{helpText}</p>}
    </div>
  );
};

// Componente interno que usa a store
const NexusValuesDiffPanelContent = () => {
  const { checkStatus, browseRepository } = useNexusConfig();
  const { compareValues, loading, error } = useNexusValues();

  // Sugestões de browse
  const [releaseSuggestions, setReleaseSuggestions] = useState<BrowseItem[]>([]);
  const [browsing, setBrowsing] = useState({ releases: false });

  // Usar store ao invés de useState local
  const {
    status,
    statusLoading,
    showConfigPanel,
    comparing,
    file1,
    file2,
    file1Content,
    file2Content,
    compareError,
    file1Url,
    file2Url,
    showDiffModal,
    setStatus,
    setStatusLoading,
    setShowConfigPanel,
    setComparing,
    setFile1,
    setFile2,
    setFile1Content,
    setFile2Content,
    setCompareError,
    setFile1Url,
    setFile2Url,
    setShowDiffModal,
    clearCompareResults,
  } = useNexusStore();

  // Check Nexus status on mount
  useEffect(() => {
    loadStatus();
  }, []);

  const loadStatus = async () => {
    setStatusLoading(true);
    try {
      const nexusStatus = await checkStatus();
      console.log('[NexusValuesDiffPanel] Status loaded:', nexusStatus);
      setStatus(nexusStatus);
    } catch (err) {
      console.error('[NexusValuesDiffPanel] Failed to check Nexus status:', err);
      setStatus({ configured: false });
    } finally {
      setStatusLoading(false);
    }
  };

  const searchReleases = useCallback(async (query: string) => {
    if (query.length < 2) return;
    console.log('[NexusValuesDiffPanel] Searching releases:', query);
    setBrowsing(prev => ({ ...prev, releases: true }));
    try {
      const items = await browseRepository('', query);
      console.log('[NexusValuesDiffPanel] Found releases:', items.length, items.map(i => i.name));
      setReleaseSuggestions(items);
    } catch (err) {
      console.error('[NexusValuesDiffPanel] Failed to search releases:', err);
    } finally {
      setBrowsing(prev => ({ ...prev, releases: false }));
    }
  }, [browseRepository]);

  // Extrai versões e arquivos do BrowseItem (já vêm da busca de releases, sem chamada extra)
  const getReleaseData = useCallback((releaseName: string) => {
    return releaseSuggestions.find(item => item.name === releaseName);
  }, [releaseSuggestions]);

  const getVersionsForRelease = useCallback((releaseName: string): string[] => {
    return getReleaseData(releaseName)?.versions || [];
  }, [getReleaseData]);

  const getFilesForVersion = useCallback((releaseName: string, version: string): string[] => {
    return getReleaseData(releaseName)?.files?.[version] || [];
  }, [getReleaseData]);

  const handleCompare = async () => {
    setComparing(true);
    setCompareError(null);
    setFile1Content('');
    setFile2Content('');
    setFile1Url('');
    setFile2Url('');

    try {
      const result = await compareValues({ file1, file2 });

      if (result?.file1?.fullUrl) setFile1Url(result.file1.fullUrl);
      if (result?.file2?.fullUrl) setFile2Url(result.file2.fullUrl);

      if (result.error) {
        let errorDetails = result.error;
        if (result.file1?.error) errorDetails += `\nArquivo 1: ${result.file1.error}`;
        if (result.file2?.error) errorDetails += `\nArquivo 2: ${result.file2.error}`;
        setCompareError(errorDetails);
        return;
      }

      if (result.file1?.error) {
        const urlInfo = result.file1.fullUrl ? `\nURL: ${result.file1.fullUrl}` : '';
        setCompareError(`Arquivo 1: ${result.file1.error}${urlInfo}`);
        return;
      }

      if (result.file2?.error) {
        const urlInfo = result.file2.fullUrl ? `\nURL: ${result.file2.fullUrl}` : '';
        setCompareError(`Arquivo 2: ${result.file2.error}${urlInfo}`);
        return;
      }

      const content1 = result.file1?.content || '';
      const content2 = result.file2?.content || '';

      if (!content1 && !content2) {
        setCompareError('Ambos os arquivos estao vazios ou nao foram encontrados');
        return;
      }

      setFile1Content(content1);
      setFile2Content(content2);
      setShowDiffModal(true);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Erro ao comparar arquivos';
      setCompareError(errorMessage);
    } finally {
      setComparing(false);
    }
  };

  const handleExport = () => {
    const diffText = `# Comparação de Values\n\n` +
      `## Arquivo 1\n` +
      `Release: ${file1.release}\n` +
      `Version: ${file1.version}\n` +
      `Environment: ${file1.environment}\n` +
      `Type: ${file1.type}\n\n` +
      `${file1Content}\n\n` +
      `---\n\n` +
      `## Arquivo 2\n` +
      `Release: ${file2.release}\n` +
      `Version: ${file2.version}\n` +
      `Environment: ${file2.environment}\n` +
      `Type: ${file2.type}\n\n` +
      `${file2Content}`;

    const blob = new Blob([diffText], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `nexus-diff-${file1.release}-${Date.now()}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const isFormValid = () => {
    return (
      file1.release && file1.version && file1.filePath &&
      file2.release && file2.version && file2.filePath
    );
  };

  const buildPreviewUrl = (fileRequest: ValuesFileRequest): string => {
    if (!status?.baseUrl || !fileRequest.filePath) return '';
    const baseUrl = status.baseUrl.replace(/\/$/, '');
    const repo = fileRequest.repository || status.repository || '';
    return `${baseUrl}/repository/${repo}/${fileRequest.filePath}`;
  };

  const file1PreviewUrl = buildPreviewUrl(file1);
  const file2PreviewUrl = buildPreviewUrl(file2);

  // Renderizar seletor de arquivo: Release → Versão → Arquivo
  const renderFileSelector = (
    fileKey: 'file1' | 'file2',
    file: ValuesFileRequest,
    setFile: (f: ValuesFileRequest) => void,
    title: string,
    description: string,
  ) => {
    const versions = getVersionsForRelease(file.release);
    const files = getFilesForVersion(file.release, file.version);

    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Release - busca com autocomplete */}
          <NexusSearchInput
            id={`${fileKey}-release`}
            label="Release"
            value={file.release}
            onChange={(val) => {
              setFile({ ...file, release: val, version: '', filePath: '' });
            }}
            onSelect={(val) => {
              const releaseItem = releaseSuggestions.find(item => item.name === val);
              setFile({ ...file, release: val, version: '', filePath: '', repository: releaseItem?.repository || '' });
            }}
            suggestions={releaseSuggestions}
            loading={browsing.releases}
            onSearch={searchReleases}
            placeholder="Digite 2+ caracteres para buscar..."
            helpText="Ex: comercial, sortimento, faturamento"
          />

          {/* Versão - select populado da busca */}
          <div className="space-y-2">
            <Label htmlFor={`${fileKey}-version`}>Versão</Label>
            {versions.length > 0 ? (
              <Select
                value={file.version}
                onValueChange={(val: string) => setFile({ ...file, version: val, filePath: '' })}
              >
                <SelectTrigger id={`${fileKey}-version`}>
                  <SelectValue placeholder="Selecione a versão" />
                </SelectTrigger>
                <SelectContent className="max-h-60">
                  {versions.map((v) => (
                    <SelectItem key={v} value={v}>{v}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <div className="flex items-center h-10 px-3 border rounded-md bg-muted/30">
                <span className="text-sm text-muted-foreground">
                  {file.release ? 'Nenhuma versão encontrada' : 'Selecione uma release primeiro'}
                </span>
              </div>
            )}
          </div>

          {/* Arquivo - select populado da busca */}
          <div className="space-y-2">
            <Label htmlFor={`${fileKey}-file`}>Arquivo</Label>
            {files.length > 0 ? (
              <Select
                value={file.filePath ? file.filePath.replace(`${file.release}/${file.version}/`, '') : ''}
                onValueChange={(val: string) => {
                  setFile({ ...file, filePath: `${file.release}/${file.version}/${val}` });
                }}
              >
                <SelectTrigger id={`${fileKey}-file`}>
                  <SelectValue placeholder="Selecione o arquivo" />
                </SelectTrigger>
                <SelectContent className="max-h-60">
                  {files.map((f) => (
                    <SelectItem key={f} value={f}>{f}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <div className="flex items-center h-10 px-3 border rounded-md bg-muted/30">
                <span className="text-sm text-muted-foreground">
                  {file.version ? 'Nenhum arquivo encontrado' : 'Selecione uma versão primeiro'}
                </span>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    );
  };

  // Mostrar loading enquanto verifica status
  if (statusLoading) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-4 p-8">
        <Loader2 className="h-12 w-12 animate-spin text-muted-foreground" />
        <h3 className="text-lg font-medium">Verificando conexao com Nexus...</h3>
      </div>
    );
  }

  // Mostrar configuração se não estiver configurado
  if (!status?.configured) {
    return (
      <>
        <div className="flex flex-col items-center justify-center h-full gap-4 p-8">
          <AlertCircle className="h-12 w-12 text-muted-foreground" />
          <h3 className="text-lg font-medium">Nexus nao configurado</h3>
          <p className="text-sm text-muted-foreground text-center max-w-md">
            Configure a conexao com o Nexus Repository Manager para comecar a comparar arquivos values.yaml
          </p>
          <Button onClick={() => setShowConfigPanel(true)}>
            <SettingsIcon className="h-4 w-4 mr-2" />
            Configurar Nexus
          </Button>
        </div>

        <CredentialRedirectDialog
          open={showConfigPanel}
          onOpenChange={setShowConfigPanel}
          credentialName="Nexus Repository"
        />
      </>
    );
  }

  return (
    <div className="flex flex-col h-full p-4 gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">Comparar Values do Nexus</h2>
          <p className="text-sm text-muted-foreground">
            Compare arquivos values.yaml de diferentes ambientes ou versões
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setShowConfigPanel(true)}>
          <SettingsIcon className="h-4 w-4 mr-2" />
          Configurar
        </Button>
      </div>

      {/* Status */}
      {status && (
        <Alert>
          <AlertDescription>
            Conectado ao Nexus: <strong>{status.baseUrl}</strong> • Repository: <strong>{status.repository}</strong>
          </AlertDescription>
        </Alert>
      )}

      {/* File Selectors */}
      <div className="grid grid-cols-2 gap-4">
        {renderFileSelector('file1', file1, setFile1, 'Arquivo 1', 'Selecione o primeiro arquivo para comparação')}
        {renderFileSelector('file2', file2, setFile2, 'Arquivo 2', 'Selecione o segundo arquivo para comparação')}
      </div>

      {/* URL Preview */}
      {(file1PreviewUrl || file2PreviewUrl) && (
        <Card className="bg-muted/50">
          <CardHeader className="py-3">
            <CardTitle className="text-sm">Preview das URLs</CardTitle>
            <CardDescription className="text-xs">
              Verifique se as URLs estão corretas antes de comparar. Se estiverem erradas, ajuste o "Padrão de URL" nas configurações.
            </CardDescription>
          </CardHeader>
          <CardContent className="py-2 space-y-2">
            {file1PreviewUrl && (
              <div className="text-xs">
                <span className="font-semibold">Arquivo 1:</span>
                <code className="block bg-background p-2 rounded text-xs break-all mt-1">{file1PreviewUrl}</code>
              </div>
            )}
            {file2PreviewUrl && (
              <div className="text-xs">
                <span className="font-semibold">Arquivo 2:</span>
                <code className="block bg-background p-2 rounded text-xs break-all mt-1">{file2PreviewUrl}</code>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Action Buttons */}
      <div className="flex items-center justify-center gap-4">
        <Button
          size="lg"
          onClick={handleCompare}
          disabled={!isFormValid() || comparing || loading}
        >
          {comparing || loading ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin mr-2" />
              Comparando...
            </>
          ) : (
            <>
              <GitCompare className="h-4 w-4 mr-2" />
              Comparar
            </>
          )}
        </Button>

        {(file1Content || file2Content) && (
          <Button variant="outline" onClick={handleExport}>
            <Download className="h-4 w-4 mr-2" />
            Exportar
          </Button>
        )}
      </div>

      {/* Errors */}
      {(error || compareError) && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription className="whitespace-pre-line">{error || compareError}</AlertDescription>
        </Alert>
      )}

      {/* Botao para reabrir o diff se ja foi carregado */}
      {(file1Content || file2Content) && !showDiffModal && (
        <Button variant="outline" onClick={() => setShowDiffModal(true)}>
          <GitCompare className="h-4 w-4 mr-2" />
          Ver Comparacao
        </Button>
      )}

      {/* Modal do Diff - Não fecha ao clicar fora */}
      <Dialog open={showDiffModal} onOpenChange={() => {}}>
        <DialogContent
          className="max-w-[95vw] w-[95vw] h-[90vh] flex flex-col"
          onPointerDownOutside={(e) => e.preventDefault()}
          onEscapeKeyDown={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
        >
          {/* Header com botao X */}
          <DialogHeader className="flex-shrink-0">
            <div className="flex items-center justify-between">
              <div>
                <DialogTitle>Comparacao de Values</DialogTitle>
                <DialogDescription>
                  {file1.filePath || `${file1.release}/${file1.version}`} vs {file2.filePath || `${file2.release}/${file2.version}`}
                </DialogDescription>
              </div>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={handleExport}>
                  <Download className="h-4 w-4 mr-2" />
                  Exportar
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setShowDiffModal(false)}
                  className="h-8 w-8"
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </DialogHeader>

          {/* URLs dos arquivos */}
          {(file1Url || file2Url) && (
            <Alert className="flex-shrink-0">
              <AlertDescription className="text-xs font-mono">
                <div><strong>Arquivo 1:</strong> {file1Url || 'N/A'}</div>
                <div><strong>Arquivo 2:</strong> {file2Url || 'N/A'}</div>
              </AlertDescription>
            </Alert>
          )}

          {/* Aviso se algum arquivo estiver vazio */}
          {(!file1Content || !file2Content) && (
            <Alert variant="default" className="flex-shrink-0">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                {!file1Content && !file2Content
                  ? 'Ambos os arquivos estao vazios'
                  : !file1Content
                    ? 'Arquivo 1 esta vazio ou nao foi encontrado'
                    : 'Arquivo 2 esta vazio ou nao foi encontrado'
                }
              </AlertDescription>
            </Alert>
          )}

          {/* Monaco Diff Editor */}
          <div className="flex-1 border rounded-lg overflow-hidden min-h-0" style={{ minHeight: '400px' }}>
            <MonacoYamlEditor
              mode="diff"
              originalValue={file1Content || '# Arquivo vazio ou nao encontrado'}
              value={file2Content || '# Arquivo vazio ou nao encontrado'}
              readOnly={true}
              height="calc(90vh - 200px)"
            />
          </div>
        </DialogContent>
      </Dialog>

      {/* Dialog de redirecionamento para configuração */}
      <CredentialRedirectDialog
        open={showConfigPanel}
        onOpenChange={setShowConfigPanel}
        credentialName="Nexus Repository"
      />
    </div>
  );
};

// Componente exportado que envolve o Content com o Provider
export const NexusValuesDiffPanel = () => {
  return (
    <NexusProvider>
      <NexusValuesDiffPanelContent />
    </NexusProvider>
  );
};
