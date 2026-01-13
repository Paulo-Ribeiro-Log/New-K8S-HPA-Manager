import { useEffect, useRef, useState } from "react";
import Editor, { OnChange, BeforeMount, OnMount } from "@monaco-editor/react";
import { DiffEditor } from "@monaco-editor/react";
import type { Monaco } from "@monaco-editor/react";
import type * as MonacoEditorNS from "monaco-editor";
import { configureMonacoYaml, type MonacoYaml } from "monaco-yaml";

interface MonacoYamlEditorProps {
  value: string;
  onChange?: (value: string) => void;
  originalValue?: string;
  mode?: "editor" | "diff";
  height?: string | number;
  readOnly?: boolean;
}

export const MonacoYamlEditor = ({ value, onChange, originalValue, mode = "editor", height = 320, readOnly = false }: MonacoYamlEditorProps) => {
  const [mounted, setMounted] = useState(false);
  const editorRef = useRef<MonacoEditorNS.editor.IStandaloneCodeEditor | null>(null);
  const diffEditorRef = useRef<MonacoEditorNS.editor.IStandaloneDiffEditor | null>(null);
  const yamlConfigRef = useRef<MonacoYaml | null>(null);

  const handleBeforeMount: BeforeMount = (monacoInstance: Monaco) => {
    // Dispose previous config if exists
    yamlConfigRef.current?.dispose();
    
    // Configure monaco-yaml with ALL features enabled
    yamlConfigRef.current = configureMonacoYaml(monacoInstance, {
      enableSchemaRequest: false,
      hover: true,           // ✅ Hover com documentação
      completion: true,      // ✅ Autocompletar
      format: true,          // ✅ Formatação
      validate: true,        // ✅ Validação em tempo real
      isKubernetes: true,    // ✅ Features Kubernetes
    });
  };

  const handleMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS, () => {
      if (!onChange) return;
      const currentValue = editor.getValue();
      onChange(currentValue);
    });
    setMounted(true);
  };

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
      // Cleanup on unmount
      if (diffEditorRef.current) {
        try {
          diffEditorRef.current.dispose();
        } catch (error) {
          console.warn('Error disposing diff editor:', error);
        }
        diffEditorRef.current = null;
      }
      if (editorRef.current) {
        try {
          editorRef.current.dispose();
        } catch (error) {
          console.warn('Error disposing editor:', error);
        }
        editorRef.current = null;
      }
      yamlConfigRef.current?.dispose();
    };
  }, []);

  useEffect(() => {
    return () => {
      yamlConfigRef.current?.dispose();
    };
  }, []);

  const handleChange: OnChange = (nextValue) => {
    if (!onChange) return;
    onChange(nextValue ?? "");
  };

  const commonOptions = {
    automaticLayout: true,
    scrollBeyondLastLine: false,
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
    <div className="border border-border/60 rounded-lg overflow-hidden">
      {mode === "diff" ? (
        <DiffEditor
          height={height}
          original={originalValue ?? ""}
          modified={value}
          onMount={() => setMounted(true)}
          theme="vs-dark"
          options={{
            renderSideBySide: false,
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
