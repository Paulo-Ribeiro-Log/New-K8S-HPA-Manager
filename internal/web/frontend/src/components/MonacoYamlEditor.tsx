import { useEffect, useRef, useState } from "react";
import Editor, { OnChange, BeforeMount, OnMount } from "@/lib/monacoEditor";
import { DiffEditor } from "@/lib/monacoEditor";
import type { Monaco } from "@/lib/monacoEditor";
import type * as MonacoEditorNS from "monaco-editor";
import { configureMonacoYaml } from "monaco-yaml";
import { toast } from "sonner";
import { explainCronExpression, isValidCronExpression, textToCron } from "@/lib/cronParser";
import { scanSecretForWhitespaceIssues } from "@/lib/secretWhitespaceCheck";

const SECRET_KIND_REGEX = /^kind:\s*Secret\b/m;

// configureMonacoYaml é global — deve ser chamado UMA vez por sessão.
// Chamadas repetidas corrompem o worker YAML e podem remover actions do contexto.
let _yamlConfigured = false;

interface MonacoYamlEditorProps {
  value: string;
  onChange?: (value: string) => void;
  originalValue?: string;
  mode?: "editor" | "diff";
  height?: string | number;
  readOnly?: boolean;
  // Dispara "Verificar espaços em branco (base64)" automaticamente sempre que `documentKey` mudar
  // (em vez de exigir clique manual no menu de contexto) — usado pelo painel de detalhes da aba
  // Secrets, onde faz sentido avisar assim que o manifesto é carregado. `documentKey` (ex:
  // `cluster/namespace/name`) é o sinal de "documento novo carregado", distinto de `value` mudar
  // por causa de cada tecla digitada pelo usuário — sem essa distinção, o auto-check reexecutaria
  // (e notificaria) a cada edição, indo contra o comportamento já documentado de exigir novo scan
  // manual após editar.
  autoCheckSecretWhitespace?: boolean;
  documentKey?: string | null;
}

