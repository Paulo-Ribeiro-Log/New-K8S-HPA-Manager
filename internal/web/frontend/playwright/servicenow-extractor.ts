/**
 * ServiceNow CHG Extractor via Playwright
 *
 * Usa automação de browser para extrair dados da CHG do ServiceNow
 * aproveitando a autenticação Azure AD SSO do usuário.
 *
 * Uso:
 *   npx ts-node playwright/servicenow-extractor.ts "https://viavarejo.service-now.com/change_request.do?sys_id=..."
 *
 * Ou via API:
 *   POST /api/v1/servicenow/extract
 *   Body: { "url": "https://viavarejo.service-now.com/change_request.do?sys_id=..." }
 */

import { chromium, Browser, Page, BrowserContext } from 'playwright';
import * as path from 'path';
import * as fs from 'fs';

// Diretório para armazenar sessão do browser (reutilizar login)
const USER_DATA_DIR = path.join(process.env.HOME || '', '.k8s-hpa-manager', 'playwright-session');

// Padrões de extração do template da esteira de CD
const EXTRACTION_PATTERNS = {
  // * Aplicação(ões): tms-sync-1p-order-management-acl.
  application: /\* Aplicação\(ões\):\s*(.+?)\./,

  // * Versão: 0.0.6-2.
  version: /\* Versão:\s*(.+?)\./,

  // * Repositório: github.com/viavarejo-internal/tms-sync-1p-order-management-acl.git.
  repository: /\* Repositório:\s*github\.com\/viavarejo-internal\/(.+?)\.git/,

  // * Squad(s): Planejamento.
  squad: /\* Squad\(s\):\s*(.+?)\./,

  // * Branch no GitHub: release/0.0.6.
  branch: /\* Branch no GitHub:\s*(.+?)\./,

  // * Produto: tms-sync-1p-order-management-acl.
  product: /\* Produto:\s*(.+?)\./,

  // * Link da release no XL-Release: http://release.viavarejo.com.br/#/releases/...
  xlReleaseUrl: /\* Link da release no XL-Release:\s*(.+)/,

  // * Titulo da release no XL-Release: [Planejamento] tms-sync-1p-order-management-acl - 0.0.6-2.
  xlReleaseTitle: /\* Titulo da release no XL-Release:\s*(.+?)\./,

  // Issues do Jira (múltiplas)
  jiraIssues: /([A-Z]+-\d+)/g,
};

export interface ExtractedCHGData {
  success: boolean;
  changeNumber?: string;
  shortDescription?: string;
  description?: string;
  state?: string;
  extracted: {
    application?: string;
    version?: string;
    repository?: string;
    squad?: string;
    branch?: string;
    product?: string;
    xlReleaseUrl?: string;
    xlReleaseTitle?: string;
    jiraIssues?: string[];
  };
  confidence: {
    application: number;
    version: number;
    repository: number;
  };
  error?: string;
}

/**
 * Extrai sys_id da URL do ServiceNow
 */
function extractSysId(url: string): string | null {
  const match = url.match(/sys_id=([a-f0-9]{32})/i);
  return match ? match[1] : null;
}

/**
 * Faz parsing do campo "Motivo da mudança" para extrair dados estruturados
 */
function parseDescription(description: string): ExtractedCHGData['extracted'] {
  const extracted: ExtractedCHGData['extracted'] = {};

  // Aplicação
  const appMatch = description.match(EXTRACTION_PATTERNS.application);
  if (appMatch) {
    extracted.application = appMatch[1].trim();
  }

  // Versão
  const versionMatch = description.match(EXTRACTION_PATTERNS.version);
  if (versionMatch) {
    extracted.version = versionMatch[1].trim();
  }

  // Repositório
  const repoMatch = description.match(EXTRACTION_PATTERNS.repository);
  if (repoMatch) {
    extracted.repository = repoMatch[1].trim();
  }

  // Squad
  const squadMatch = description.match(EXTRACTION_PATTERNS.squad);
  if (squadMatch) {
    extracted.squad = squadMatch[1].trim();
  }

  // Branch
  const branchMatch = description.match(EXTRACTION_PATTERNS.branch);
  if (branchMatch) {
    extracted.branch = branchMatch[1].trim();
  }

  // Produto
  const productMatch = description.match(EXTRACTION_PATTERNS.product);
  if (productMatch) {
    extracted.product = productMatch[1].trim();
  }

  // XL Release URL
  const xlUrlMatch = description.match(EXTRACTION_PATTERNS.xlReleaseUrl);
  if (xlUrlMatch) {
    extracted.xlReleaseUrl = xlUrlMatch[1].trim();
  }

  // XL Release Title
  const xlTitleMatch = description.match(EXTRACTION_PATTERNS.xlReleaseTitle);
  if (xlTitleMatch) {
    extracted.xlReleaseTitle = xlTitleMatch[1].trim();
  }

  // Jira Issues
  const jiraMatches = description.match(EXTRACTION_PATTERNS.jiraIssues);
  if (jiraMatches) {
    extracted.jiraIssues = [...new Set(jiraMatches)]; // Remove duplicatas
  }

  return extracted;
}

