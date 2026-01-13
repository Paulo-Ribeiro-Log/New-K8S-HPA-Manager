import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';
import { RadioGroup, RadioGroupItem } from './ui/radio-group';
import { Download, Loader2, FolderOpen, FolderTree, Edit3 } from 'lucide-react';
import { toast } from 'sonner';
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs';
import { PodFileBrowser } from './PodFileBrowser';

interface PodFileTransferModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
  podName: string;
  containers: string[];
}

export const PodFileTransferModal = ({
  open,
  onOpenChange,
  cluster,
  namespace,
  podName,
  containers,
}: PodFileTransferModalProps) => {
  const [selectedContainer, setSelectedContainer] = useState<string>(containers[0] || '');
  const [remotePath, setRemotePath] = useState<string>('');
  const [transferType, setTransferType] = useState<'file' | 'directory'>('file');
  const [isDownloading, setIsDownloading] = useState(false);

  // CRÍTICO: Resetar container quando pod/containers mudam
  // Isso evita usar container de pod anterior que não existe no pod atual
  useEffect(() => {
    if (containers.length > 0) {
      setSelectedContainer(containers[0]);
    }
  }, [podName, containers]);

  const handleDownload = async () => {
    if (!remotePath.trim()) {
      toast.error('Caminho inválido', {
        description: 'Por favor, informe o caminho do arquivo ou diretório',
      });
      return;
    }

    setIsDownloading(true);

    try {
      // Construir URL com query parameters
      const params = new URLSearchParams({
        path: remotePath,
        type: transferType,
      });

      if (selectedContainer) {
        params.append('container', selectedContainer);
      }

      const url = `/api/v1/pods/${cluster}/${namespace}/${podName}/download?${params.toString()}`;

      // Fazer download via fetch com autenticação
      const token = localStorage.getItem('auth_token') || 'poc-token-123';
      const response = await fetch(url, {
        headers: {
          'Authorization': `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        let errorMsg = 'Erro ao fazer download';
        try {
          const error = await response.json();
          errorMsg = error.error || errorMsg;
        } catch {
          errorMsg = `HTTP ${response.status}: ${response.statusText}`;
        }
        throw new Error(errorMsg);
      }

      // Obter nome do arquivo do header Content-Disposition
      const contentDisposition = response.headers.get('Content-Disposition');
      let filename = 'download';
      if (contentDisposition) {
        const match = contentDisposition.match(/filename=(.+)/);
        if (match) {
          filename = match[1];
        }
      }

      // Criar blob e download
      const blob = await response.blob();
      const downloadUrl = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = downloadUrl;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(downloadUrl);
      document.body.removeChild(a);

      toast.success('Download concluído', {
        description: `Arquivo ${filename} baixado com sucesso`,
      });

      onOpenChange(false);
      setRemotePath('');
    } catch (error) {
      console.error('Download error:', error);
      toast.error('Erro ao fazer download', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    } finally {
      setIsDownloading(false);
    }
  };

  // Handler para download do file browser
  const handleBrowserDownload = async (path: string, isDirectory: boolean) => {
    setIsDownloading(true);
    setRemotePath(path);
    setTransferType(isDirectory ? 'directory' : 'file');

    try {
      const params = new URLSearchParams({
        path: path,
        type: isDirectory ? 'directory' : 'file',
      });

      if (selectedContainer) {
        params.append('container', selectedContainer);
      }

      const url = `/api/v1/pods/${cluster}/${namespace}/${podName}/download?${params.toString()}`;

      const token = localStorage.getItem('auth_token') || 'poc-token-123';
      const response = await fetch(url, {
        headers: {
          'Authorization': `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        let errorMsg = 'Erro ao fazer download';
        try {
          const error = await response.json();
          errorMsg = error.error || errorMsg;
        } catch {
          errorMsg = `HTTP ${response.status}: ${response.statusText}`;
        }
        throw new Error(errorMsg);
      }

      const contentDisposition = response.headers.get('Content-Disposition');
      let filename = 'download';
      if (contentDisposition) {
        const match = contentDisposition.match(/filename=(.+)/);
        if (match) {
          filename = match[1];
        }
      }

      const blob = await response.blob();
      const downloadUrl = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = downloadUrl;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(downloadUrl);
      document.body.removeChild(a);

      toast.success('Download concluído', {
        description: `Arquivo ${filename} baixado com sucesso`,
      });
    } catch (error) {
      console.error('Download error:', error);
      toast.error('Erro ao fazer download', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    } finally {
      setIsDownloading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[800px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Download className="h-5 w-5 text-primary" />
            Transferir Arquivos do PVC
          </DialogTitle>
          <DialogDescription>
            Faça download de arquivos ou diretórios do pod para sua máquina local
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="browser" className="w-full">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="browser">
              <FolderTree className="mr-2 h-4 w-4" />
              Navegar Arquivos
            </TabsTrigger>
            <TabsTrigger value="manual">
              <Edit3 className="mr-2 h-4 w-4" />
              Caminho Manual
            </TabsTrigger>
          </TabsList>

          <TabsContent value="browser" className="mt-4">
            {/* Container Selection para Browser */}
            {containers.length > 1 && (
              <div className="space-y-2 mb-4">
                <Label htmlFor="container-browser">Container</Label>
                <Select value={selectedContainer} onValueChange={setSelectedContainer}>
                  <SelectTrigger id="container-browser">
                    <SelectValue placeholder="Selecione o container" />
                  </SelectTrigger>
                  <SelectContent>
                    {containers.map((container) => (
                      <SelectItem key={container} value={container}>
                        {container}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            <PodFileBrowser
              cluster={cluster}
              namespace={namespace}
              podName={podName}
              container={selectedContainer}
              onDownload={handleBrowserDownload}
              onClose={() => onOpenChange(false)}
            />
          </TabsContent>

          <TabsContent value="manual" className="mt-4">
            <div className="space-y-4 py-4">
          {/* Pod Info */}
          <div className="space-y-2">
            <div className="text-sm">
              <span className="font-medium">Pod:</span> {podName}
            </div>
            <div className="text-sm text-muted-foreground">
              <span className="font-medium">Namespace:</span> {namespace}
            </div>
          </div>

          {/* Container Selection */}
          {containers.length > 1 && (
            <div className="space-y-2">
              <Label htmlFor="container">Container</Label>
              <Select value={selectedContainer} onValueChange={setSelectedContainer}>
                <SelectTrigger id="container">
                  <SelectValue placeholder="Selecione o container" />
                </SelectTrigger>
                <SelectContent>
                  {containers.map((container) => (
                    <SelectItem key={container} value={container}>
                      {container}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Remote Path Input */}
          <div className="space-y-2">
            <Label htmlFor="path">Caminho Absoluto no Pod</Label>
            <div className="flex gap-2">
              <Input
                id="path"
                placeholder="/mnt/storage/PRD/2024-10-31/[43166337503]/arquivo.jpg"
                value={remotePath}
                onChange={(e) => setRemotePath(e.target.value)}
                className="flex-1"
              />
              <Button
                variant="outline"
                size="icon"
                onClick={() => setRemotePath('/mnt/storage/')}
                title="Atalho para /mnt/storage/"
              >
                <FolderOpen className="h-4 w-4" />
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              IMPORTANTE: Use caminho ABSOLUTO iniciando com /. Exemplo: /mnt/storage/pasta/arquivo.jpg
            </p>
          </div>

          {/* Transfer Type */}
          <div className="space-y-2">
            <Label>Tipo de Transferência</Label>
            <RadioGroup
              value={transferType}
              onValueChange={(value) => setTransferType(value as 'file' | 'directory')}
              className="flex gap-4"
            >
              <div className="flex items-center space-x-2">
                <RadioGroupItem value="file" id="file" />
                <Label htmlFor="file" className="font-normal cursor-pointer">
                  Arquivo
                </Label>
              </div>
              <div className="flex items-center space-x-2">
                <RadioGroupItem value="directory" id="directory" />
                <Label htmlFor="directory" className="font-normal cursor-pointer">
                  Diretório (tar.gz)
                </Label>
              </div>
            </RadioGroup>
            {transferType === 'directory' && (
              <p className="text-xs text-muted-foreground">
                O diretório será compactado em tar.gz antes do download
              </p>
            )}
          </div>
        </div>

        <div className="flex justify-end gap-2 mt-4">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isDownloading}>
            Cancelar
          </Button>
          <Button onClick={handleDownload} disabled={isDownloading || !remotePath.trim()}>
            {isDownloading ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Baixando...
              </>
            ) : (
              <>
                <Download className="mr-2 h-4 w-4" />
                Download
              </>
            )}
          </Button>
        </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
};
