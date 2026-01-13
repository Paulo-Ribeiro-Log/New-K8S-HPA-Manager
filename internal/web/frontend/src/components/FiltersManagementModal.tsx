import { useState, useEffect } from "react";
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
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Filter,
  Trash2,
  Plus,
  Loader2,
  Info,
} from "lucide-react";
import { useFilters, type FilterType, type ResourceType, type FilterCategory } from "@/hooks/useFilters";
import { toast } from "sonner";

interface FiltersManagementModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export const FiltersManagementModal = ({ open, onOpenChange }: FiltersManagementModalProps) => {
  const { rules, stats, categories, loading, fetchRules, fetchCategories, addRule, removeRule } = useFilters();

  // Estado do formulário
  const [formType, setFormType] = useState<FilterType>("exact");
  const [formResourceType, setFormResourceType] = useState<ResourceType>("Deployment");
  const [formNamespace, setFormNamespace] = useState("");
  const [formName, setFormName] = useState("");
  const [formCategory, setFormCategory] = useState<FilterCategory | "">("");
  const [formReason, setFormReason] = useState("");
  const [submitting, setSubmitting] = useState(false);

  // Carregar dados ao abrir modal
  useEffect(() => {
    if (open) {
      fetchRules();
      fetchCategories();
    }
  }, [open, fetchRules, fetchCategories]);

  // Reset form
  const resetForm = () => {
    setFormType("exact");
    setFormResourceType("Deployment");
    setFormNamespace("");
    setFormName("");
    setFormCategory("");
    setFormReason("");
  };

  // Adicionar regra
  const handleAddRule = async () => {
    // Validação
    if (formType === "exact" && (!formNamespace || !formName)) {
      toast.error("Namespace e nome são obrigatórios para filtro exato");
      return;
    }

    if (formType === "namespace" && !formNamespace) {
      toast.error("Namespace é obrigatório");
      return;
    }

    if (formType === "category" && !formCategory) {
      toast.error("Categoria é obrigatória");
      return;
    }

    setSubmitting(true);

    const rule: any = {
      type: formType,
      resource_type: formResourceType,
      reason: formReason || "Filtro customizado",
    };

    // Adicionar campos específicos por tipo
    if (formType === "exact") {
      rule.namespace = formNamespace;
      rule.name = formName;
    } else if (formType === "namespace") {
      rule.namespace = formNamespace;
    } else if (formType === "category") {
      rule.category = formCategory;
    }

    const success = await addRule(rule);

    setSubmitting(false);

    if (success) {
      resetForm();
    }
  };

  // Deletar regra
  const handleDeleteRule = async (id: string) => {
    if (!confirm("Tem certeza que deseja remover este filtro?")) {
      return;
    }

    await removeRule(id);
  };

  // Badge de tipo
  const getTypeBadge = (type: FilterType) => {
    switch (type) {
      case "exact":
        return <Badge className="bg-blue-600 text-white">Exato</Badge>;
      case "namespace":
        return <Badge className="bg-green-600 text-white">Namespace</Badge>;
      case "category":
        return <Badge className="bg-purple-600 text-white">Categoria</Badge>;
      default:
        return <Badge variant="outline">{type}</Badge>;
    }
  };

  // Filtrar categorias por tipo de recurso
  const filteredCategories = categories.filter((cat) => {
    const value = cat.value;
    if (formResourceType === "Deployment") {
      return value.startsWith("deployment_");
    } else if (formResourceType === "ConfigMap") {
      return value.startsWith("configmap_");
    } else if (formResourceType === "Secret") {
      return value.startsWith("secret_");
    }
    return false;
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Filter className="h-5 w-5" />
            Gerenciar Filtros de Health Checking
          </DialogTitle>
          <DialogDescription>
            Configure quais recursos devem ser ignorados durante o health check.
            {stats && (
              <span className="ml-2">
                <strong>{stats.total}</strong> filtro(s) ativo(s): {stats.exact} exato(s), {stats.namespace} namespace(s), {stats.category} categoria(s)
              </span>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-4 flex-1 overflow-hidden">
          {/* Coluna esquerda: Lista de regras */}
          <div className="border rounded-lg p-3 overflow-hidden flex flex-col">
            <h3 className="text-sm font-semibold mb-2">Filtros Ativos</h3>

            {loading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            ) : rules.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
                <Info className="h-8 w-8 mb-2 opacity-50" />
                <p className="text-sm">Nenhum filtro configurado</p>
              </div>
            ) : (
              <ScrollArea className="flex-1">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-[80px]">Tipo</TableHead>
                      <TableHead className="w-[100px]">Recurso</TableHead>
                      <TableHead>Descrição</TableHead>
                      <TableHead className="w-[50px]"></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rules.map((rule) =>
                      <TableRow key={rule.id}>
                          <TableCell>{getTypeBadge(rule.type)}</TableCell>
                          <TableCell>
                            <Badge variant="secondary" className="text-xs">
                              {rule.resource_type}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            <div className="text-xs">
                              {rule.type === "exact" && (
                                <div>
                                  <strong>{rule.namespace}/{rule.name}</strong>
                                  <div className="text-muted-foreground">{rule.reason}</div>
                                </div>
                              )}
                              {rule.type === "namespace" && (
                                <div>
                                  <strong>Namespace: {rule.namespace}</strong>
                                  <div className="text-muted-foreground">{rule.reason}</div>
                                </div>
                              )}
                              {rule.type === "category" && (
                                <div>
                                  <strong>{rule.category}</strong>
                                  <div className="text-muted-foreground">{rule.reason}</div>
                                </div>
                              )}
                            </div>
                          </TableCell>
                          <TableCell>
                            {/* DEBUG: Mostrar created_by */}
                            <div className="text-xs text-muted-foreground mb-1">
                              {rule.created_by}
                            </div>
                            {rule.created_by !== "system" && (
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => handleDeleteRule(rule.id)}
                                className="h-7 w-7 p-0 text-red-600 hover:text-red-700"
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            )}
                          </TableCell>
                        </TableRow>
                    )}
                  </TableBody>
                </Table>
              </ScrollArea>
            )}
          </div>

