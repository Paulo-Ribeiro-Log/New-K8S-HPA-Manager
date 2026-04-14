import { useState, useEffect } from 'react';
import { Button } from './ui/button';
import { Checkbox } from './ui/checkbox';
import { ScrollArea } from './ui/scroll-area';
import { Input } from './ui/input';
import { Label } from './ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table';
import {
  Folder,
  File,
  Download,
  ChevronRight,
  Home,
  Loader2,
  FolderOpen,
  FolderTree,
} from 'lucide-react';
import { toast } from 'sonner';
import { Badge } from './ui/badge';

interface FileInfo {
  name: string;
  path: string;
  size: number;
  isDirectory: boolean;
  permissions: string;
  modTime: string;
}

interface PodFileBrowserProps {
  cluster: string;
  namespace: string;
  podName: string;
  container: string;
  onDownload: (path: string, isDirectory: boolean) => void;
  onClose?: () => void;
}

export const PodFileBrowser = ({
  cluster,
  namespace,
  podName,
  container,
  onDownload,
  onClose,
}: PodFileBrowserProps) => {
  const [currentPath, setCurrentPath] = useState<string>('');
  const [initialPath, setInitialPath] = useState<string>('/mnt/storage');
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const [isLoading, setIsLoading] = useState(false);
  const [hasStarted, setHasStarted] = useState(false);

  // Carregar arquivos do diretório atual
  const loadDirectory = async (path: string) => {
    setIsLoading(true);
    try {
      const params = new URLSearchParams({ path });
      if (container) {
        params.append('container', container);
      }

      const url = `/api/v1/pods/${cluster}/${namespace}/${podName}/browse?${params.toString()}`;
      const token = localStorage.getItem('auth_token');

      const response = await fetch(url, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        let errorMsg = 'Erro ao carregar diretório';
        try {
          const error = await response.json();
          errorMsg = error.error || errorMsg;
        } catch {
          errorMsg = `HTTP ${response.status}: ${response.statusText}`;
        }
        throw new Error(errorMsg);
      }

      const result = await response.json();
      if (result.success && result.data.files) {
        setFiles(result.data.files);
        setCurrentPath(result.data.path);
        setSelectedFiles(new Set());
      }
    } catch (error) {
      console.error('Error loading directory:', error);
      toast.error('Erro ao carregar diretório', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    } finally {
      setIsLoading(false);
    }
  };

  // Iniciar navegação
  const startBrowsing = () => {
    if (!initialPath.trim()) {
      toast.error('Informe um diretório');
      return;
    }
    setHasStarted(true);
    loadDirectory(initialPath);
  };

  // Voltar para tela de seleção de diretório
  const resetBrowser = () => {
    setHasStarted(false);
    setCurrentPath('');
    setFiles([]);
    setSelectedFiles(new Set());
  };

  // Navegar para diretório
  const navigateToDirectory = (path: string) => {
    loadDirectory(path);
  };

  // Navegar para parent
  const navigateToParent = () => {
    if (currentPath === '/') return;
    const parentPath = currentPath.split('/').slice(0, -1).join('/') || '/';
    navigateToDirectory(parentPath);
  };

  // Toggle seleção de arquivo
  const toggleFileSelection = (filePath: string) => {
    const newSelected = new Set(selectedFiles);
    if (newSelected.has(filePath)) {
      newSelected.delete(filePath);
    } else {
      newSelected.add(filePath);
    }
    setSelectedFiles(newSelected);
  };

  // Selecionar todos
  const toggleSelectAll = () => {
    if (selectedFiles.size === files.length) {
      setSelectedFiles(new Set());
    } else {
      setSelectedFiles(new Set(files.map((f) => f.path)));
    }
  };

  // Download de arquivos selecionados
  const handleDownloadSelected = async () => {
    if (selectedFiles.size === 0) {
      toast.error('Nenhum arquivo selecionado');
      return;
    }

    if (selectedFiles.size === 1) {
      // Download simples
      const filePath = Array.from(selectedFiles)[0];
      const file = files.find((f) => f.path === filePath);
      if (file) {
        onDownload(file.path, file.isDirectory);
      }
    } else {
      // Download em batch (múltiplos arquivos)
      await handleBatchDownload(Array.from(selectedFiles));
    }
  };

  // Download em batch - múltiplos arquivos em tar.gz
  const handleBatchDownload = async (paths: string[]) => {
    try {
      const url = `/api/v1/pods/${cluster}/${namespace}/${podName}/download/batch?container=${container}`;
      const token = localStorage.getItem('auth_token');

      const response = await fetch(url, {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ paths }),
      });

      if (!response.ok) {
        let errorMsg = 'Erro ao fazer batch download';
        try {
          const error = await response.json();
          errorMsg = error.error || errorMsg;
        } catch {
          errorMsg = `HTTP ${response.status}: ${response.statusText}`;
        }
        throw new Error(errorMsg);
      }

      const contentDisposition = response.headers.get('Content-Disposition');
      let filename = 'batch-download.tar.gz';
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

      toast.success('Batch download concluído', {
        description: `${paths.length} arquivos empacotados em ${filename}`,
      });

      // Limpar seleção após download
      setSelectedFiles(new Set());
    } catch (error) {
      console.error('Batch download error:', error);
      toast.error('Erro ao fazer batch download', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    }
  };

  // Formatar tamanho de arquivo
  const formatSize = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  // Formatar data
  const formatDate = (dateStr: string): string => {
    try {
      const date = new Date(dateStr);
      return date.toLocaleString('pt-BR', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return dateStr;
    }
  };

  // Breadcrumb path parts
  const pathParts = currentPath.split('/').filter(Boolean);

  // Diretórios comuns para seleção rápida
  const commonDirectories = [
    { label: '/mnt/storage (PVC Storage)', value: '/mnt/storage' },
    { label: '/var/log (Logs)', value: '/var/log' },
    { label: '/tmp (Temporários)', value: '/tmp' },
    { label: '/app (Aplicação)', value: '/app' },
    { label: '/etc (Configurações)', value: '/etc' },
    { label: '/ (Raiz)', value: '/' },
  ];

  // Tela inicial - escolher diretório
  if (!hasStarted) {
    return (
      <div className="flex flex-col h-[500px] items-center justify-center gap-6 p-6">
        <div className="text-center space-y-2">
          <FolderOpen className="h-16 w-16 mx-auto text-muted-foreground" />
          <h3 className="text-lg font-semibold">Escolha o Diretório Inicial</h3>
          <p className="text-sm text-muted-foreground max-w-md">
            Selecione um diretório comum ou digite um caminho customizado para começar a navegação
          </p>
        </div>

        <div className="w-full max-w-md space-y-4">
          {/* Diretórios Comuns */}
          <div className="space-y-2">
            <Label>Diretórios Comuns</Label>
            <Select value={initialPath} onValueChange={setInitialPath}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {commonDirectories.map((dir) => (
                  <SelectItem key={dir.value} value={dir.value}>
                    {dir.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Caminho Customizado */}
          <div className="space-y-2">
            <Label>Ou Digite um Caminho Customizado</Label>
            <Input
              placeholder="/caminho/customizado"
              value={initialPath}
              onChange={(e) => setInitialPath(e.target.value)}
              onKeyPress={(e) => {
                if (e.key === 'Enter') {
                  startBrowsing();
                }
              }}
            />
          </div>

          {/* Botões */}
          <div className="flex gap-2">
            {onClose && (
              <Button
                variant="outline"
                className="flex-1"
                onClick={onClose}
              >
                Cancelar
              </Button>
            )}
            <Button
              className="flex-1"
              onClick={startBrowsing}
              disabled={!initialPath.trim()}
            >
              <FolderTree className="mr-2 h-4 w-4" />
              Iniciar Navegação
            </Button>
          </div>
        </div>
      </div>
    );
  }

  // File Browser Normal
  return (
    <div className="flex flex-col h-[500px]">
      {/* Breadcrumb Navigation */}
      <div className="flex items-center gap-2 p-3 border-b bg-muted/30">
        <Button
          variant="ghost"
          size="sm"
          onClick={resetBrowser}
          title="Voltar para Seleção de Diretório"
        >
          <ChevronRight className="h-4 w-4 rotate-180" />
        </Button>

        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigateToDirectory('/')}
          title="Home (Raiz)"
        >
          <Home className="h-4 w-4" />
        </Button>

        {currentPath !== '/' && (
          <Button
            variant="ghost"
            size="sm"
            onClick={navigateToParent}
            title="Voltar para Pasta Anterior"
          >
            <FolderOpen className="h-4 w-4" />
          </Button>
        )}

        <div className="flex items-center gap-1 flex-1 overflow-x-auto">
          <span className="text-sm text-muted-foreground">/</span>
          {pathParts.map((part, index) => (
            <div key={index} className="flex items-center gap-1">
              <Button
                variant="link"
                size="sm"
                className="h-auto p-0 text-sm"
                onClick={() => {
                  const path = '/' + pathParts.slice(0, index + 1).join('/');
                  navigateToDirectory(path);
                }}
              >
                {part}
              </Button>
              {index < pathParts.length - 1 && (
                <ChevronRight className="h-3 w-3 text-muted-foreground" />
              )}
            </div>
          ))}
        </div>

        <Badge variant="outline">{files.length} itens</Badge>
      </div>

      {/* Action Bar */}
      <div className="flex items-center justify-between p-3 border-b">
        <div className="flex items-center gap-2">
          <Checkbox
            checked={selectedFiles.size === files.length && files.length > 0}
            onCheckedChange={toggleSelectAll}
          />
          <span className="text-sm text-muted-foreground">
            {selectedFiles.size > 0
              ? `${selectedFiles.size} selecionado(s)`
              : 'Selecionar todos'}
          </span>
        </div>

        <Button
          size="sm"
          onClick={handleDownloadSelected}
          disabled={selectedFiles.size === 0}
        >
          <Download className="mr-2 h-4 w-4" />
          Download {selectedFiles.size > 1 ? `(${selectedFiles.size})` : ''}
        </Button>
      </div>

      {/* File List */}
      <div className="flex-1 overflow-hidden">
        <ScrollArea className="h-full">
          {isLoading ? (
            <div className="flex items-center justify-center h-[300px]">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : files.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-[300px] text-muted-foreground">
              <Folder className="h-12 w-12 mb-2 opacity-50" />
              <p>Diretório vazio</p>
            </div>
          ) : (
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[40px]"></TableHead>
                <TableHead>Nome</TableHead>
                <TableHead className="w-[120px]">Tamanho</TableHead>
                <TableHead className="w-[100px]">Permissões</TableHead>
                <TableHead className="w-[160px]">Modificado</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {files.map((file) => (
                <TableRow
                  key={file.path}
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={(e) => {
                    if (file.isDirectory) {
                      navigateToDirectory(file.path);
                    } else {
                      e.stopPropagation();
                      toggleFileSelection(file.path);
                    }
                  }}
                >
                  <TableCell>
                    <Checkbox
                      checked={selectedFiles.has(file.path)}
                      onCheckedChange={() => toggleFileSelection(file.path)}
                      onClick={(e) => e.stopPropagation()}
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      {file.isDirectory ? (
                        <Folder className="h-4 w-4 text-blue-500" />
                      ) : (
                        <File className="h-4 w-4 text-muted-foreground" />
                      )}
                      <span className="font-medium">{file.name}</span>
                    </div>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {file.isDirectory ? '-' : formatSize(file.size)}
                  </TableCell>
                  <TableCell className="text-xs font-mono text-muted-foreground">
                    {file.permissions}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {formatDate(file.modTime)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        </ScrollArea>
      </div>
    </div>
  );
};
