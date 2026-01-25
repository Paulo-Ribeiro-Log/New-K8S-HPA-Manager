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
// Nota: Os campos terminam com ".\n" ou ".\t" - usar lookahead para capturar corretamente
const EXTRACTION_PATTERNS = {
  // * Aplicação(ões): tms-sync-1p-order-management-acl.
  application: /\* Aplicação\(ões\):\s*([^.\n]+)\./,

  // * Versão: 1.1.0-3.  (pode ter pontos no meio, então capturar até o ponto final)
  version: /\* Versão:\s*([\d]+\.[\d]+\.[\d]+-?[\d]*)\./,

  // * Repositório: github.com/viavarejo-internal/tms-sync-1p-order-management-acl.git.
  repository: /\* Repositório:\s*github\.com\/viavarejo-internal\/([^.]+)\.git/,

  // * Squad(s): Retira Rápido.
  squad: /\* Squad\(s\):\s*([^.\n]+)\./,

  // * Branch no GitHub: release/1.1.0.  (pode ter pontos no branch)
  branch: /\* Branch no GitHub:\s*([^\n]+)\./,

  // * Produto: gcb-order-details-api.
  product: /\* Produto:\s*([^.\n]+)\./,

  // * Link da release no XL-Release: http://release.viavarejo.com.br/#/releases/...
  xlReleaseUrl: /\* Link da release no XL-Release:\s*(https?:\/\/[^\s\n]+)/,

  // * Titulo da release no XL-Release: [Retira Rápido] gcb-order-details-api - 1.1.0-3.
  xlReleaseTitle: /\* Titulo da release no XL-Release:\s*([^\n]+)\./,

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
    headless = true, // Tentar headless primeiro, abrir visível apenas se precisar de login
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

    let page = context.pages()[0] || await context.newPage();

    // Navegar para a CHG
    console.log(`[ServiceNow] Navegando para: ${chgUrl}`);
    await page.goto(chgUrl, { waitUntil: 'networkidle', timeout });

    // Verificar se precisa de login (Azure AD)
    const initialUrl = page.url();
    const needsLogin = initialUrl.includes('login.microsoftonline.com') ||
                       initialUrl.includes('login.windows.net') ||
                       initialUrl.includes('saml');

    // Se precisa de login e estamos em modo headless, reabrir em modo visível
    if (needsLogin && headless) {
      console.log('[ServiceNow] Login necessário - reabrindo browser em modo visível...');

      // Fechar o contexto atual
      if (context) {
        await context.close();
      }

      // Reabrir em modo visível para o usuário fazer login
      context = await chromium.launchPersistentContext(USER_DATA_DIR, {
        headless: false, // Visível para login
        args: ['--disable-blink-features=AutomationControlled'],
        viewport: { width: 1280, height: 800 },
      });

      const newPage = context.pages()[0] || await context.newPage();
      await newPage.goto(chgUrl, { waitUntil: 'networkidle', timeout });

      // Usar a nova página
      page = newPage;
    }

    // Loop até conseguir acessar o ServiceNow
    let attempts = 0;
    const maxAttempts = 60; // 5 minutos (5 segundos por tentativa)

    while (attempts < maxAttempts) {
      const currentUrl = page.url();

      // Só logar a cada 10 tentativas para não poluir
      if (attempts % 2 === 0) {
        console.log(`[ServiceNow] Aguardando... (${attempts * 5}s)`);
      }

      // Se estamos no ServiceNow, podemos prosseguir
      if (currentUrl.includes('service-now.com') && !currentUrl.includes('login')) {
        console.log('[ServiceNow] Acesso ao ServiceNow confirmado!');
        break;
      }

      // Se estamos em página de login
      if (currentUrl.includes('login.microsoftonline.com') || currentUrl.includes('login.windows.net') || currentUrl.includes('saml')) {
        if (attempts === 0) {
          console.log('[ServiceNow] Aguardando login do usuário no Azure AD...');
          console.log('[ServiceNow] Por favor, faça login na janela do navegador.');
        }

        attempts++;
        await page.waitForTimeout(5000); // Esperar 5 segundos
        continue;
      }

      // URL desconhecida, aguardar um pouco
      attempts++;
      await page.waitForTimeout(5000);
    }

    if (attempts >= maxAttempts) {
      throw new Error('Timeout aguardando login no Azure AD (5 minutos)');
    }

    // Aguardar carregamento completo da página
    console.log('[ServiceNow] Aguardando carregamento completo...');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000); // Dar tempo extra para JavaScript carregar

    // ServiceNow usa iframe gsft_main - aguardar e usar o frame correto
    console.log('[ServiceNow] Aguardando iframe gsft_main...');
    await page.waitForTimeout(5000); // Dar mais tempo para iframes carregarem

    // Listar TODOS os frames com mais detalhes
    const allFrames = page.frames();
    console.log(`[ServiceNow] Total de frames: ${allFrames.length}`);
    for (let i = 0; i < allFrames.length; i++) {
      const f = allFrames[i];
      const frameName = f.name() || `frame-${i}`;
      const frameUrl = f.url();
      console.log(`[ServiceNow] Frame ${i}: name="${frameName}", url="${frameUrl.substring(0, 80)}..."`);
    }

    // Tentar encontrar o iframe gsft_main
    let gsftFrame = page.frame('gsft_main');

    // Se não encontrar por nome, procurar por URL que contenha change_request
    if (!gsftFrame) {
      for (const f of allFrames) {
        if (f.url().includes('change_request') || f.url().includes('sys_id')) {
          gsftFrame = f;
          console.log('[ServiceNow] Encontrado frame por URL:', f.url().substring(0, 80));
          break;
        }
      }
    }

    const targetFrame = gsftFrame || page.mainFrame();

    if (gsftFrame) {
      console.log('[ServiceNow] Usando iframe gsft_main para extração');
    } else {
      console.log('[ServiceNow] Usando frame principal');
    }

    // Debug: listar elementos na página
    const debugInfo = await targetFrame.evaluate(`
      (function() {
        var iframes = document.querySelectorAll('iframe');
        var textareas = document.querySelectorAll('textarea');
        var forms = document.querySelectorAll('form');

        return {
          iframeCount: iframes.length,
          iframeIds: Array.from(iframes).map(i => i.id || i.name || 'unnamed').slice(0, 5),
          textareaCount: textareas.length,
          textareaIds: Array.from(textareas).map(t => t.id || t.name || 'unnamed').slice(0, 10),
          formCount: forms.length,
          title: document.title,
          url: window.location.href.substring(0, 100)
        };
      })()
    `);
    console.log('[ServiceNow] Debug info:', JSON.stringify(debugInfo, null, 2));

    // Aguardar formulário carregar no frame correto
    try {
      await targetFrame.waitForSelector('form, textarea, [aria-label="Motivo da mudança"]', { timeout: 10000 });
      console.log('[ServiceNow] Formulário encontrado');
    } catch (e) {
      console.log('[ServiceNow] Timeout aguardando formulário, continuando...');
    }

    // Extrair dados da CHG usando JavaScript puro como string
    // Isso evita que o transpilador processe o código
    const extractScript = `
      (function() {
        function getFieldValue(fieldName) {
          var selectors = [
            'input[name="' + fieldName + '"]',
            'textarea[name="' + fieldName + '"]',
            '#' + fieldName,
            '[id$="' + fieldName + '"]',
            '[data-field="' + fieldName + '"]'
          ];

          for (var i = 0; i < selectors.length; i++) {
            var el = document.querySelector(selectors[i]);
            if (el && el.value) {
              return el.value;
            }
          }

          var iframes = document.querySelectorAll('iframe');
          for (var j = 0; j < iframes.length; j++) {
            try {
              var iframeDoc = iframes[j].contentDocument;
              if (iframeDoc) {
                for (var k = 0; k < selectors.length; k++) {
                  var el2 = iframeDoc.querySelector(selectors[k]);
                  if (el2 && el2.value) {
                    return el2.value;
                  }
                }
              }
            } catch (e) {}
          }
          return '';
        }

        // Número da CHG
        var changeNumber = '';
        var numberEl = document.querySelector('#sys_readonly\\.change_request\\.number') ||
                      document.querySelector('[name="change_request.number"]') ||
                      document.querySelector('[aria-label="Número"]');
        if (numberEl) {
          changeNumber = numberEl.value || numberEl.textContent || '';
        }
        if (!changeNumber) {
          changeNumber = getFieldValue('number') ||
                        getFieldValue('sys_readonly.change_request.number') || '';
        }
        if (!changeNumber) {
          var match = document.title.match(/(CHG[0-9]+)/i);
          if (match) changeNumber = match[1];
        }

        // Short description
        var shortDescription = '';
        var shortDescEl = document.querySelector('#sys_readonly\\.change_request\\.short_description') ||
                         document.querySelector('[name="change_request.short_description"]') ||
                         document.querySelector('[aria-label="Descrição resumida"]');
        if (shortDescEl) {
          shortDescription = shortDescEl.value || shortDescEl.textContent || '';
        }
        if (!shortDescription) {
          shortDescription = getFieldValue('short_description') ||
                            getFieldValue('sys_readonly.change_request.short_description') || '';
        }

        // Motivo da mudança / Justification - campo principal
        var description = '';

        // Seletor específico do ServiceNow para "Motivo da mudança"
        var justificationEl = document.querySelector('#sys_readonly\\.change_request\\.justification') ||
                             document.querySelector('[name="change_request.justification"]') ||
                             document.querySelector('[aria-label="Motivo da mudança"]');
        if (justificationEl) {
          description = justificationEl.value || justificationEl.textContent || '';
        }

        // Fallback para outros campos
        if (!description) {
          description = getFieldValue('description') ||
                       getFieldValue('sys_readonly.change_request.description') ||
                       getFieldValue('u_motivo_mudanca') || '';
        }

        var stateEl = document.querySelector('[name="state"]');
        var state = stateEl && stateEl.options ? (stateEl.options[stateEl.selectedIndex] ? stateEl.options[stateEl.selectedIndex].text : '') : '';

        return {
          changeNumber: changeNumber,
          shortDescription: shortDescription,
          description: description,
          state: state
        };
      })()
    `;
    const chgData = await targetFrame.evaluate(extractScript) as {
      changeNumber: string;
      shortDescription: string;
      description: string;
      state: string;
    };

    // Se não conseguiu extrair description, tentar método alternativo
    if (!chgData.description) {
      console.log('[ServiceNow] Tentando método alternativo para extrair description...');

      // Tentar clicar em tab ou expandir campo se necessário
      try {
        const descriptionTab = await targetFrame.$('text=Descrição');
        if (descriptionTab) {
          await descriptionTab.click();
          await page.waitForTimeout(1000);
        }
      } catch (e) {
        // Ignorar erro de clique
      }

      // Tentar extrair novamente usando string para evitar transpilação
      const altScript = `
        (function() {
          // Procurar especificamente pelo textarea de justificação
          var justEl = document.querySelector('#sys_readonly\\.change_request\\.justification') ||
                      document.querySelector('[name="change_request.justification"]') ||
                      document.querySelector('[aria-label="Motivo da mudança"]');
          if (justEl && (justEl.value || justEl.textContent)) {
            return justEl.value || justEl.textContent;
          }

          // Fallback: procurar textareas com conteúdo relevante
          var textareas = document.querySelectorAll('textarea');
          for (var i = 0; i < textareas.length; i++) {
            var val = textareas[i].value || '';
            if (val.length > 100 && (val.indexOf('Aplicação') >= 0 || val.indexOf('Versão') >= 0 || val.indexOf('Squad') >= 0)) {
              return val;
            }
          }
          return '';
        })()
      `;
      const altDescription = await targetFrame.evaluate(altScript) as string;

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
// Detecta se está sendo executado diretamente (compatível com ESM)
const isMainModule = import.meta.url === `file://${process.argv[1]}` ||
                     process.argv[1]?.endsWith('servicenow-extractor.ts');

if (isMainModule) {
  const url = process.argv[2];

  if (!url) {
    console.log('Uso: npx tsx playwright/servicenow-extractor.ts <URL_DA_CHG>');
    console.log('');
    console.log('Exemplo:');
    console.log('  npx tsx playwright/servicenow-extractor.ts "https://viavarejo.service-now.com/change_request.do?sys_id=abc123..."');
    process.exit(1);
  }

  extractCHGData(url, { headless: true, reuseSession: true })
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
