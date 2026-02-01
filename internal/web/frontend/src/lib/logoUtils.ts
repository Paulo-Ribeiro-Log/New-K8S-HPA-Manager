import jsPDF from "jspdf";
import logoUrl from "@/assets/sre-logistics-logo.png";

// ============================================================================
// PRE-CARREGAMENTO DA LOGO
// ============================================================================

// Cria o elemento Image e inicia carregamento imediatamente
const logoImg = new Image();
logoImg.src = logoUrl;

// Promise que resolve quando a imagem termina de carregar
const logoLoaded = new Promise<HTMLImageElement>((resolve, reject) => {
  if (logoImg.complete && logoImg.naturalWidth > 0) {
    resolve(logoImg);
  } else {
    logoImg.onload = () => resolve(logoImg);
    logoImg.onerror = () => reject(new Error("Falha ao carregar logo"));
  }
});

// ============================================================================
// FUNCOES PARA PDF
// ============================================================================

/**
 * Adiciona cabecalho com logo ao PDF
 * @param doc - Documento jsPDF
 * @param title - Titulo principal
 * @param subtitle - Subtitulo (opcional)
 * @param headerHeight - Altura do cabecalho (default: 35)
 * @returns Y position apos o header
 */
export const addLogoHeaderToPDF = async (
  doc: jsPDF,
  title: string,
  subtitle?: string,
  headerHeight: number = 35
): Promise<number> => {
  const pageWidth = doc.internal.pageSize.getWidth();

  // Header box azul
  doc.setFillColor(41, 128, 185);
  doc.rect(0, 0, pageWidth, headerHeight, "F");

  // Adicionar logo
  try {
    const img = await logoLoaded;
    // Logo no canto esquerdo (25x25 pixels, posicao 10,5)
    // Nota: O arquivo .png é na verdade JPEG internamente
    doc.addImage(img, "JPEG", 10, 5, 25, 25);
  } catch (error) {
    console.warn("Nao foi possivel adicionar logo ao PDF:", error);
  }

  // Titulo centralizado (considerando espaco da logo)
  doc.setFontSize(18);
  doc.setFont("helvetica", "bold");
  doc.setTextColor(255, 255, 255);

  const titleX = (pageWidth + 35) / 2; // Desloca para direita por causa da logo
  const titleY = subtitle ? headerHeight / 2 - 3 : headerHeight / 2 + 2;
  doc.text(title, titleX, titleY, { align: "center" });

  // Subtitulo
  if (subtitle) {
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    doc.text(subtitle, titleX, titleY + 8, { align: "center" });
  }

  // Reset cor do texto
  doc.setTextColor(0, 0, 0);

  return headerHeight + 10;
};

/**
 * Adiciona apenas a logo ao PDF (sem header)
 * @param doc - Documento jsPDF
 * @param x - Posicao X
 * @param y - Posicao Y
 * @param width - Largura (default: 25)
 * @param height - Altura (default: 25)
 */
export const addLogoToPDF = async (
  doc: jsPDF,
  x: number,
  y: number,
  width: number = 25,
  height: number = 25
): Promise<void> => {
  try {
    const img = await logoLoaded;
    // Nota: O arquivo .png é na verdade JPEG internamente
    doc.addImage(img, "JPEG", x, y, width, height);
  } catch (error) {
    console.warn("Nao foi possivel adicionar logo ao PDF:", error);
  }
};

// ============================================================================
// FUNCOES PARA MARKDOWN
// ============================================================================

/**
 * Gera cabecalho Markdown estilizado
 * @param title - Titulo principal
 * @param subtitle - Subtitulo (opcional)
 * @returns String com cabecalho Markdown
 */
export const getMarkdownHeader = (title: string, subtitle?: string): string => {
  let header = "";

  header += `<div align="center">\n\n`;
  header += `# ${title}\n\n`;
  if (subtitle) {
    header += `**${subtitle}**\n\n`;
  }
  header += `*SRE Logistica - K8s HPA Manager*\n\n`;
  header += `</div>\n\n`;
  header += `---\n\n`;

  return header;
};

// ============================================================================
// CONSTANTES
// ============================================================================

export const APP_NAME = "SRE Logistica - K8s HPA Manager";
export const APP_SHORT_NAME = "K8s HPA Manager";

/**
 * Adiciona footer padrao ao PDF
 */
export const addFooterToPDF = (
  doc: jsPDF,
  pageNumber: number,
  totalPages: number,
  additionalInfo?: string
): void => {
  const pageWidth = doc.internal.pageSize.getWidth();
  const pageHeight = doc.internal.pageSize.getHeight();

  doc.setFontSize(8);
  doc.setFont("helvetica", "italic");
  doc.setTextColor(128, 128, 128);

  // Informacao adicional (esquerda)
  if (additionalInfo) {
    doc.text(additionalInfo, 14, pageHeight - 10);
  }

  // Nome da aplicacao (centro)
  doc.text(APP_NAME, pageWidth / 2, pageHeight - 10, { align: "center" });

  // Numero da pagina (direita)
  doc.text(
    `Pagina ${pageNumber} de ${totalPages}`,
    pageWidth - 14,
    pageHeight - 10,
    { align: "right" }
  );

  // Reset cor
  doc.setTextColor(0, 0, 0);
};
