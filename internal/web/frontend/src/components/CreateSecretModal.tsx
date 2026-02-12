import { useState, useEffect } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Plus, X, Loader2, Key, Code2, AlertCircle } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { Namespace } from "@/lib/api/types";
import { MonacoYamlEditor } from "./MonacoYamlEditor";
import * as yaml from "js-yaml";

interface CreateSecretModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespaces: Namespace[];
  onSuccess?: () => void;
}

type SecretType = "delinea" | "normal-secret" | "custom";

interface Annotation {
  key: string;
  value: string;
}

interface DataEntry {
  key: string;
  value: string;
  encodeBase64: boolean;
}

export const CreateSecretModal = ({
  open,
  onOpenChange,
  cluster,
  namespaces,
  onSuccess,
}: CreateSecretModalProps) => {
  const [secretType, setSecretType] = useState<SecretType>("delinea");
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("");
  const [annotations, setAnnotations] = useState<Annotation[]>([{ key: "", value: "" }]);
  const [dataEntries, setDataEntries] = useState<DataEntry[]>([{ key: "", value: "", encodeBase64: true }]);
  const [isCreating, setIsCreating] = useState(false);
  const [customYaml, setCustomYaml] = useState("");
  const [yamlError, setYamlError] = useState<string | null>(null);

  // Template YAML para modo Custom (sem name/namespace - serão injetados automaticamente)
  const generateCustomTemplate = (): string => {
    return `# Edite apenas o conteúdo do Secret abaixo
# Name e Namespace serão definidos pelos campos acima

type: Opaque

# Labels (opcional)
labels: {}

# Annotations (opcional)
annotations: {}

# Dados em base64
data:
  # username: YWRtaW4=
  # password: c2VuaGExMjM=

# Ou dados em texto plano (serão convertidos automaticamente para base64)
stringData:
  # exemplo-chave: valor-em-texto-plano
`;
  };

  // Inicializar template quando mudar para modo custom
  useEffect(() => {
    if (secretType === "custom" && !customYaml) {
      setCustomYaml(generateCustomTemplate());
    }
  }, [secretType]);

  // Validar YAML em tempo real no modo custom (apenas sintaxe, name/namespace vêm dos campos)
  useEffect(() => {
    if (secretType === "custom" && customYaml) {
      try {
        const parsed = yaml.load(customYaml) as Record<string, unknown>;
        if (!parsed || typeof parsed !== "object") {
          setYamlError("YAML deve ser um objeto valido");
          return;
        }
        // Validação básica - apenas sintaxe YAML
        setYamlError(null);
      } catch (e) {
        setYamlError(e instanceof Error ? e.message : "YAML invalido");
      }
    } else {
      setYamlError(null);
    }
  }, [customYaml, secretType]);

  // Função para encodar em base64
  const toBase64 = (str: string): string => {
    try {
      return btoa(unescape(encodeURIComponent(str)));
    } catch {
      return btoa(str);
    }
  };

  // Atualizar annotations quando o tipo de secret mudar
  useEffect(() => {
    if (secretType === "delinea") {
      setAnnotations([
        { key: "dsv.delinea.com/credentials", value: "" },
        { key: "dsv.delinea.com/set-secret", value: "" }
      ]);
    } else if (secretType === "custom") {
      // No modo custom, inicializar template se vazio
      if (!customYaml) {
        setCustomYaml(generateCustomTemplate());
      }
    } else {
      setAnnotations([{ key: "", value: "" }]);
    }
  }, [secretType]);

  const handleAddAnnotation = () => {
    setAnnotations([...annotations, { key: "", value: "" }]);
  };

  const handleRemoveAnnotation = (index: number) => {
    setAnnotations(annotations.filter((_, i) => i !== index));
  };

  const handleAnnotationChange = (index: number, field: "key" | "value", value: string) => {
    const newAnnotations = [...annotations];
    newAnnotations[index][field] = value;
    setAnnotations(newAnnotations);
  };

  // Funções para manipular Data entries
  const handleAddDataEntry = () => {
    setDataEntries([...dataEntries, { key: "", value: "", encodeBase64: true }]);
  };

  const handleRemoveDataEntry = (index: number) => {
    setDataEntries(dataEntries.filter((_, i) => i !== index));
  };

  const handleDataEntryChange = (index: number, field: "key" | "value", value: string) => {
    const newEntries = [...dataEntries];
    newEntries[index][field] = value;
    setDataEntries(newEntries);
  };

  const handleToggleEncode = (index: number) => {
    const newEntries = [...dataEntries];
    newEntries[index].encodeBase64 = !newEntries[index].encodeBase64;
    setDataEntries(newEntries);
  };

  const generateSecretYAML = (): string => {
    const annotationsObj: Record<string, string> = {};
    annotations.forEach((ann) => {
      if (ann.key && ann.value) {
        annotationsObj[ann.key] = ann.value;
      }
    });

    // Gerar data entries com encode base64 quando necessário
    const dataObj: Record<string, string> = {};
    dataEntries.forEach((entry) => {
      if (entry.key && entry.value) {
        dataObj[entry.key] = entry.encodeBase64 ? toBase64(entry.value) : entry.value;
      }
    });

    // Se não há dados, adicionar placeholder
    const hasData = Object.keys(dataObj).length > 0;
    const dataSection = hasData
      ? Object.entries(dataObj).map(([k, v]) => `  ${k}: ${v}`).join('\n')
      : '  placeholder: cGxhY2Vob2xkZXI=';

    return `apiVersion: v1
kind: Secret
metadata:
  name: ${name}
  namespace: ${namespace}${Object.keys(annotationsObj).length > 0 ? '\n  annotations:' : ''}${Object.entries(annotationsObj).map(([k, v]) => `\n    ${k}: ${v}`).join('')}
type: Opaque
data:
${dataSection}`;
  };

  // Gerar YAML final para modo custom (mescla campos do form com YAML editado)
  const generateCustomSecretYAML = (): string => {
    try {
      const parsed = yaml.load(customYaml) as Record<string, unknown> || {};

      // Construir objeto final do Secret
      const secretObj: Record<string, unknown> = {
        apiVersion: "v1",
        kind: "Secret",
        metadata: {
          name: name,
          namespace: namespace,
          ...(parsed.labels && Object.keys(parsed.labels as object).length > 0
            ? { labels: parsed.labels }
            : {}),
          ...(parsed.annotations && Object.keys(parsed.annotations as object).length > 0
            ? { annotations: parsed.annotations }
            : {}),
        },
        type: (parsed.type as string) || "Opaque",
      };

      // Adicionar data se existir
      if (parsed.data && Object.keys(parsed.data as object).length > 0) {
        secretObj.data = parsed.data;
      }

      // Adicionar stringData se existir
      if (parsed.stringData && Object.keys(parsed.stringData as object).length > 0) {
        secretObj.stringData = parsed.stringData;
      }

      return yaml.dump(secretObj, { indent: 2, lineWidth: -1 });
    } catch {
      return "";
    }
  };

  const handleCreate = async () => {
    // Validação comum: nome e namespace são sempre obrigatórios
    if (!name.trim()) {
      toast.error("Nome do secret é obrigatório");
      return;
    }

    if (!namespace) {
      toast.error("Namespace é obrigatório");
      return;
    }

    // Validações específicas por modo
    if (secretType === "custom") {
      if (!customYaml.trim()) {
        toast.error("YAML do secret é obrigatório");
        return;
      }
      if (yamlError) {
        toast.error("Corrija os erros no YAML antes de criar");
        return;
      }

      // Gerar YAML final mesclando campos do form com YAML editado
      const finalYaml = generateCustomSecretYAML();
      if (!finalYaml) {
        toast.error("Erro ao processar YAML");
        return;
      }

      setIsCreating(true);
      try {
        await apiClient.createSecret(cluster, namespace, {
          yaml: finalYaml,
          fieldManager: "web-secret-creator-custom",
        });

        toast.success("Secret criado com sucesso", {
          description: `${namespace}/${name}`,
        });

        // Reset form
        setName("");
        setNamespace("");
        setAnnotations([{ key: "", value: "" }]);
        setDataEntries([{ key: "", value: "", encodeBase64: true }]);
        setCustomYaml("");
        setSecretType("delinea");

        onOpenChange(false);
        onSuccess?.();
      } catch (err) {
        toast.error("Falha ao criar secret", {
          description: err instanceof Error ? err.message : "Erro desconhecido",
        });
      } finally {
        setIsCreating(false);
      }
      return;
    }

    // Modo normal (delinea ou normal-secret)
    setIsCreating(true);
    try {
      const secretYAML = generateSecretYAML();

      await apiClient.createSecret(cluster, namespace, {
        yaml: secretYAML,
        fieldManager: "web-secret-creator",
      });

      toast.success("Secret criado com sucesso", {
        description: `${namespace}/${name}`,
      });

      // Reset form
      setName("");
      setNamespace("");
      setAnnotations([{ key: "", value: "" }]);
      setDataEntries([{ key: "", value: "", encodeBase64: true }]);
      setCustomYaml("");
      setSecretType("delinea");

      onOpenChange(false);
      onSuccess?.();
    } catch (err) {
      toast.error("Falha ao criar secret", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setIsCreating(false);
    }
  };

  const handleCancel = () => {
    setName("");
    setNamespace("");
    setAnnotations([{ key: "", value: "" }]);
    setDataEntries([{ key: "", value: "", encodeBase64: true }]);
    setCustomYaml("");
    setYamlError(null);
    setSecretType("delinea");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[1400px] max-h-[95vh] overflow-hidden">
        <DialogHeader>
          <DialogTitle>Criar Novo Secret</DialogTitle>
          <DialogDescription>
            Crie um novo secret no cluster selecionado
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="h-[calc(95vh-220px)] pr-4">
          <div className="space-y-4 py-4">
          {/* Tipo de Secret */}
          <div className="space-y-2">
            <Label htmlFor="secret-type">Tipo de Secret</Label>
            <Select value={secretType} onValueChange={(value) => setSecretType(value as SecretType)}>
              <SelectTrigger id="secret-type">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="delinea">Delinea</SelectItem>
                <SelectItem value="normal-secret">Normal secret</SelectItem>
                <SelectItem value="custom">
                  <span className="flex items-center gap-2">
                    <Code2 className="h-4 w-4" />
                    Custom (YAML)
                  </span>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Nome - sempre visível */}
          <div className="space-y-2">
            <Label htmlFor="secret-name">Nome *</Label>
            <Input
              id="secret-name"
              placeholder="ex: my-secret"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          {/* Namespace - sempre visível */}
          <div className="space-y-2">
            <Label htmlFor="secret-namespace">Namespace *</Label>
            <Select value={namespace} onValueChange={setNamespace}>
              <SelectTrigger id="secret-namespace">
                <SelectValue placeholder="Selecione um namespace" />
              </SelectTrigger>
              <SelectContent>
                {namespaces.map((ns) => (
                  <SelectItem key={ns.name} value={ns.name}>
                    {ns.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Modo Custom - Monaco Editor para conteúdo */}
          {secretType === "custom" && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label className="flex items-center gap-2">
                  <Code2 className="h-4 w-4" />
                  Conteúdo do Secret (YAML)
                </Label>
                {yamlError && (
                  <div className="flex items-center gap-1 text-xs text-destructive">
                    <AlertCircle className="h-3 w-3" />
                    {yamlError}
                  </div>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                Edite type, labels, annotations, data e stringData. Os campos apiVersion, kind, name e namespace serão adicionados automaticamente.
              </p>
              <MonacoYamlEditor
                value={customYaml}
                onChange={setCustomYaml}
                height={500}
                readOnly={false}
              />
            </div>
          )}

          {/* Campos para modos Delinea e Normal Secret */}
          {secretType !== "custom" && (
            <>
              {/* Annotations */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label>Annotations {secretType === "normal-secret" && "(opcional)"}</Label>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleAddAnnotation}
                  >
                    <Plus className="w-4 h-4 mr-1" />
                    Adicionar
                  </Button>
                </div>

                <div className="space-y-2">
                  {annotations.map((annotation, index) => {
                    // Desabilitar chave apenas para as annotations pré-definidas do Delinea
                    const isDelineaPreset = secretType === "delinea" &&
                      (annotation.key === "dsv.delinea.com/credentials" ||
                       annotation.key === "dsv.delinea.com/set-secret");

                    return (
                      <div key={index} className="flex gap-2 items-start">
                        <div className="flex-1">
                          <Input
                            placeholder="Chave (ex: app)"
                            value={annotation.key}
                            onChange={(e) => handleAnnotationChange(index, "key", e.target.value)}
                            disabled={isDelineaPreset}
                          />
                        </div>
                        <div className="flex-1">
                          <Input
                            placeholder="Valor (ex: myapp)"
                            value={annotation.value}
                            onChange={(e) => handleAnnotationChange(index, "value", e.target.value)}
                          />
                        </div>
                        {annotations.length > 1 && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            onClick={() => handleRemoveAnnotation(index)}
                          >
                            <X className="w-4 h-4" />
                          </Button>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>

              {/* Data (chave/valor) */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label className="flex items-center gap-2">
                    <Key className="w-4 h-4" />
                    Data (chave/valor)
                  </Label>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleAddDataEntry}
                  >
                    <Plus className="w-4 h-4 mr-1" />
                    Adicionar
                  </Button>
                </div>

                <div className="space-y-3">
                  {dataEntries.map((entry, index) => (
                    <div key={index} className="flex gap-2 items-center border rounded-md p-2 bg-muted/30">
                      <div className="flex-1">
                        <Input
                          placeholder="Chave (ex: username)"
                          value={entry.key}
                          onChange={(e) => handleDataEntryChange(index, "key", e.target.value)}
                          className="text-sm"
                        />
                      </div>
                      <div className="flex-1">
                        <Input
                          placeholder="Valor (ex: admin)"
                          value={entry.value}
                          onChange={(e) => handleDataEntryChange(index, "value", e.target.value)}
                          className="text-sm"
                        />
                      </div>
                      <div className="flex items-center gap-2 min-w-[120px]">
                        <Switch
                          id={`encode-${index}`}
                          checked={entry.encodeBase64}
                          onCheckedChange={() => handleToggleEncode(index)}
                        />
                        <Label htmlFor={`encode-${index}`} className="text-xs cursor-pointer">
                          Base64
                        </Label>
                      </div>
                      {dataEntries.length > 1 && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          onClick={() => handleRemoveDataEntry(index)}
                        >
                          <X className="w-4 h-4" />
                        </Button>
                      )}
                    </div>
                  ))}
                </div>
                <p className="text-xs text-muted-foreground">
                  Ative "Base64" para encodar o valor automaticamente. Desative se o valor já estiver em base64.
                </p>
              </div>

              {/* Preview do YAML */}
              {name && namespace && (
                <div className="space-y-2">
                  <Label>Preview do YAML</Label>
                  <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto">
                    {generateSecretYAML()}
                  </pre>
                </div>
              )}
            </>
          )}
          </div>
        </ScrollArea>

        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={handleCancel} disabled={isCreating}>
            Cancelar
          </Button>
          <Button
            onClick={handleCreate}
            disabled={isCreating || !name.trim() || !namespace || (secretType === "custom" && !!yamlError)}
          >
            {isCreating && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
            Criar Secret
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
};
