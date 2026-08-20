import { useEffect, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FileArchive, Loader2, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useCertificates } from "@/hooks/useCertificates";
import type { PFXExtractInfo } from "@/types/certificates";

interface PFXExtractModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // onExtracted é chamado com o resultado assim que a extração termina — quem abriu o modal
  // decide o que fazer (ex: nada, só avisar que ficou salvo; o uso real acontece depois, via
  // CertificateSourcePickerModal, aba "Extraído de PFX").
  onExtracted?: (info: PFXExtractInfo) => void;
}

// PFXExtractModal — extrai tls.crt (leaf + chain já concatenados)/tls.key de um arquivo .pfx
// (PKCS#12) direto na aplicação, evitando o passo manual via openssl. Ver
// PFX-CERT-EXTRACTION-PLAN.md. O arquivo + senha são enviados ao backend numa única requisição
// multipart; a senha nunca é persistida (usada uma vez pra decodificar, descartada em seguida) —
// o resultado (tls.crt/tls.key) fica salvo sob o nome escolhido aqui, navegável depois pelo
// CertificateSourcePickerModal (3ª aba, "Extraído de PFX") nos modais de instalação já existentes.
export function PFXExtractModal({ open, onOpenChange, onExtracted }: PFXExtractModalProps) {
  const { extractPFX } = useCertificates();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [comment, setComment] = useState("");
  const [extracting, setExtracting] = useState(false);
  const [result, setResult] = useState<PFXExtractInfo | null>(null);

  useEffect(() => {
    if (open) {
      setFile(null);
      setName("");
      setPassword("");
      setComment("");
      setResult(null);
    }
  }, [open]);

  const handleFileChange = (f: File | null) => {
    setFile(f);
    // Sugestão automática de nome = nome do arquivo sem extensão, editável — usuário pode trocar
    // livremente antes de extrair. Só sugere se o campo ainda estiver vazio (não sobrescreve uma
    // escolha já feita ao trocar o arquivo).
    if (f && !name.trim()) {
      setName(f.name.replace(/\.(pfx|p12)$/i, ""));
    }
  };

  const handleExtract = async () => {
    if (!file || !name.trim() || !password) return;
    setExtracting(true);
    try {
      const { info } = await extractPFX(file, password, name.trim(), comment.trim() || undefined);
      setResult(info);
      toast.success(`Certificado "${info.name}" extraído e salvo`);
      onExtracted?.(info);
    } catch (e: unknown) {
      toast.error("Erro ao extrair .pfx", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    } finally {
      setExtracting(false);
    }
  };

  const formatDate = (iso: string) => {
    try {
      return new Date(iso).toLocaleDateString("pt-BR");
    } catch {
      return iso;
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!extracting) onOpenChange(v); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FileArchive className="h-4 w-4" />
            Extrair Certificado de .pfx
          </DialogTitle>
          <DialogDescription>
            Separa o certificado (com a chain) e a chave privada de um arquivo .pfx/.p12 — evita
            precisar rodar <code>openssl</code> manualmente. A senha do arquivo é usada só uma vez
            pra decodificar e nunca é salva.
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-3 py-2">
            <div className="rounded border border-green-500/40 bg-green-500/10 p-3 space-y-1">
              <div className="flex items-center gap-1.5 text-green-400 text-sm font-medium">
                <ShieldCheck className="h-4 w-4" />
                Extraído e salvo como "{result.name}"
              </div>
              <p className="text-xs text-muted-foreground">Subject: {result.subject}</p>
              <p className="text-xs text-muted-foreground">Issuer: {result.issuer}</p>
              <p className="text-xs text-muted-foreground">Expira: {formatDate(result.not_after)}</p>
              <p className="text-xs text-muted-foreground">
                {result.chain_length} certificado(s) na chain (leaf + intermediárias/raiz, se houver)
              </p>
            </div>
            <p className="text-xs text-muted-foreground">
              Pra instalar, abra o formulário de instalação manual (Secrets → "Atualizar
              Certificado", ou Certificados TLS → Upload) e escolha "Extraído de PFX" — o nome{" "}
              <strong>{result.name}</strong> vai aparecer na lista.
            </p>
          </div>
        ) : (
          <div className="space-y-3 py-2">
            <div>
              <Label htmlFor="pfx-file" className="text-xs">Arquivo .pfx / .p12</Label>
              <input
                ref={fileInputRef}
                id="pfx-file"
                type="file"
                accept=".pfx,.p12"
                onChange={(e) => handleFileChange(e.target.files?.[0] ?? null)}
                className="mt-1 flex h-9 w-full rounded-md border border-input bg-background px-3 py-1.5 text-xs file:mr-2 file:rounded file:border-0 file:bg-muted file:px-2 file:py-1 file:text-xs"
              />
            </div>
            <div>
              <Label htmlFor="pfx-name" className="text-xs">Nome do certificado</Label>
              <Input
                id="pfx-name"
                className="mt-1 h-9 text-xs"
                placeholder="ex: via-tls-prod-2026"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                Nome livre pra identificar essa extração depois — não precisa bater com o nome do Secret.
              </p>
            </div>
            <div>
              <Label htmlFor="pfx-password" className="text-xs">Senha do .pfx</Label>
              <Input
                id="pfx-password"
                type="password"
                className="mt-1 h-9 text-xs"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && file && name.trim() && password) handleExtract(); }}
              />
            </div>
            <div>
              <Label htmlFor="pfx-comment" className="text-xs">Comentário (opcional)</Label>
              <Input
                id="pfx-comment"
                className="mt-1 h-9 text-xs"
                placeholder="ex: recebido do fornecedor X em..."
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
            </div>
          </div>
        )}

        <DialogFooter>
          {result ? (
            <Button size="sm" onClick={() => onOpenChange(false)}>Fechar</Button>
          ) : (
            <>
              <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={extracting}>
                Cancelar
              </Button>
              <Button
                size="sm"
                onClick={handleExtract}
                disabled={!file || !name.trim() || !password || extracting}
              >
                {extracting ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <FileArchive className="h-4 w-4 mr-2" />}
                {extracting ? "Extraindo..." : "Extrair"}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
