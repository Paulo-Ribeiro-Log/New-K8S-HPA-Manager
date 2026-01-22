import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import {
  Loader2,
  Download,
  CheckCircle2,
  AlertCircle,
  FileText,
  GitBranch,
  Package,
  Users,
  ExternalLink,
  Copy,
  ClipboardPaste,
} from "lucide-react";
import { toast } from "sonner";

interface ExtractedData {
  application: string;
  version: string;
  github_repo: string;
  xlrelease_url: string;
  squad: string;
  branch: string;
  jira_issues: string[];
  product: string;
  project: string;
  xlrelease_title: string;
  severity: string;
  confidence: number;
}

interface ChangeRequest {
  sys_id: string;
  number: string;
  short_description: string;
  description: string;
  state: string;
}

interface ImportResponse {
  success: boolean;
  change_request?: ChangeRequest;
  extracted_data?: ExtractedData;
  error?: string;
}

interface ServiceNowImportModalProps {
  open: boolean;
  onClose: () => void;
  onImportSuccess: (data: {
    deploymentName: string;
    githubRepo: string;
    newVersion: string;
    xlReleaseUrl?: string;
    changeNumber?: string;
  }) => void;
}

export function ServiceNowImportModal({
  open,
  onClose,
  onImportSuccess,
}: ServiceNowImportModalProps) {
  const [chgUrl, setChgUrl] = useState("");
  const [description, setDescription] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [result, setResult] = useState<ImportResponse | null>(null);

  const handleOpenServiceNow = () => {
    if (chgUrl.trim()) {
      window.open(chgUrl.trim(), "_blank");
    } else {
      window.open("https://viavarejo.service-now.com/nav_to.do?uri=change_request_list.do", "_blank");
    }
  };

  const handlePasteFromClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        setDescription(text);
        toast.success("Texto colado da área de transferência");
      }
    } catch (error) {
      toast.error("Não foi possível acessar a área de transferência");
    }
  };

  const handleParseDescription = async () => {
    if (!description.trim()) {
      toast.error("Cole o texto do 'Motivo da mudança'");
      return;
    }

    setIsLoading(true);
    setResult(null);

    try {
      const response = await fetch("/api/v1/servicenow/parse", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${localStorage.getItem('token') || 'poc-token-123'}`,
        },
        body: JSON.stringify({ description: description.trim() }),
      });

      const data = await response.json();

      if (!data.success) {
        const errorMsg = typeof data.error === 'string'
          ? data.error
          : data.error?.message || "Erro ao extrair dados";
        toast.error(errorMsg);
        return;
      }

      setResult({
        success: true,
        extracted_data: data.extracted_data,
      });
      toast.success("Dados extraídos com sucesso!");
    } catch (error) {
      toast.error("Erro ao conectar com o servidor");
      console.error("Parse error:", error);
    } finally {
      setIsLoading(false);
    }
  };

  const handleUseData = () => {
    if (!result?.extracted_data) {
      toast.error("Nenhum dado para usar");
      return;
    }

    const data = result.extracted_data;

    onImportSuccess({
      deploymentName: data.application,
      githubRepo: data.github_repo,
      newVersion: data.version,
      xlReleaseUrl: data.xlrelease_url,
      changeNumber: result.change_request?.number,
    });

    handleClose();
    toast.success("Campos preenchidos automaticamente!");
  };

  const handleClose = () => {
    setChgUrl("");
    setDescription("");
    setResult(null);
    onClose();
  };

  const getConfidenceColor = (confidence: number) => {
    if (confidence >= 0.9) return "bg-green-500";
    if (confidence >= 0.7) return "bg-yellow-500";
    return "bg-red-500";
  };

  const getConfidenceText = (confidence: number) => {
    const percent = Math.round(confidence * 100);
    if (confidence >= 0.9) return `${percent}% - Alta`;
    if (confidence >= 0.7) return `${percent}% - Média`;
    return `${percent}% - Baixa`;
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Download className="h-5 w-5" />
            Importar de ServiceNow
          </DialogTitle>
          <DialogDescription>
            Importe os dados de uma CHG para preencher automaticamente os campos.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* Passo 1: Abrir ServiceNow */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="rounded-full">1</Badge>
              <Label className="font-medium">Abrir a CHG no ServiceNow</Label>
            </div>
            <div className="flex gap-2">
              <Input
                placeholder="Cole a URL da CHG (opcional)"
                value={chgUrl}
                onChange={(e) => setChgUrl(e.target.value)}
                className="flex-1"
              />
              <Button variant="outline" onClick={handleOpenServiceNow}>
                <ExternalLink className="h-4 w-4 mr-2" />
                Abrir
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Cole a URL da CHG e clique em "Abrir", ou deixe em branco para abrir a lista de CHGs.
            </p>
          </div>

          <Separator />

          {/* Passo 2: Copiar texto */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="rounded-full">2</Badge>
              <Label className="font-medium">Copiar o campo "Motivo da mudança"</Label>
            </div>
            <Alert>
              <Copy className="h-4 w-4" />
              <AlertDescription>
                Na página da CHG, localize o campo <strong>"Motivo da mudança"</strong> e copie todo o conteúdo (Ctrl+A, Ctrl+C).
              </AlertDescription>
            </Alert>
          </div>

          <Separator />

          {/* Passo 3: Colar e extrair */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="rounded-full">3</Badge>
                <Label className="font-medium">Colar o texto aqui</Label>
              </div>
              <Button variant="ghost" size="sm" onClick={handlePasteFromClipboard}>
                <ClipboardPaste className="h-4 w-4 mr-1" />
                Colar
              </Button>
            </div>
            <Textarea
              placeholder="Cole aqui o conteúdo do campo 'Motivo da mudança'..."
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={isLoading}
              rows={8}
              className="font-mono text-xs"
            />
          </div>

          <Button
            onClick={handleParseDescription}
            disabled={isLoading || !description.trim()}
            className="w-full"
            size="lg"
          >
            {isLoading ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Extraindo dados...
              </>
            ) : (
              <>
                <FileText className="h-4 w-4 mr-2" />
                Extrair Dados da CHG
              </>
            )}
          </Button>
        </div>

        {/* Resultado */}
        {result && result.success && result.extracted_data && (
          <>
            <Separator />

            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="font-medium text-sm flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 text-green-500" />
                  Dados Extraídos
                </span>
                <Badge className={getConfidenceColor(result.extracted_data.confidence)}>
                  Confiança: {getConfidenceText(result.extracted_data.confidence)}
                </Badge>
              </div>

              <div className="grid grid-cols-2 gap-3 text-sm bg-muted p-4 rounded-lg">
                {/* Aplicação */}
                <div className="flex items-center gap-2">
                  <Package className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Aplicação:</span>
                </div>
                <span className="font-mono truncate font-medium">
                  {result.extracted_data.application || "-"}
                </span>

                {/* Versão */}
                <div className="flex items-center gap-2">
                  <GitBranch className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Versão:</span>
                </div>
                <span className="font-mono font-medium">
                  {result.extracted_data.version || "-"}
                </span>

                {/* Repositório */}
                <div className="flex items-center gap-2">
                  <GitBranch className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Repositório:</span>
                </div>
                <span className="font-mono truncate font-medium">
                  {result.extracted_data.github_repo || "-"}
                </span>

                {/* Squad */}
                <div className="flex items-center gap-2">
                  <Users className="h-4 w-4 text-muted-foreground" />
                  <span className="text-muted-foreground">Squad:</span>
                </div>
                <span className="truncate font-medium">
                  {result.extracted_data.squad || "-"}
                </span>
              </div>

              {/* Jira Issues */}
              {result.extracted_data.jira_issues && result.extracted_data.jira_issues.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {result.extracted_data.jira_issues.map((issue, idx) => (
                    <Badge key={idx} variant="secondary" className="text-xs">
                      {issue}
                    </Badge>
                  ))}
                </div>
              )}

              {/* XL Release URL */}
              {result.extracted_data.xlrelease_url && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={() => window.open(result.extracted_data!.xlrelease_url, "_blank")}
                >
                  <ExternalLink className="h-4 w-4 mr-2" />
                  Abrir no XL Release
                </Button>
              )}

              {/* Aviso de revisão */}
              {(!result.extracted_data.application || !result.extracted_data.version) && (
                <Alert>
                  <AlertCircle className="h-4 w-4" />
                  <AlertDescription>
                    Alguns campos não foram encontrados. Você precisará preenchê-los manualmente.
                  </AlertDescription>
                </Alert>
              )}

              {/* Botões de ação */}
              <div className="flex gap-2 justify-end">
                <Button variant="outline" onClick={handleClose}>
                  Cancelar
                </Button>
                <Button
                  onClick={handleUseData}
                  disabled={!result.extracted_data?.application && !result.extracted_data?.version}
                >
                  <CheckCircle2 className="h-4 w-4 mr-2" />
                  Usar Dados
                </Button>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
