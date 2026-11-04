import type jsPDF from "jspdf";

// O jspdf-autotable grava `lastAutoTable` no documento depois de desenhar uma tabela, mas o tipo
// do jsPDF não declara esse campo.
type DocWithAutoTable = jsPDF & { lastAutoTable: { finalY: number } };

/** Posição Y logo abaixo da última tabela desenhada com autoTable. */
export function lastTableY(doc: jsPDF): number {
  return (doc as DocWithAutoTable).lastAutoTable.finalY;
}
