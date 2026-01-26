import { useEffect } from 'react';
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
import { Loader2, AlertCircle, GitCompare, Download, Settings as SettingsIcon, X } from 'lucide-react';
import { Alert, AlertDescription } from './ui/alert';
import { MonacoYamlEditor } from './MonacoYamlEditor';
import { CredentialRedirectDialog } from './profile/CredentialRedirectDialog';
import { VALID_ENVIRONMENTS, VALID_TYPES, DEFAULT_URL_PATTERN, ValuesFileRequest } from '../types/nexus';
import { NexusProvider, useNexusStore } from '../store/nexusStore';

// Componente interno que usa a store
const NexusValuesDiffPanelContent = () => {
  const { checkStatus } = useNexusConfig();
  const { compareValues, loading, error } = useNexusValues();

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
      // Não abre mais automaticamente - usuário clica no botão se quiser
    } catch (err) {
      console.error('[NexusValuesDiffPanel] Failed to check Nexus status:', err);
      // Se falhou ao verificar status, assumir que não está configurado
      setStatus({ configured: false });
    } finally {
      setStatusLoading(false);
    }
  };

  const handleCompare = async () => {
    setComparing(true);
    setCompareError(null);
    setFile1Content('');
    setFile2Content('');
    setFile1Url('');
    setFile2Url('');

    console.log('[NexusValuesDiffPanel] Starting comparison...');
    console.log('[NexusValuesDiffPanel] File1:', file1);
    console.log('[NexusValuesDiffPanel] File2:', file2);

    try {
      const result = await compareValues({ file1, file2 });
      console.log('[NexusValuesDiffPanel] Compare result:', result);
      console.log('[NexusValuesDiffPanel] File1 content length:', result?.file1?.content?.length || 0);
      console.log('[NexusValuesDiffPanel] File2 content length:', result?.file2?.content?.length || 0);

      // Salvar URLs para exibição de erros
      if (result?.file1?.fullUrl) setFile1Url(result.file1.fullUrl);
      if (result?.file2?.fullUrl) setFile2Url(result.file2.fullUrl);

      // Verificar erro geral
      if (result.error) {
        console.error('[NexusValuesDiffPanel] Result error:', result.error);
        let errorDetails = result.error;
        // Adicionar detalhes dos erros dos arquivos
        if (result.file1?.error) errorDetails += `\nArquivo 1: ${result.file1.error}`;
        if (result.file2?.error) errorDetails += `\nArquivo 2: ${result.file2.error}`;
        setCompareError(errorDetails);
        return;
      }

      // Verificar erro do arquivo 1
      if (result.file1?.error) {
        console.error('[NexusValuesDiffPanel] File1 error:', result.file1.error);
        const urlInfo = result.file1.fullUrl ? `\nURL: ${result.file1.fullUrl}` : '';
        setCompareError(`Arquivo 1: ${result.file1.error}${urlInfo}`);
        return;
      }

      // Verificar erro do arquivo 2
      if (result.file2?.error) {
        console.error('[NexusValuesDiffPanel] File2 error:', result.file2.error);
        const urlInfo = result.file2.fullUrl ? `\nURL: ${result.file2.fullUrl}` : '';
        setCompareError(`Arquivo 2: ${result.file2.error}${urlInfo}`);
        return;
      }

      // Verificar se os conteúdos existem
      const content1 = result.file1?.content || '';
      const content2 = result.file2?.content || '';

      if (!content1 && !content2) {
        console.warn('[NexusValuesDiffPanel] Both files are empty');
        setCompareError('Ambos os arquivos estao vazios ou nao foram encontrados');
        return;
      }

      console.log('[NexusValuesDiffPanel] Setting file contents...');
      console.log('[NexusValuesDiffPanel] Content1 preview:', content1.substring(0, 100));
      console.log('[NexusValuesDiffPanel] Content2 preview:', content2.substring(0, 100));

      setFile1Content(content1);
      setFile2Content(content2);
      setShowDiffModal(true); // Abre o modal automaticamente
      console.log('[NexusValuesDiffPanel] Comparison successful!');
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Erro ao comparar arquivos';
      console.error('[NexusValuesDiffPanel] Exception:', errorMessage, err);
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
      file1.release && file1.version && file1.environment && file1.type &&
      file2.release && file2.version && file2.environment && file2.type
    );
  };

  // Função para construir a URL de preview
  const buildPreviewUrl = (fileRequest: ValuesFileRequest): string => {
    if (!status?.baseUrl || !status?.repository) return '';
    if (!fileRequest.release || !fileRequest.version || !fileRequest.environment || !fileRequest.type) return '';

    const pattern = status.urlPattern || DEFAULT_URL_PATTERN;
    let url = pattern;
    url = url.replace('{baseUrl}', status.baseUrl.replace(/\/$/, ''));
    url = url.replace('{repository}', status.repository);
    url = url.replace('{release}', fileRequest.release);
    url = url.replace('{version}', fileRequest.version);
    url = url.replace('{environment}', fileRequest.environment);
    url = url.replace('{type}', fileRequest.type);
    return url;
  };

  const file1PreviewUrl = buildPreviewUrl(file1);
  const file2PreviewUrl = buildPreviewUrl(file2);

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
        {/* File 1 */}
        <Card>
          <CardHeader>
            <CardTitle>Arquivo 1</CardTitle>
            <CardDescription>Selecione o primeiro arquivo para comparação</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="file1-release">Release</Label>
              <Input
                id="file1-release"
                value={file1.release}
                onChange={(e) => setFile1({ ...file1, release: e.target.value })}
                placeholder="nome-do-release"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="file1-version">Versão</Label>
              <Input
                id="file1-version"
                value={file1.version}
                onChange={(e) => setFile1({ ...file1, version: e.target.value })}
                placeholder="v1.0.0"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="file1-environment">Ambiente</Label>
              <Select
                value={file1.environment}
                onValueChange={(value: any) => setFile1({ ...file1, environment: value })}
              >
                <SelectTrigger id="file1-environment">
                  <SelectValue placeholder="Selecione o ambiente" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">DEFAULT</SelectItem>
                  {VALID_ENVIRONMENTS.map((env) => (
                    <SelectItem key={env} value={env}>
                      {env.toUpperCase()}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="file1-type">Tipo</Label>
              <Select
                value={file1.type}
                onValueChange={(value: any) => setFile1({ ...file1, type: value })}
              >
                <SelectTrigger id="file1-type">
                  <SelectValue placeholder="Selecione o tipo" />
                </SelectTrigger>
                <SelectContent>
                  {VALID_TYPES.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </CardContent>
        </Card>

        {/* File 2 */}
        <Card>
          <CardHeader>
            <CardTitle>Arquivo 2</CardTitle>
            <CardDescription>Selecione o segundo arquivo para comparação</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="file2-release">Release</Label>
              <Input
                id="file2-release"
                value={file2.release}
                onChange={(e) => setFile2({ ...file2, release: e.target.value })}
                placeholder="nome-do-release"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="file2-version">Versão</Label>
              <Input
                id="file2-version"
                value={file2.version}
                onChange={(e) => setFile2({ ...file2, version: e.target.value })}
                placeholder="v1.0.0"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="file2-environment">Ambiente</Label>
              <Select
                value={file2.environment}
                onValueChange={(value: any) => setFile2({ ...file2, environment: value })}
              >
                <SelectTrigger id="file2-environment">
                  <SelectValue placeholder="Selecione o ambiente" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">DEFAULT</SelectItem>
                  {VALID_ENVIRONMENTS.map((env) => (
                    <SelectItem key={env} value={env}>
                      {env.toUpperCase()}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="file2-type">Tipo</Label>
              <Select
                value={file2.type}
                onValueChange={(value: any) => setFile2({ ...file2, type: value })}
              >
                <SelectTrigger id="file2-type">
                  <SelectValue placeholder="Selecione o tipo" />
                </SelectTrigger>
                <SelectContent>
                  {VALID_TYPES.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </CardContent>
        </Card>
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
                  {file1.release}/{file1.version}/{file1.environment}/{file1.type} vs {file2.release}/{file2.version}/{file2.environment}/{file2.type}
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