/**
 * Calcula confiança da extração
 */
function calculateConfidence(extracted: ExtractedCHGData['extracted']): ExtractedCHGData['confidence'] {
  return {
    application: extracted.application ? 0.99 : 0,
    version: extracted.version ? 0.99 : 0,
    repository: extracted.repository ? 0.99 : 0,
  };
}

/**
 * Extrai dados da CHG do ServiceNow usando Playwright
 */
export async function extractCHGData(chgUrl: string, options: {
  headless?: boolean;
  timeout?: number;
  reuseSession?: boolean;
} = {}): Promise<ExtractedCHGData> {
  const {
    headless = false, // Não-headless por padrão para permitir login Azure AD
    timeout = 60000,
    reuseSession = true,
  } = options;

  // Validar URL
  const sysId = extractSysId(chgUrl);
  if (!sysId) {
    return {
      success: false,
      extracted: {},
      confidence: { application: 0, version: 0, repository: 0 },
      error: 'URL inválida: sys_id não encontrado',
    };
  }

  let browser: Browser | null = null;
  let context: BrowserContext | null = null;

  try {
    // Criar diretório de sessão se não existir
    if (reuseSession && !fs.existsSync(USER_DATA_DIR)) {
      fs.mkdirSync(USER_DATA_DIR, { recursive: true });
    }

    // Lançar browser
    if (reuseSession) {
      // Usar contexto persistente para reutilizar sessão
      context = await chromium.launchPersistentContext(USER_DATA_DIR, {
        headless,
        args: ['--disable-blink-features=AutomationControlled'],
        viewport: { width: 1280, height: 800 },
      });
      browser = null; // Contexto persistente não tem browser separado
    } else {
      browser = await chromium.launch({
        headless,
        args: ['--disable-blink-features=AutomationControlled'],
      });
      context = await browser.newContext({
        viewport: { width: 1280, height: 800 },
      });
    }

    const page = context.pages()[0] || await context.newPage();

    // Navegar para a CHG
    console.log(`[ServiceNow] Navegando para: ${chgUrl}`);
    await page.goto(chgUrl, { waitUntil: 'networkidle', timeout });

    // Verificar se precisa de login (Azure AD)
    const currentUrl = page.url();
    if (currentUrl.includes('login.microsoftonline.com') || currentUrl.includes('login.windows.net')) {
      console.log('[ServiceNow] Detectado redirect para Azure AD. Aguardando login do usuário...');

      // Aguardar usuário fazer login manualmente
      // Espera até voltar para o ServiceNow
      await page.waitForURL(/service-now\.com/, { timeout: 120000 });
      console.log('[ServiceNow] Login concluído!');

      // Aguardar carregamento completo da página
      await page.waitForLoadState('networkidle');
    }

    // Aguardar formulário carregar
    await page.waitForSelector('form', { timeout: 30000 });

    // Extrair dados da CHG
    const chgData = await page.evaluate(() => {
      // Função auxiliar para extrair valor de campo
      const getFieldValue = (fieldName: string): string => {
        // Tentar diferentes seletores
        const selectors = [
          `input[name="${fieldName}"]`,
          `textarea[name="${fieldName}"]`,
          `#${fieldName}`,
          `[id$="${fieldName}"]`,
          `[data-field="${fieldName}"]`,
        ];

        for (const selector of selectors) {
          const el = document.querySelector(selector) as HTMLInputElement | HTMLTextAreaElement;
          if (el && el.value) {
            return el.value;
          }
        }

        // Tentar buscar em iframes (ServiceNow usa iframes)
        const iframes = document.querySelectorAll('iframe');
        for (const iframe of iframes) {
          try {
            const iframeDoc = (iframe as HTMLIFrameElement).contentDocument;
            if (iframeDoc) {
              for (const selector of selectors) {
                const el = iframeDoc.querySelector(selector) as HTMLInputElement | HTMLTextAreaElement;
                if (el && el.value) {
                  return el.value;
                }
              }
            }
          } catch (e) {
            // Ignorar erros de cross-origin
          }
        }

        return '';
      };

      // Tentar extrair o número da CHG
      let changeNumber = getFieldValue('number') ||
                        getFieldValue('sys_readonly.change_request.number') ||
                        '';

      // Se não encontrou, tentar pelo título da página
      if (!changeNumber) {
        const title = document.title;
        const match = title.match(/(CHG\d+)/i);
        if (match) {
          changeNumber = match[1];
        }
      }

      // Extrair short_description
      const shortDescription = getFieldValue('short_description') ||
                              getFieldValue('sys_readonly.change_request.short_description') ||
                              '';

      // Extrair description (Motivo da mudança)
      const description = getFieldValue('description') ||
                         getFieldValue('sys_readonly.change_request.description') ||
                         getFieldValue('u_motivo_mudanca') ||
                         '';

      // Extrair state
      const stateEl = document.querySelector('[name="state"]') as HTMLSelectElement;
      const state = stateEl ? stateEl.options[stateEl.selectedIndex]?.text || '' : '';

      return {
        changeNumber,
        shortDescription,
        description,
        state,
      };
    });

    // Se não conseguiu extrair description, tentar método alternativo
    if (!chgData.description) {
      console.log('[ServiceNow] Tentando método alternativo para extrair description...');

      // Tentar clicar em tab ou expandir campo se necessário
      const descriptionTab = await page.$('text=Descrição');
      if (descriptionTab) {
        await descriptionTab.click();
        await page.waitForTimeout(1000);
      }

      // Tentar extrair novamente
      const altDescription = await page.evaluate(() => {
        // Buscar qualquer textarea grande
        const textareas = document.querySelectorAll('textarea');
        for (const ta of textareas) {
          if (ta.value && ta.value.length > 200) {
            return ta.value;
          }
        }

        // Buscar em elementos de texto
        const divs = document.querySelectorAll('div[data-field], span[data-field]');
        for (const div of divs) {
          const text = div.textContent || '';
          if (text.includes('Aplicação') && text.includes('Versão')) {
            return text;
          }
        }

        return '';
      });

      if (altDescription) {
        chgData.description = altDescription;
      }
    }

    // Fazer parsing do description
    const extracted = parseDescription(chgData.description);
    const confidence = calculateConfidence(extracted);

    console.log('[ServiceNow] Dados extraídos com sucesso!');
    console.log(`  - CHG: ${chgData.changeNumber}`);
    console.log(`  - Aplicação: ${extracted.application}`);
    console.log(`  - Versão: ${extracted.version}`);
    console.log(`  - Repositório: ${extracted.repository}`);

    return {
      success: true,
      changeNumber: chgData.changeNumber,
      shortDescription: chgData.shortDescription,
      description: chgData.description,
      state: chgData.state,
      extracted,
      confidence,
    };

  } catch (error) {
    console.error('[ServiceNow] Erro ao extrair dados:', error);
    return {
      success: false,
      extracted: {},
      confidence: { application: 0, version: 0, repository: 0 },
      error: error instanceof Error ? error.message : 'Erro desconhecido',
    };
  } finally {
    // Fechar browser (mas manter sessão salva)
    if (context && !browser) {
      await context.close();
    }
    if (browser) {
      await browser.close();
    }
  }
}

// CLI: executar diretamente com URL como argumento
if (require.main === module) {
  const url = process.argv[2];

  if (!url) {
    console.log('Uso: npx ts-node playwright/servicenow-extractor.ts <URL_DA_CHG>');
    console.log('');
    console.log('Exemplo:');
    console.log('  npx ts-node playwright/servicenow-extractor.ts "https://viavarejo.service-now.com/change_request.do?sys_id=abc123..."');
    process.exit(1);
  }

  extractCHGData(url, { headless: false, reuseSession: true })
    .then((result) => {
      console.log('\n=== RESULTADO ===');
      console.log(JSON.stringify(result, null, 2));
      process.exit(result.success ? 0 : 1);
    })
    .catch((err) => {
      console.error('Erro fatal:', err);
      process.exit(1);
    });
}
