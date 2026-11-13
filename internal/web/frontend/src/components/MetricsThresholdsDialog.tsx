import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { DEFAULT_THRESHOLDS, isDefaultThresholds, validateThresholds, type Thresholds } from "@/lib/metricsThresholds";

type NumKey = "latWarnMs" | "latRedMs" | "err5Rate" | "err4Rate" | "usageWarn" | "usageRed" | "throttleWarn" | "throttleRed" | "netWarn" | "restartsWarn";

interface Field {
  key: NumKey;
  label: string;
  unit: string;
}
interface Group {
  title: string;
  hint: string;
  fields: Field[];
}

const GROUPS: Group[] = [
  { title: "Latência", hint: "Vale para o maior percentil marcado (o que define a situação). Acima do alerta fica âmbar; acima do erro, vermelho.", fields: [
    { key: "latWarnMs", label: "Alerta acima de", unit: "ms" }, { key: "latRedMs", label: "Erro acima de", unit: "ms" } ] },
  { title: "Erros 5xx e 4xx", hint: "Taxa por segundo no momento. 0 significa que qualquer erro já conta; suba se a aplicação convive com um ruído de fundo.", fields: [
    { key: "err5Rate", label: "5xx: erro acima de", unit: "por s" }, { key: "err4Rate", label: "4xx: alerta acima de", unit: "por s" } ] },
  { title: "CPU e memória", hint: "Percentual do limit do container.", fields: [
    { key: "usageWarn", label: "Alerta acima de", unit: "%" }, { key: "usageRed", label: "Erro acima de", unit: "%" } ] },
  { title: "Throttling de CPU", hint: "Percentual dos períodos de CPU em que o container foi limitado.", fields: [
    { key: "throttleWarn", label: "Alerta acima de", unit: "%" }, { key: "throttleRed", label: "Erro acima de", unit: "%" } ] },
  { title: "Rede e restarts", hint: "Descartes e erros de pacote por segundo; restarts nos últimos 15 min (OOMKilled continua sendo erro).", fields: [
    { key: "netWarn", label: "Rede: alerta acima de", unit: "por s" }, { key: "restartsWarn", label: "Restarts: alerta acima de", unit: "restarts" } ] },
];

const toForm = (t: Thresholds): Record<NumKey, string> =>
  Object.fromEntries(GROUPS.flatMap((g) => g.fields).map((f) => [f.key, String(t[f.key])])) as Record<NumKey, string>;

// "1,5" e "1.5" valem; vazio ou texto vira NaN e a validação acusa
const parseNum = (s: string): number => (s.trim() === "" ? NaN : Number(s.trim().replace(",", ".")));

/**
 * Limites de alerta do Tempo real. Cada usuário tem os seus; "Restaurar padrão" volta aos valores de fábrica
 * (os que eram fixos no código). Valida antes de salvar: alerta não pode passar do erro, nada negativo.
 */
export function MetricsThresholdsDialog({
  open, onOpenChange, value, onSave, onReset,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  value: Thresholds;
  onSave: (t: Thresholds) => void;
  onReset: () => void;
}) {
  const [form, setForm] = useState<Record<NumKey, string>>(() => toForm(value));
  const [hpaCeiling, setHpaCeiling] = useState(value.hpaCeiling);
  const [pods, setPods] = useState(value.pods);
  const [errors, setErrors] = useState<string[]>([]);

  // ao abrir, o formulário parte do que está valendo
  useEffect(() => {
    if (open) {
      setForm(toForm(value));
      setHpaCeiling(value.hpaCeiling);
      setPods(value.pods);
      setErrors([]);
    }
  }, [open, value]);

  const build = (): Thresholds => {
    const t = { ...DEFAULT_THRESHOLDS, hpaCeiling, pods } as Thresholds;
    for (const g of GROUPS) for (const f of g.fields) t[f.key] = parseNum(form[f.key]);
    return t;
  };

  const save = () => {
    const t = build();
    const errs = validateThresholds(t);
    setErrors(errs);
    if (errs.length === 0) {
      onSave(t);
      onOpenChange(false);
    }
  };

  const unchanged = isDefaultThresholds(build());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Limites de alerta do Tempo real</DialogTitle>
          <DialogDescription>Definem quando uma aplicação fica em ALERTA ou ERRO na faixa de resumo, na tabela e nos cartões. São só seus e ficam salvos no servidor.</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 text-xs">
          {GROUPS.map((g) => (
            <div key={g.title} className="space-y-1.5">
              <div className="text-sm font-medium">{g.title}</div>
              <div className="text-[11px] text-muted-foreground">{g.hint}</div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {g.fields.map((f) => (
                  <label key={f.key} className="flex items-center gap-2">
                    <span className="w-44 flex-shrink-0 text-muted-foreground">{f.label}</span>
                    <Input
                      className="h-7 w-24 text-right text-xs"
                      inputMode="decimal"
                      value={form[f.key]}
                      onChange={(e) => setForm((p) => ({ ...p, [f.key]: e.target.value }))}
                      aria-label={`${g.title}: ${f.label}`}
                    />
                    <span className="text-muted-foreground">{f.unit}</span>
                    {String(DEFAULT_THRESHOLDS[f.key]) !== form[f.key].trim() && <span className="text-[10px] text-muted-foreground">(padrão {DEFAULT_THRESHOLDS[f.key].toLocaleString("pt-BR")})</span>}
                  </label>
                ))}
              </div>
            </div>
          ))}

          <div className="space-y-1.5">
            <div className="text-sm font-medium">Avisos que podem ser desligados</div>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox checked={hpaCeiling} onCheckedChange={(v) => setHpaCeiling(v === true)} />
              ALERTA quando as réplicas desejadas chegam ao teto do HPA
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <Checkbox checked={pods} onCheckedChange={(v) => setPods(v === true)} />
              ALERTA com pods pendentes ou não prontos
            </label>
          </div>

          {errors.length > 0 && (
            <ul className="space-y-0.5 rounded-md border border-red-500/40 bg-red-500/10 p-2 text-red-400" role="alert">
              {errors.map((e) => <li key={e}>{e}</li>)}
            </ul>
          )}

          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
            <Button size="sm" variant="ghost" className="text-xs" disabled={unchanged} onClick={() => { onReset(); onOpenChange(false); }} title="Volta aos valores de fábrica">Restaurar padrão</Button>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="outline" className="text-xs" onClick={() => onOpenChange(false)}>Cancelar</Button>
              <Button size="sm" className="text-xs" onClick={save}>Salvar</Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
