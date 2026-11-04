import type { ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { slugify } from "@/help/helpSearch";

// Renderiza uma página de ajuda. O plugin typography do Tailwind não está ativo no projeto, então
// cada elemento é estilizado aqui (mesmo caminho do Markdown do Code Editor). Os títulos recebem
// um id estável (slugify) para o "Nesta página" e para openHelp(id, "secao").

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (node && typeof node === "object" && "props" in node) return textOf((node as { props: { children?: ReactNode } }).props.children);
  return "";
}

const components: Components = {
  h1: ({ children }) => <h1 className="mt-4 mb-2 text-base font-semibold text-foreground">{children}</h1>,
  h2: ({ children }) => (
    <h2 id={slugify(textOf(children))} className="mt-5 mb-2 scroll-mt-2 border-b border-border/50 pb-1 text-sm font-semibold text-foreground">
      {children}
    </h2>
  ),
  h3: ({ children }) => (
    <h3 id={slugify(textOf(children))} className="mt-4 mb-1.5 scroll-mt-2 text-[13px] font-semibold text-foreground">
      {children}
    </h3>
  ),
  p: ({ children }) => <p className="my-2 text-[13px] leading-relaxed text-foreground/90">{children}</p>,
  ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5 text-[13px] text-foreground/90">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5 text-[13px] text-foreground/90">{children}</ol>,
  li: ({ children }) => <li className="leading-relaxed marker:text-muted-foreground">{children}</li>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2 hover:opacity-80">
      {children}
    </a>
  ),
  code: ({ className, children }) =>
    className?.startsWith("language-") ? (
      <code className={className}>{children}</code>
    ) : (
      <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11.5px] text-foreground">{children}</code>
    ),
  pre: ({ children }) => (
    <pre className="my-2 overflow-x-auto rounded-md border border-border/50 bg-muted/50 p-2.5 font-mono text-[11.5px] leading-relaxed">
      {children}
    </pre>
  ),
  blockquote: ({ children }) => (
    <blockquote className="my-3 rounded-r-md border-l-4 border-amber-500/70 bg-amber-500/10 px-3 py-2 text-[13px] [&>p]:my-0">
      {children}
    </blockquote>
  ),
  hr: () => <hr className="my-4 border-border/50" />,
  table: ({ children }) => (
    <div className="my-3 overflow-x-auto rounded-md border border-border/50">
      <table className="w-full border-collapse text-left text-xs">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-muted/60">{children}</thead>,
  th: ({ children }) => <th className="border-b border-border/50 px-2.5 py-1.5 font-semibold text-foreground">{children}</th>,
  td: ({ children }) => <td className="border-b border-border/30 px-2.5 py-1.5 align-top text-foreground/90">{children}</td>,
};

export function HelpMarkdown({ source }: { source: string }) {
  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
      {source}
    </ReactMarkdown>
  );
}
