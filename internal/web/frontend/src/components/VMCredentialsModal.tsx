import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, KeyRound, Plus, Pencil, Trash2, X, Copy, Check, RefreshCcw } from "lucide-react";
import { toast } from "sonner";
import { useVMCredentialProfiles } from "@/hooks/useVMs";
import { apiClient } from "@/lib/api/client";
import type { SSHCredentialProfile, SaveSSHCredentialProfileInput, LocalSSHKeyEntry } from "@/lib/api/types";

interface VMCredentialsModalProps {
  open: boolean;
  onClose: () => void;
}

const emptyForm: SaveSSHCredentialProfileInput = {
  name: "",
  username: "",
  authMethod: "key",
  privateKeyPEM: "",
  passphrase: "",
  password: "",
};

// keySource — só relevante ao CRIAR um perfil novo com authMethod "key" (editar sempre usa
// "paste", mesmo comportamento de sempre: reentrar o segredo pra trocá-lo, ou deixar em branco
// pra manter o atual). Pedido explícito do usuário: além de colar uma chave já existente, também
// gerar um par novo (RSA/ED25519) ou importar de uma chave já presente em ~/.ssh do servidor.
type KeySource = "paste" | "generate" | "local";

// VMCredentialsModal — gerencia os perfis de credencial SSH do usuário logado (Fase 3 do plano).
// Só metadados trafegam de volta do backend (nome/usuário/método) — o segredo em si nunca é
// reexibido depois de salvo (mesmo princípio de GetGitHubProfiles/maskGitHubToken), então editar
// um perfil existente sempre exige informar o segredo de novo (chave/senha), nunca vem
// pré-preenchido.
export default function VMCredentialsModal({ open, onClose }: VMCredentialsModalProps) {
  const { profiles, loading, refetch } = useVMCredentialProfiles();
  const [editing, setEditing] = useState<SSHCredentialProfile | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<SaveSSHCredentialProfileInput>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SSHCredentialProfile | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [keySource, setKeySource] = useState<KeySource>("paste");
  const [keyType, setKeyType] = useState<"rsa" | "ed25519">("ed25519");
  const [rsaBits, setRsaBits] = useState("4096");
  const [generatedPublicKey, setGeneratedPublicKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const [localKeys, setLocalKeys] = useState<LocalSSHKeyEntry[]>([]);
  const [loadingLocalKeys, setLoadingLocalKeys] = useState(false);
  const [selectedLocalKeyPath, setSelectedLocalKeyPath] = useState("");

  const fetchLocalKeys = () => {
    setLoadingLocalKeys(true);
    apiClient
      .listLocalVMSSHKeys()
      .then(setLocalKeys)
      .catch(() => setLocalKeys([]))
      .finally(() => setLoadingLocalKeys(false));
  };

  useEffect(() => {
    if (creating && !editing && form.authMethod === "key" && keySource === "local") {
      fetchLocalKeys();
    }
  }, [creating, editing, form.authMethod, keySource]);

  const resetKeySourceState = () => {
    setKeySource("paste");
    setKeyType("ed25519");
    setRsaBits("4096");
    setGeneratedPublicKey(null);
    setSelectedLocalKeyPath("");
    setLocalKeys([]);
  };

  const startCreate = () => {
    setForm(emptyForm);
    setEditing(null);
    resetKeySourceState();
    setCreating(true);
  };

  const startEdit = (p: SSHCredentialProfile) => {
    setForm({ name: p.name, username: p.username, authMethod: p.authMethod, privateKeyPEM: "", passphrase: "", password: "" });
    setEditing(p);
    resetKeySourceState();
    setCreating(true);
  };

  const cancelForm = () => {
    setCreating(false);
    setEditing(null);
    setForm(emptyForm);
    resetKeySourceState();
  };

  const handleSavePasteOrPassword = async () => {
    if (form.authMethod === "key" && !form.privateKeyPEM?.trim()) {
      toast.error("Informe a chave privada");
      return;
    }
    if (form.authMethod === "password" && !form.password?.trim()) {
      toast.error("Informe a senha");
      return;
    }
    setSaving(true);
    try {
      if (editing) {
        await apiClient.updateVMCredentialProfile(editing.id, form);
        toast.success("Perfil atualizado");
      } else {
        await apiClient.createVMCredentialProfile(form);
        toast.success("Perfil criado");
      }
      cancelForm();
      refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao salvar perfil");
    } finally {
      setSaving(false);
    }
  };

  const handleGenerate = async () => {
    setSaving(true);
    try {
      const result = await apiClient.generateVMSSHKeyPair({
        name: form.name,
        username: form.username,
        keyType,
        bits: keyType === "rsa" ? Number(rsaBits) : undefined,
      });
      setGeneratedPublicKey(result.publicKey);
      toast.success("Chave gerada e perfil salvo");
      refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao gerar chave");
    } finally {
      setSaving(false);
    }
  };

  const handleImportLocal = async () => {
    if (!selectedLocalKeyPath) {
      toast.error("Selecione uma chave em ~/.ssh");
      return;
    }
    setSaving(true);
    try {
      await apiClient.importLocalVMSSHKey({
        path: selectedLocalKeyPath,
        name: form.name,
        username: form.username,
        passphrase: form.passphrase,
      });
      toast.success("Chave importada e perfil salvo");
      cancelForm();
      refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao importar chave");
    } finally {
      setSaving(false);
    }
  };

  const handleSave = () => {
    if (!form.name.trim() || !form.username.trim()) {
      toast.error("Nome e usuário são obrigatórios");
      return;
    }
    if (editing || form.authMethod === "password" || keySource === "paste") {
      handleSavePasteOrPassword();
    } else if (keySource === "generate") {
      handleGenerate();
    } else {
      handleImportLocal();
    }
  };

  const handleCopyPublicKey = () => {
    if (!generatedPublicKey) return;
    navigator.clipboard.writeText(generatedPublicKey);
    setCopied(true);
    toast.success("Chave pública copiada");
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await apiClient.deleteVMCredentialProfile(deleteTarget.id);
      toast.success("Perfil removido");
      setDeleteTarget(null);
      refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao remover perfil");
    } finally {
      setDeleting(false);
    }
  };

  // Só mostra o seletor de origem da chave ao CRIAR (não editar) com authMethod "key".
  const showKeySourceSelector = creating && !editing && form.authMethod === "key";

  return (
    <>
      <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col overflow-hidden">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="h-4 w-4" /> Perfis de Credencial SSH
            </DialogTitle>
            <DialogDescription>
              Chave privada e senha são criptografadas em repouso (AES-256-GCM) e nunca reexibidas depois de salvas.
            </DialogDescription>
          </DialogHeader>

          {!creating && (
            <>
              <div className="flex justify-end flex-shrink-0">
                <Button size="sm" onClick={startCreate}>
                  <Plus className="h-3.5 w-3.5 mr-1.5" /> Novo perfil
                </Button>
              </div>

              <ScrollArea className="flex-1 min-h-0 border rounded-md">
                {loading && (
                  <div className="p-6 flex justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                  </div>
                )}
                {!loading && profiles.length === 0 && (
                  <div className="text-sm text-muted-foreground p-6 text-center">
                    Nenhum perfil de credencial SSH cadastrado ainda.
                  </div>
                )}
                {!loading && profiles.length > 0 && (
                  <div className="divide-y">
                    {profiles.map((p) => (
                      <div key={p.id} className="flex items-center gap-3 p-3">
                        <div className="min-w-0 flex-1">
                          <div className="text-sm font-medium truncate">{p.name}</div>
                          <div className="text-xs text-muted-foreground font-mono truncate">
                            {p.username} · {p.authMethod === "key" ? "chave privada" : "senha"}
                          </div>
                        </div>
                        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => startEdit(p)} title="Editar">
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          onClick={() => setDeleteTarget(p)}
                          title="Excluir"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </ScrollArea>

              <DialogFooter>
                <Button variant="outline" onClick={onClose}>Fechar</Button>
              </DialogFooter>
            </>
          )}

          {creating && (
            <div className="flex-1 min-h-0 overflow-y-auto space-y-4 pr-1">
              <div className="flex items-center justify-between">
                <div className="text-sm font-medium">{editing ? `Editando "${editing.name}"` : "Novo perfil"}</div>
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={cancelForm}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>

              {/* Resultado da geração de chave — substitui o resto do formulário até o usuário
                  concluir (a chave pública só existe neste momento; nunca mais reexibida depois). */}
              {generatedPublicKey !== null ? (
                <div className="space-y-3">
                  <p className="text-sm text-muted-foreground">
                    Perfil <strong>{form.name}</strong> criado. Copie a chave pública abaixo e adicione em{" "}
                    <code className="font-mono text-xs bg-muted px-1 rounded">~/.ssh/authorized_keys</code> do usuário{" "}
                    <strong>{form.username}</strong> na VM de destino.
                  </p>
                  <div className="flex items-start gap-2">
                    <Textarea readOnly value={generatedPublicKey} className="font-mono text-xs h-24 flex-1" />
                    <Button variant="outline" size="icon" className="h-9 w-9 flex-shrink-0" onClick={handleCopyPublicKey}>
                      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                    </Button>
                  </div>
                  <div className="flex justify-end">
                    <Button onClick={cancelForm}>Concluir</Button>
                  </div>
                </div>
              ) : (
                <>
                  <div className="space-y-1.5">
                    <Label className="text-xs">Nome do perfil</Label>
                    <Input
                      value={form.name}
                      onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                      placeholder="ex: chave-frota-prd"
                    />
                  </div>

                  <div className="space-y-1.5">
                    <Label className="text-xs">Usuário SSH</Label>
                    <Input
                      value={form.username}
                      onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))}
                      placeholder="ec2-user / ubuntu / admin"
                    />
                  </div>

                  <div className="space-y-1.5">
                    <Label className="text-xs">Método de autenticação</Label>
                    <RadioGroup
                      value={form.authMethod}
                      onValueChange={(v) => setForm((f) => ({ ...f, authMethod: v as "key" | "password" }))}
                      className="flex gap-4"
                    >
                      <div className="flex items-center gap-2">
                        <RadioGroupItem value="key" id="auth-key" />
                        <Label htmlFor="auth-key" className="text-sm font-normal">Chave privada</Label>
                      </div>
                      <div className="flex items-center gap-2">
                        <RadioGroupItem value="password" id="auth-password" />
                        <Label htmlFor="auth-password" className="text-sm font-normal">Senha</Label>
                      </div>
                    </RadioGroup>
                  </div>

                  {form.authMethod === "key" ? (
                    <>
                      {showKeySourceSelector && (
                        <div className="space-y-1.5">
                          <Label className="text-xs">Origem da chave</Label>
                          <div className="flex gap-1">
                            {(
                              [
                                ["paste", "Colar chave"],
                                ["generate", "Gerar novo par"],
                                ["local", "Importar de ~/.ssh"],
                              ] as [KeySource, string][]
                            ).map(([value, label]) => (
                              <Button
                                key={value}
                                type="button"
                                variant={keySource === value ? "default" : "outline"}
                                size="sm"
                                className="text-xs"
                                onClick={() => setKeySource(value)}
                              >
                                {label}
                              </Button>
                            ))}
                          </div>
                        </div>
                      )}

                      {(!showKeySourceSelector || keySource === "paste") && (
                        <>
                          <div className="space-y-1.5">
                            <Label className="text-xs">
                              Chave privada (PEM) {editing && <span className="text-muted-foreground">— deixe em branco para manter a atual</span>}
                            </Label>
                            <Textarea
                              value={form.privateKeyPEM}
                              onChange={(e) => setForm((f) => ({ ...f, privateKeyPEM: e.target.value }))}
                              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
                              className="font-mono text-xs h-32"
                            />
                          </div>
                          <div className="space-y-1.5">
                            <Label className="text-xs">Passphrase (opcional, se a chave for cifrada)</Label>
                            <Input
                              type="password"
                              value={form.passphrase}
                              onChange={(e) => setForm((f) => ({ ...f, passphrase: e.target.value }))}
                            />
                          </div>
                        </>
                      )}

                      {showKeySourceSelector && keySource === "generate" && (
                        <div className="space-y-3 border rounded-md p-3">
                          <div className="space-y-1.5">
                            <Label className="text-xs">Tipo de chave</Label>
                            <RadioGroup value={keyType} onValueChange={(v) => setKeyType(v as "rsa" | "ed25519")} className="flex gap-4">
                              <div className="flex items-center gap-2">
                                <RadioGroupItem value="ed25519" id="keytype-ed25519" />
                                <Label htmlFor="keytype-ed25519" className="text-sm font-normal">ED25519 (recomendado)</Label>
                              </div>
                              <div className="flex items-center gap-2">
                                <RadioGroupItem value="rsa" id="keytype-rsa" />
                                <Label htmlFor="keytype-rsa" className="text-sm font-normal">RSA</Label>
                              </div>
                            </RadioGroup>
                          </div>
                          {keyType === "rsa" && (
                            <div className="space-y-1.5 max-w-[160px]">
                              <Label className="text-xs">Tamanho (bits)</Label>
                              <Input value={rsaBits} onChange={(e) => setRsaBits(e.target.value)} placeholder="4096" />
                            </div>
                          )}
                          <p className="text-xs text-muted-foreground">
                            O app gera o par, salva a chave privada criptografada como este perfil e mostra a chave
                            pública em seguida pra você copiar pro destino.
                          </p>
                        </div>
                      )}

                      {showKeySourceSelector && keySource === "local" && (
                        <div className="space-y-2 border rounded-md p-3">
                          <div className="flex items-center justify-between">
                            <Label className="text-xs">Chave em ~/.ssh (do servidor)</Label>
                            <Button variant="ghost" size="icon" className="h-6 w-6" onClick={fetchLocalKeys} title="Atualizar lista">
                              <RefreshCcw className={`h-3 w-3 ${loadingLocalKeys ? "animate-spin" : ""}`} />
                            </Button>
                          </div>
                          {loadingLocalKeys && (
                            <div className="text-xs text-muted-foreground flex items-center gap-1.5">
                              <Loader2 className="h-3 w-3 animate-spin" /> Carregando...
                            </div>
                          )}
                          {!loadingLocalKeys && localKeys.length === 0 && (
                            <p className="text-xs text-muted-foreground">
                              Nenhuma chave privada encontrada em ~/.ssh do servidor.
                            </p>
                          )}
                          {!loadingLocalKeys && localKeys.length > 0 && (
                            <RadioGroup value={selectedLocalKeyPath} onValueChange={setSelectedLocalKeyPath} className="space-y-1.5">
                              {localKeys.map((k) => (
                                <div key={k.path} className="flex items-center gap-2">
                                  <RadioGroupItem value={k.path} id={`localkey-${k.path}`} />
                                  <Label htmlFor={`localkey-${k.path}`} className="text-sm font-mono font-normal">
                                    {k.name}
                                  </Label>
                                </div>
                              ))}
                            </RadioGroup>
                          )}
                          <div className="space-y-1.5 pt-1">
                            <Label className="text-xs">Passphrase (opcional, se a chave for cifrada)</Label>
                            <Input
                              type="password"
                              value={form.passphrase}
                              onChange={(e) => setForm((f) => ({ ...f, passphrase: e.target.value }))}
                            />
                          </div>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="space-y-1.5">
                      <Label className="text-xs">
                        Senha {editing && <span className="text-muted-foreground">— deixe em branco para manter a atual</span>}
                      </Label>
                      <Input
                        type="password"
                        value={form.password}
                        onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                      />
                    </div>
                  )}

                  <DialogFooter>
                    <Button variant="outline" onClick={cancelForm}>Cancelar</Button>
                    <Button onClick={handleSave} disabled={saving}>
                      {saving && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
                      Salvar
                    </Button>
                  </DialogFooter>
                </>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteTarget !== null} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Excluir perfil "{deleteTarget?.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              Essa ação não pode ser desfeita. Terminais SSH que usam este perfil deixarão de conseguir conectar.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              {deleting && <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />}
              Excluir
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
