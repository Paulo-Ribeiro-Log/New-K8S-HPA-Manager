import { useEffect, useRef, useState } from "react";
import Editor, { OnChange, BeforeMount, OnMount } from "@monaco-editor/react";
import type { Monaco } from "@monaco-editor/react";
import type * as MonacoEditorNS from "monaco-editor";
import { configureMonacoYaml, type MonacoYaml } from "monaco-yaml";
import configMapSchema from "@/lib/schemas/configmap.schema.json";

interface MonacoYamlEditorProps {
  value: string;
  onChange: (value: string) => void;
  height?: string | number;
  readOnly?: boolean;
}

const schemaUri = "inmemory://model/configmap-schema.json";

export const MonacoYamlEditor = ({ value, onChange, height = 320, readOnly = false }: MonacoYamlEditorProps) => {
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

    monacoInstance.editor.defineTheme("k8s-hpa-theme", {
      base: "vs-dark",
      inherit: true,
      rules: [],
      colors: {
        "editor.background": "#0f172a",
      },
    });
  };

  const handleMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS, () => {
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
    onChange(nextValue ?? "");
  };

  return (
    <div className="border border-border/60 rounded-lg overflow-hidden">
      <Editor
        height={height}
        defaultLanguage="yaml"
        value={value}
        onMount={handleMount}
        beforeMount={handleBeforeMount}
        onChange={handleChange}
        theme="k8s-hpa-theme"
        options={{
          automaticLayout: true,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          wordWrap: "on",
          readOnly,
          tabSize: 2,
          formatOnPaste: true,
          formatOnType: true,
        }}
      />
    </div>
  );
};
