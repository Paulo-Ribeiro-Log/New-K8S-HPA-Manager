import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Plus, X, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { Namespace } from "@/lib/api/types";

interface CreateSecretModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespaces: Namespace[];
  onSuccess?: () => void;
}

type SecretType = "delinea" | "azure-key-vault";

interface Annotation {
  key: string;
  value: string;
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
  const [isCreating, setIsCreating] = useState(false);

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

  const generateSecretYAML = (): string => {
    const annotationsObj: Record<string, string> = {};
    annotations.forEach((ann) => {
      if (ann.key && ann.value) {
        annotationsObj[ann.key] = ann.value;
      }
    });

    if (secretType === "delinea") {
      return `apiVersion: v1
kind: Secret
metadata:
  name: ${name}
  namespace: ${namespace}${Object.keys(annotationsObj).length > 0 ? '\n  annotations:' : ''}${Object.entries(annotationsObj).map(([k, v]) => `\n    ${k}: ${v}`).join('')}
type: Opaque
data:
  placeholder: cGxhY2Vob2xkZXI=`;
    } else {
      // Azure Key Vault
      return `apiVersion: v1
kind: Secret
metadata:
  name: ${name}
  namespace: ${namespace}${Object.keys(annotationsObj).length > 0 ? '\n  annotations:' : ''}${Object.entries(annotationsObj).map(([k, v]) => `\n    ${k}: ${v}`).join('')}
type: Opaque
data:
  placeholder: cGxhY2Vob2xkZXI=`;
    }
  };

  const handleCreate = async () => {
    if (!name.trim()) {
      toast.error("Nome do secret é obrigatório");
      return;
    }

    if (!namespace) {
      toast.error("Namespace é obrigatório");
      return;
    }

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
    setSecretType("delinea");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Criar Novo Secret</DialogTitle>
          <DialogDescription>
            Crie um novo secret no cluster selecionado
          </DialogDescription>
        </DialogHeader>

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
                <SelectItem value="azure-key-vault">Azure Key Vault</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Nome */}
          <div className="space-y-2">
            <Label htmlFor="secret-name">Nome *</Label>
            <Input
              id="secret-name"
              placeholder="ex: my-secret"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          {/* Namespace */}
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

          {/* Annotations */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>Annotations (opcional)</Label>
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
              {annotations.map((annotation, index) => (
                <div key={index} className="flex gap-2 items-start">
                  <div className="flex-1">
                    <Input
                      placeholder="Chave (ex: app)"
                      value={annotation.key}
                      onChange={(e) => handleAnnotationChange(index, "key", e.target.value)}
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
              ))}
            </div>
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
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={handleCancel} disabled={isCreating}>
            Cancelar
          </Button>
          <Button onClick={handleCreate} disabled={isCreating}>
            {isCreating && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
            Criar Secret
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
};