          {/* Coluna direita: Formulário */}
          <div className="border rounded-lg p-3 overflow-hidden flex flex-col">
            <h3 className="text-sm font-semibold mb-2">Adicionar Novo Filtro</h3>

            <ScrollArea className="flex-1">
              <div className="space-y-3">
                {/* Tipo de filtro */}
                <div className="space-y-1">
                  <Label htmlFor="filter-type" className="text-xs">Tipo de Filtro</Label>
                  <Select value={formType} onValueChange={(val) => setFormType(val as FilterType)}>
                    <SelectTrigger id="filter-type" className="h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="exact">Exato (namespace/nome específico)</SelectItem>
                      <SelectItem value="namespace">Namespace (todos recursos do namespace)</SelectItem>
                      <SelectItem value="category">Categoria (todos recursos com mesmo problema)</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {/* Tipo de recurso */}
                <div className="space-y-1">
                  <Label htmlFor="resource-type" className="text-xs">Tipo de Recurso</Label>
                  <Select value={formResourceType} onValueChange={(val) => setFormResourceType(val as ResourceType)}>
                    <SelectTrigger id="resource-type" className="h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="Deployment">Deployment</SelectItem>
                      <SelectItem value="ConfigMap">ConfigMap</SelectItem>
                      <SelectItem value="Secret">Secret</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {/* Campos condicionais por tipo */}
                {formType === "exact" && (
                  <>
                    <div className="space-y-1">
                      <Label htmlFor="namespace" className="text-xs">Namespace</Label>
                      <Input
                        id="namespace"
                        placeholder="ex: ingress-nginx-external"
                        value={formNamespace}
                        onChange={(e) => setFormNamespace(e.target.value)}
                        className="h-8"
                      />
                    </div>

                    <div className="space-y-1">
                      <Label htmlFor="name" className="text-xs">Nome do Recurso</Label>
                      <Input
                        id="name"
                        placeholder="ex: tcp-services"
                        value={formName}
                        onChange={(e) => setFormName(e.target.value)}
                        className="h-8"
                      />
                    </div>
                  </>
                )}

                {formType === "namespace" && (
                  <div className="space-y-1">
                    <Label htmlFor="namespace-only" className="text-xs">Namespace</Label>
                    <Input
                      id="namespace-only"
                      placeholder="ex: kube-system"
                      value={formNamespace}
                      onChange={(e) => setFormNamespace(e.target.value)}
                      className="h-8"
                    />
                  </div>
                )}

                {formType === "category" && (
                  <div className="space-y-1">
                    <Label htmlFor="category" className="text-xs">Categoria</Label>
                    <Select value={formCategory} onValueChange={(val) => setFormCategory(val as FilterCategory)}>
                      <SelectTrigger id="category" className="h-8">
                        <SelectValue placeholder="Selecione uma categoria" />
                      </SelectTrigger>
                      <SelectContent>
                        {filteredCategories.map((cat) => (
                          <SelectItem key={cat.value} value={cat.value}>
                            <div>
                              <div className="font-medium">{cat.label}</div>
                              <div className="text-xs text-muted-foreground">{cat.description}</div>
                            </div>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}

                {/* Motivo */}
                <div className="space-y-1">
                  <Label htmlFor="reason" className="text-xs">Motivo (Opcional)</Label>
                  <Textarea
                    id="reason"
                    placeholder="Descrição do motivo do filtro..."
                    value={formReason}
                    onChange={(e) => setFormReason(e.target.value)}
                    className="h-16 resize-none"
                  />
                </div>

                {/* Botões */}
                <div className="flex gap-2 pt-2">
                  <Button
                    onClick={handleAddRule}
                    disabled={submitting}
                    className="flex-1"
                    size="sm"
                  >
                    {submitting ? (
                      <>
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                        Adicionando...
                      </>
                    ) : (
                      <>
                        <Plus className="mr-2 h-4 w-4" />
                        Adicionar Filtro
                      </>
                    )}
                  </Button>
                  <Button
                    variant="outline"
                    onClick={resetForm}
                    size="sm"
                  >
                    Limpar
                  </Button>
                </div>

                {/* Info box */}
                <div className="bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded p-2 text-xs">
                  <div className="font-medium mb-1">💡 Dica</div>
                  <ul className="list-disc list-inside space-y-0.5 text-muted-foreground">
                    <li><strong>Exato:</strong> Ignora recurso específico (ex: ingress-nginx/tcp-services)</li>
                    <li><strong>Namespace:</strong> Ignora TODOS recursos do tipo em um namespace</li>
                    <li><strong>Categoria:</strong> Ignora TODOS recursos com mesmo tipo de problema</li>
                  </ul>
                </div>
              </div>
            </ScrollArea>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
