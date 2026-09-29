// Wrapper do @monaco-editor/react: todo editor da aplicação deve importar daqui.
// O beforeMount instala (uma vez) as ações globais do editor — hoje, comentários
// de linha/bloco com os atalhos do VS Code (codeComments.ts).
import BaseEditor, { DiffEditor as BaseDiffEditor } from "@monaco-editor/react";
import type { EditorProps, DiffEditorProps } from "@monaco-editor/react";
import { installCommentActions } from "@/lib/codeComments";

export * from "@monaco-editor/react";

export default function Editor(props: EditorProps) {
  return <BaseEditor {...props} beforeMount={m => { installCommentActions(m); props.beforeMount?.(m); }} />;
}

export function DiffEditor(props: DiffEditorProps) {
  return <BaseDiffEditor {...props} beforeMount={m => { installCommentActions(m); props.beforeMount?.(m); }} />;
}
