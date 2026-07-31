interface IssueListProps {
  errors?: string[];
  warnings?: string[];
  className?: string;
}

/**
 * IssueList — lista de erros (vermelho) + warnings (âmbar), extraída de
 * CertificateChainValidationPanel.tsx pra ser reutilizada também na renderização de HostIssue[]
 * (badges de Ingress/Gateway em CertificateDetailModal.tsx) sem duplicar o mesmo `<ul>` duas vezes.
 */
export function IssueList({ errors, warnings, className }: IssueListProps) {
  if ((!errors || errors.length === 0) && (!warnings || warnings.length === 0)) {
    return null;
  }

  return (
    <div className={className}>
      {errors && errors.length > 0 && (
        <ul className="space-y-0.5 pt-1">
          {errors.map((e, i) => (
            <li key={i} className="text-xs text-red-500">• {e}</li>
          ))}
        </ul>
      )}
      {warnings && warnings.length > 0 && (
        <ul className="space-y-0.5">
          {warnings.map((w, i) => (
            <li key={i} className="text-xs text-amber-500">• {w}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