export const MonacoYamlEditor = ({ value, onChange, originalValue, mode = "editor", height = 320, readOnly = false, autoCheckSecretWhitespace = false, documentKey }: MonacoYamlEditorProps) => {
  const [mounted, setMounted] = useState(false);
  const editorRef = useRef<MonacoEditorNS.editor.IStandaloneCodeEditor | null>(null);
  const diffEditorRef = useRef<MonacoEditorNS.editor.IStandaloneDiffEditor | null>(null);
  const monacoRef = useRef<Monaco | null>(null);
  const whitespaceDecorationsRef = useRef<MonacoEditorNS.editor.IEditorDecorationsCollection | null>(null);

  const handleBeforeMount: BeforeMount = (monacoInstance: Monaco) => {
    if (_yamlConfigured) return;
    _yamlConfigured = true;
    configureMonacoYaml(monacoInstance, {
      enableSchemaRequest: false,
      hover: true,
      completion: true,
      format: true,
      validate: true,
      isKubernetes: true,
    });
  };

  // Compartilhada entre a action do menu de contexto (clique manual) e o auto-check disparado por
  // `documentKey` (carregamento de um Secret novo) — mesma lógica de scan/decoração/toast nos dois
  // casos, de propósito: sem o toast de sucesso também no caminho automático, não haveria NENHUM
  // sinal visível de que o auto-check rodou quando o secret está limpo — indistinguível de "não
  // disparou" (bug real reportado: usuário testou um secret sem espaço em branco e não viu nada).
  const runWhitespaceCheck = () => {
    const ed = editorRef.current;
    const monacoInstance = monacoRef.current;
    if (!ed || !monacoInstance) return;

    if (whitespaceDecorationsRef.current) {
      whitespaceDecorationsRef.current.clear();
      whitespaceDecorationsRef.current = null;
    }

    const results = scanSecretForWhitespaceIssues(ed.getValue());
    if (results.length === 0) {
      toast.success("Nenhum espaço em branco suspeito encontrado nos valores decodificados");
      return;
    }

    const issueLabel = (issues: ("leading" | "trailing")[]) =>
      issues.map((issue) => (issue === "leading" ? "início" : "fim")).join("/");

    const decorations: MonacoEditorNS.editor.IModelDeltaDecoration[] = results.map((r) => {
      const message = {
        value:
          `Espaço em branco suspeito no **${issueLabel(r.issues)}** do valor decodificado — comum quando ` +
          `o secret foi criado com \`echo\` sem \`-n\`.\n\nPrévia: \`${r.decodedPreview}\``,
      };
      return {
        range: new monacoInstance.Range(r.lineNumber, 1, r.lineNumber, 1),
        options: {
          isWholeLine: true,
          className: "monaco-whitespace-issue-line",
          glyphMarginClassName: "monaco-whitespace-issue-glyph",
          glyphMarginHoverMessage: message,
          hoverMessage: message,
        },
      };
    });
    whitespaceDecorationsRef.current = ed.createDecorationsCollection(decorations);

    const MAX_LISTED = 5;
    const listed = results
      .slice(0, MAX_LISTED)
      .map((r) => `${r.key} (${issueLabel(r.issues)})`)
      .join(", ");
    const suffix = results.length > MAX_LISTED ? ` e mais ${results.length - MAX_LISTED}` : "";
    toast.warning(`${results.length} chave(s) com espaço em branco suspeito: ${listed}${suffix}`, {
      description: "Veja o ícone de aviso na margem esquerda das linhas destacadas.",
    });
  };

  // Lido por ref: handleMount roda uma vez só, e capturar `onChange` direto congelaria a
  // primeira versão da função (closure antiga).
  const onChangeRef = useRef(onChange);
  useEffect(() => { onChangeRef.current = onChange; });

  const handleMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    monacoRef.current = monacoInstance;

    // Ctrl+S para salvar — addAction (escopo deste editor), não addCommand: o addCommand é
    // global no Monaco e roubava o Ctrl+S dos outros editores abertos (ex: Code Editor).
    editor.addAction({
      id: "yaml.save",
      label: "Salvar",
      keybindings: [monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS],
      run: () => { onChangeRef.current?.(editor.getValue()); },
    });

    // Troca o texto selecionado pelo resultado de `transform` (null = não mexe).
    const replaceSelection = (ed: MonacoEditorNS.editor.ICodeEditor, source: string, transform: (text: string) => string | null) => {
      const selection = ed.getSelection();
      if (!selection) return;
      const selectedText = ed.getModel()?.getValueInRange(selection);
      if (!selectedText) return;
      try {
        const result = transform(selectedText);
        if (result === null) return;
        ed.executeEdits(source, [{ range: selection, text: result, forceMoveMarkers: true }]);
        onChangeRef.current?.(ed.getValue());
      } catch (error) {
        console.error(`Erro em ${source}:`, error);
      }
    };

    // Ações de edição registradas SEMPRE, com precondition "!editorReadonly": o Monaco as esconde
    // do menu e desativa os atalhos enquanto o editor está readOnly e as reativa sozinho quando
    // ele fica editável. Antes eram registradas só se `readOnly` fosse false no mount — e como
    // vários chamadores alternam readOnly depois (ex: ResourceYamlPanel usa readOnly={loading}),
    // um editor montado durante o loading ficava sem essas ações para sempre.
    const editable = "!editorReadonly";
    const { KeyMod, KeyCode } = monacoInstance;
    editor.addAction({
      id: "encode-base64-action",
      label: "Encode para Base64",
      keybindings: [KeyMod.CtrlCmd | KeyMod.Shift | KeyCode.KeyE],
      precondition: editable,
      contextMenuGroupId: "1_modification",
      contextMenuOrder: 1,
      run: (ed) => replaceSelection(ed, "encode-base64", (t) => btoa(unescape(encodeURIComponent(t)))),
    });
    editor.addAction({
      id: "decode-base64-action",
      label: "Decode de Base64",
      keybindings: [KeyMod.CtrlCmd | KeyMod.Shift | KeyCode.KeyD],
      precondition: editable,
      contextMenuGroupId: "1_modification",
      contextMenuOrder: 2,
      run: (ed) => replaceSelection(ed, "decode-base64", (t) => decodeURIComponent(escape(atob(t)))),
    });
    editor.addAction({
      id: "cron-to-text-action",
      label: "Cron → Texto legível",
      precondition: editable,
      contextMenuGroupId: "1_modification",
      contextMenuOrder: 3,
      run: (ed) => replaceSelection(ed, "cron-to-text", (t) => {
        const cron = t.trim();
        if (!isValidCronExpression(cron)) return null;
        return explainCronExpression(cron)?.readable ?? null;
      }),
    });
    editor.addAction({
      id: "text-to-cron-action",
      label: "Texto → Expressão Cron",
      precondition: editable,
      contextMenuGroupId: "1_modification",
      contextMenuOrder: 4,
      run: (ed) => replaceSelection(ed, "text-to-cron", (t) => textToCron(t.trim()) || null),
    });

    // Verificação de espaços em branco em valores base64 — só faz sentido (e só aparece no menu
    // de contexto) quando o YAML aberto é um Secret; funciona mesmo em modo readOnly, já que só lê
    // e decora, nunca edita o conteúdo.
    const isSecretYamlKey = editor.createContextKey<boolean>("isSecretYaml", SECRET_KIND_REGEX.test(editor.getValue()));
    editor.onDidChangeModelContent(() => {
      isSecretYamlKey.set(SECRET_KIND_REGEX.test(editor.getValue()));
      if (whitespaceDecorationsRef.current) {
        whitespaceDecorationsRef.current.clear();
        whitespaceDecorationsRef.current = null;
      }
    });

    editor.addAction({
      id: "check-base64-whitespace-action",
      label: "Verificar espaços em branco (base64)",
      contextMenuGroupId: "1_modification",
      contextMenuOrder: 5,
      precondition: "isSecretYaml",
      run: () => runWhitespaceCheck(),
    });

    setMounted(true);
  };

  // Auto-check: dispara a mesma verificação (com o mesmo toast/decoração do clique manual) assim
  // que um documento novo é carregado — não a cada tecla digitada, só quando `documentKey` muda. Só
  // se aplica quando o conteúdo é de fato um Secret; para os demais recursos
  // (`autoCheckSecretWhitespace` desligado, ou YAML sem `kind: Secret`) é um no-op.
  useEffect(() => {
    if (!autoCheckSecretWhitespace || !mounted || !documentKey) return;
    const ed = editorRef.current;
    if (!ed || !SECRET_KIND_REGEX.test(ed.getValue())) return;
    runWhitespaceCheck();
  }, [documentKey, mounted, autoCheckSecretWhitespace]);

  const handleDiffMount = (editor: MonacoEditorNS.editor.IStandaloneDiffEditor) => {
    diffEditorRef.current = editor;
    setMounted(true);
  };

  // Cleanup when switching modes
  useEffect(() => {
    if (mode === 'editor' && diffEditorRef.current) {
      try {
        diffEditorRef.current.dispose();
        diffEditorRef.current = null;
      } catch (error) {
        console.warn('Error disposing diff editor on mode switch:', error);
      }
    } else if (mode === 'diff' && editorRef.current) {
      try {
        editorRef.current.dispose();
        editorRef.current = null;
      } catch (error) {
        console.warn('Error disposing editor on mode switch:', error);
      }
    }
  }, [mode]);

  useEffect(() => {
    return () => {
      if (diffEditorRef.current) {
        try { diffEditorRef.current.dispose(); } catch (_) {}
        diffEditorRef.current = null;
      }
      if (editorRef.current) {
        try { editorRef.current.dispose(); } catch (_) {}
        editorRef.current = null;
      }
    };
  }, []);

  const handleChange: OnChange = (nextValue) => {
    if (!onChange) return;
    onChange(nextValue ?? "");
  };

  const commonOptions = {
    automaticLayout: true,
    scrollBeyondLastLine: true,  // ✅ Permite scroll além da última linha (força scroll sempre)
    wordWrap: "on" as const,
    tabSize: 2,
    formatOnPaste: true,
    formatOnType: true,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', 'Courier New', monospace",
    fontLigatures: true,
    cursorBlinking: "smooth" as const,
    cursorSmoothCaretAnimation: "on" as const,
    smoothScrolling: true,
    scrollbar: {
      vertical: "visible" as const,
      horizontal: "visible" as const,
      useShadows: true,
      verticalScrollbarSize: 14,
      horizontalScrollbarSize: 14,
      alwaysConsumeMouseWheel: true,  // ✅ Força scroll wheel sempre ativo
    },
    renderWhitespace: "selection" as const,
    renderLineHighlight: "all" as const,
    lineNumbers: "on" as const,
    glyphMargin: true,
    folding: true,
    foldingHighlight: true,
    showFoldingControls: "mouseover" as const,
    matchBrackets: "always" as const,
    colorDecorators: true,
    suggest: {
      showIcons: true,
      showSnippets: true,
    },
  };

  return (
    // h-full: sem isso, um `height="100%"` passado pelo chamador (ex: dentro de um `flex-1
    // min-h-0`) nunca resolve — a % só funciona se ESTE wrapper também tiver altura própria.
    // Inofensivo para chamadores com altura fixa em pixels (ex: height={320}): o <Editor> interno
    // já define sua própria altura absoluta nesse caso, então a altura deste wrapper (resolvida
    // ou não) não afeta o resultado — ele só encolhe/cresce pra caber o conteúdo do filho.
    <div className="border border-border/60 rounded-lg overflow-hidden h-full">
      {mode === "diff" ? (
        <DiffEditor
          height={height}
          original={originalValue ?? ""}
          modified={value}
          onMount={() => setMounted(true)}
          theme="vs-dark"
          language="yaml"
          options={{
            renderSideBySide: true,
            readOnly: true,
            minimap: { enabled: false },
            ...commonOptions,
          }}
        />
      ) : (
        <Editor
          height={height}
          defaultLanguage="yaml"
          value={value}
          beforeMount={handleBeforeMount}
          onMount={handleMount}
          onChange={handleChange}
          theme="vs-dark"
          options={{
            minimap: { enabled: false },
            readOnly,
            ...commonOptions,
          }}
        />
      )}
    </div>
  );
};
