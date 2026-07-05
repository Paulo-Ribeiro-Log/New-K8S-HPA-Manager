import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Pencil, Check, X } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import type { CloudAccountHints } from "@/lib/api/types";

interface CloudAccountHintFieldProps {
  provider: "gcp" | "aws";
}

const FIELD_KEY: Record<CloudAccountHintFieldProps["provider"], keyof CloudAccountHints> = {
  gcp: "gcp_email",
  aws: "aws_email",
};

/**
 * Lembrete pessoal de qual e-mail usar ao autenticar via Device Auth Grant.
 * Puramente informativo — não força nem seleciona a conta na tela externa de login.
 */
export function CloudAccountHintField({ provider }: CloudAccountHintFieldProps) {
  const queryClient = useQueryClient();
  const key = FIELD_KEY[provider];
  const { data: hints } = useQuery({
    queryKey: ["cloud-account-hints"],
    queryFn: () => apiClient.getCloudAccountHints(),
    staleTime: 5 * 60 * 1000,
  });

  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState("");

  const saveMutation = useMutation({
    mutationFn: (newValue: string) =>
      apiClient.saveCloudAccountHints({ ...hints, [key]: newValue }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["cloud-account-hints"] });
      setEditing(false);
    },
  });

  const currentEmail = hints?.[key];

  if (editing) {
    return (
      <div className="flex items-center gap-1.5 mt-1">
        <Input
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="ex: nome.ca@via.com.br"
          className="h-7 text-xs"
          onKeyDown={(e) => {
            if (e.key === "Enter") saveMutation.mutate(value.trim());
            if (e.key === "Escape") setEditing(false);
          }}
        />
        <Button
          size="icon"
          variant="ghost"
          className="h-7 w-7 flex-shrink-0"
          disabled={saveMutation.isPending}
          onClick={() => saveMutation.mutate(value.trim())}
        >
          <Check className="h-3.5 w-3.5" />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className="h-7 w-7 flex-shrink-0"
          onClick={() => setEditing(false)}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
    );
  }

  if (currentEmail) {
    return (
      <button
        type="button"
        onClick={() => {
          setValue(currentEmail);
          setEditing(true);
        }}
        className="flex items-center gap-1 mt-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        Autenticando como: <span className="font-medium">{currentEmail}</span>
        <Pencil className="h-3 w-3" />
      </button>
    );
  }

  return (
    <button
      type="button"
      onClick={() => {
        setValue("");
        setEditing(true);
      }}
      className="mt-1 text-xs text-muted-foreground hover:text-foreground underline decoration-dotted transition-colors"
    >
      Usar outra conta para autenticar? (ex: nome.ca@via.com.br)
    </button>
  );
}
