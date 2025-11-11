import { useEffect, useRef, useState } from "react";
import Editor, { OnChange, BeforeMount, OnMount } from "@monaco-editor/react";
import { DiffEditor } from "@monaco-editor/react";
import type { Monaco } from "@monaco-editor/react";
import type * as MonacoEditorNS from "monaco-editor";
import { configureMonacoYaml, type MonacoYaml } from "monaco-yaml";
import configMapSchema from "@/lib/schemas/configmap.schema.json";

interface MonacoYamlEditorProps {
  value: string;
  onChange?: (value: string) => void;
  originalValue?: string;
  mode?: "editor" | "diff";
  height?: string | number;
  readOnly?: boolean;
}

const schemaUri = "inmemory://model/configmap-schema.json";

export const MonacoYamlEditor = ({ value, onChange, originalValue, mode = "editor", height = 320, readOnly = false }: MonacoYamlEditorProps) => {
  const [mounted, setMounted] = useState(false);
  const editorRef = useRef<MonacoEditorNS.editor.IStandaloneCodeEditor | null>(null);
  const yamlConfigRef = useRef<MonacoYaml | null>(null);

  const handleBeforeMount: BeforeMount = (monacoInstance: Monaco) => {
    yamlConfigRef.current?.dispose();
    yamlConfigRef.current = configureMonacoYaml(monacoInstance, {
      enableSchemaRequest: false,
      hover: true,
      completion: true,
      format: true,
      validate: true,
      isKubernetes: true,
      schemas: [
        {
          uri: schemaUri,
          fileMatch: ["*"],
          schema: configMapSchema as any,
        },
      ],
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
  };

  return (
    <div className="border border-border/60 rounded-lg overflow-hidden">
      {mode === "diff" ? (
        <DiffEditor
          height={height}
          original={originalValue ?? ""}
          modified={value}
          onMount={() => setMounted(true)}
          beforeMount={handleBeforeMount}
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
          beforeMount={handleBeforeMount}
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
