package servicenow

import (
	"fmt"
	"os"
	"path/filepath"
)

// ServiceNowExtractorScript contém o script TypeScript embarcado
// Este script será criado automaticamente em ~/.k8s-hpa-manager/playwright/
const ServiceNowExtractorScript = `/**
 * ServiceNow CHG Extractor via Playwright
 *
 * Usa automação de browser para extrair dados da CHG do ServiceNow
 * aproveitando a autenticação Azure AD SSO do usuário.
 *
 * Uso:
 *   npx tsx servicenow-extractor.ts "https://viavarejo.service-now.com/change_request.do?sys_id=..."
 *   npx tsx servicenow-extractor.ts --login
 */

import { chromium, Browser, BrowserContext } from 'playwright';
import * as path from 'path';
import * as fs from 'fs';

// Diretório para armazenar sessão do browser (reutilizar login)
const USER_DATA_DIR = path.join(process.env.HOME || '', '.k8s-hpa-manager', 'playwright-session');

/**
 * Verifica se existe uma sessão válida (cookies do Azure AD)
 */
function hasValidSession(): boolean {
  try {
    if (!fs.existsSync(USER_DATA_DIR)) {
      console.log('[ServiceNow] Diretório de sessão não existe - login será necessário');
      return false;
    }

    const stateFiles = [
      path.join(USER_DATA_DIR, 'Default', 'Cookies'),
      path.join(USER_DATA_DIR, 'Default', 'Local Storage'),
      path.join(USER_DATA_DIR, 'Default', 'Session Storage'),
    ];

    const hasStateFiles = stateFiles.some(f => {
      try {
        return fs.existsSync(f);
      } catch {
        return false;
      }
    });

    if (!hasStateFiles) {
      console.log('[ServiceNow] Arquivos de sessão não encontrados - login será necessário');
      return false;
    }

    const stats = fs.statSync(USER_DATA_DIR);
    const lastModified = stats.mtime.getTime();
    const now = Date.now();
    const hoursSinceLastUse = (now - lastModified) / (1000 * 60 * 60);

    if (hoursSinceLastUse > 8) {
      console.log('[ServiceNow] Sessão antiga (' + hoursSinceLastUse.toFixed(1) + 'h) - login pode ser necessário');
      return false;
    }

    console.log('[ServiceNow] Sessão encontrada (' + hoursSinceLastUse.toFixed(1) + 'h atrás)');
    return true;
  } catch (error) {
    console.log('[ServiceNow] Erro ao verificar sessão:', error);
    return false;
  }
}

// Padrões de extração do template da esteira de CD
const EXTRACTION_PATTERNS = {
  application: /\* Aplicação\(ões\):\s*([^.\n]+)\./,
  version: /\* Versão:\s*([\d]+\.[\d]+\.[\d]+-?[\d]*)\./,
  repository: /\* Repositório:\s*github\.com\/[^/]+\/([^.]+)\.git/,
  squad: /\* Squad\(s\):\s*([^.\n]+)\./,
  branch: /\* Branch no GitHub:\s*([^\n]+)\./,
  product: /\* Produto:\s*([^.\n]+)\./,
  xlReleaseUrl: /\* Link da release no XL-Release:\s*(https?:\/\/[^\s\n]+)/,
  xlReleaseTitle: /\* Titulo da release no XL-Release:\s*([^\n]+)\./,
  jiraIssues: /([A-Z]+-\d+)/g,
};

interface ExtractedCHGData {
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

function extractSysId(url: string): string | null {
  const match = url.match(/sys_id=([a-f0-9]{32})/i);
  return match ? match[1] : null;
}

function parseDescription(description: string): ExtractedCHGData['extracted'] {
  const extracted: ExtractedCHGData['extracted'] = {};

  const appMatch = description.match(EXTRACTION_PATTERNS.application);
  if (appMatch) extracted.application = appMatch[1].trim();

  const versionMatch = description.match(EXTRACTION_PATTERNS.version);
  if (versionMatch) extracted.version = versionMatch[1].trim();

  const repoMatch = description.match(EXTRACTION_PATTERNS.repository);
  if (repoMatch) extracted.repository = repoMatch[1].trim();

  const squadMatch = description.match(EXTRACTION_PATTERNS.squad);
  if (squadMatch) extracted.squad = squadMatch[1].trim();

  const branchMatch = description.match(EXTRACTION_PATTERNS.branch);
  if (branchMatch) extracted.branch = branchMatch[1].trim();

  const productMatch = description.match(EXTRACTION_PATTERNS.product);
  if (productMatch) extracted.product = productMatch[1].trim();

  const xlUrlMatch = description.match(EXTRACTION_PATTERNS.xlReleaseUrl);
  if (xlUrlMatch) extracted.xlReleaseUrl = xlUrlMatch[1].trim();

  const xlTitleMatch = description.match(EXTRACTION_PATTERNS.xlReleaseTitle);
  if (xlTitleMatch) extracted.xlReleaseTitle = xlTitleMatch[1].trim();

  const jiraMatches = description.match(EXTRACTION_PATTERNS.jiraIssues);
  if (jiraMatches) extracted.jiraIssues = [...new Set(jiraMatches)];

  return extracted;
}

function calculateConfidence(extracted: ExtractedCHGData['extracted']): ExtractedCHGData['confidence'] {
  return {
    application: extracted.application ? 0.99 : 0,
    version: extracted.version ? 0.99 : 0,
    repository: extracted.repository ? 0.99 : 0,
  };
}

async function extractCHGData(chgUrl: string, options: {
  headless?: boolean;
  timeout?: number;
  reuseSession?: boolean;
  forceVisible?: boolean;
} = {}): Promise<ExtractedCHGData> {
  const sessionValid = hasValidSession();

  const {
    headless = !options.forceVisible,
    timeout = 60000,
    reuseSession = true,
    forceVisible = false,
  } = options;

  if (!sessionValid && !forceVisible) {
    console.log('[ServiceNow] Sessão não encontrada ou expirada.');
    console.log('[ServiceNow] Por favor, faça login via Menu de Perfil > ServiceNow Session > Fazer Login');
    return {
      success: false,
      extracted: {},
      confidence: { application: 0, version: 0, repository: 0 },
      error: 'Sessão expirada ou não encontrada. Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair dados.',
    };
  }

  console.log('[ServiceNow] Modo de execução: ' + (headless ? 'headless (silencioso)' : 'visível (login)') + ' (sessão válida: ' + sessionValid + ')');

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
    if (reuseSession && !fs.existsSync(USER_DATA_DIR)) {
      fs.mkdirSync(USER_DATA_DIR, { recursive: true });
    }

    if (reuseSession) {
      context = await chromium.launchPersistentContext(USER_DATA_DIR, {
        headless,
        args: ['--disable-blink-features=AutomationControlled'],
        viewport: { width: 1280, height: 800 },
      });
      browser = null;
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

    console.log('[ServiceNow] Navegando para: ' + chgUrl);
    await page.goto(chgUrl, { waitUntil: 'networkidle', timeout });

    const initialUrl = page.url();
    const loginPatterns = [
      'login.microsoftonline.com',
      'login.windows.net',
      'login.live.com',
      'adfs.',
      '/adfs/',
      'saml',
      'oauth',
      'authenticate',
      'signin',
      'sso.',
      '/sso/',
      'identity.',
      'sts.',
      'federation',
      'authn',
    ];

    const needsLogin = loginPatterns.some(pattern =>
      initialUrl.toLowerCase().includes(pattern.toLowerCase())
    );

    console.log('[ServiceNow] URL inicial: ' + initialUrl.substring(0, 100) + '...');
    console.log('[ServiceNow] Precisa login: ' + needsLogin);

    if (needsLogin && headless) {
      console.log('[ServiceNow] Login necessário mas extração está em modo silencioso.');
      console.log('[ServiceNow] Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair.');

      if (context) {
        try { await context.close(); } catch (e) { /* ignorar */ }
      }

      return {
        success: false,
        extracted: {},
        confidence: { application: 0, version: 0, repository: 0 },
        error: 'Login necessário. Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair dados.',
      };
    }

    if (!needsLogin && !page.url().includes('service-now.com')) {
      console.log('[ServiceNow] Aguardando redirecionamento para ServiceNow...');
      await page.waitForTimeout(3000);
    }

    let attempts = 0;
    const maxAttempts = 60;

    while (attempts < maxAttempts) {
      const currentUrl = page.url();

      if (attempts % 2 === 0) {
        console.log('[ServiceNow] Aguardando... (' + (attempts * 5) + 's)');
      }

      if (currentUrl.includes('service-now.com') && !currentUrl.includes('login')) {
        console.log('[ServiceNow] Acesso ao ServiceNow confirmado!');
        break;
      }

      const isLoginPage = loginPatterns.some(pattern =>
        currentUrl.toLowerCase().includes(pattern.toLowerCase())
      );

      if (isLoginPage) {
        if (headless) {
          console.log('[ServiceNow] Página de login detectada durante extração silenciosa.');
          console.log('[ServiceNow] Faça login pelo Menu de Perfil > ServiceNow Session.');

          if (context) {
            try { await context.close(); } catch (e) { /* ignorar */ }
          }

          return {
            success: false,
            extracted: {},
            confidence: { application: 0, version: 0, repository: 0 },
            error: 'Sessão expirada durante extração. Faça login pelo Menu de Perfil > ServiceNow Session.',
          };
        }

        if (attempts === 0) {
          console.log('[ServiceNow] Aguardando login do usuário no Azure AD...');
          console.log('[ServiceNow] Por favor, faça login na janela do navegador.');
          console.log('[ServiceNow] URL de login detectada: ' + currentUrl.substring(0, 80) + '...');
        }

        attempts++;
        await page.waitForTimeout(5000);
        continue;
      }

      attempts++;
      await page.waitForTimeout(5000);
    }

    if (attempts >= maxAttempts) {
      throw new Error('Timeout aguardando login no Azure AD (5 minutos)');
    }

    console.log('[ServiceNow] Aguardando carregamento completo...');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    console.log('[ServiceNow] Aguardando iframe gsft_main...');
    await page.waitForTimeout(5000);

    const allFrames = page.frames();
    console.log('[ServiceNow] Total de frames: ' + allFrames.length);

    let gsftFrame = page.frame('gsft_main');

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

    try {
      await targetFrame.waitForSelector('form, textarea, [aria-label="Motivo da mudança"]', { timeout: 10000 });
      console.log('[ServiceNow] Formulário encontrado');
    } catch (e) {
      console.log('[ServiceNow] Timeout aguardando formulário, continuando...');
    }

    const extractScript = ` + "`" + `
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

        var description = '';
        var justificationEl = document.querySelector('#sys_readonly\\.change_request\\.justification') ||
                             document.querySelector('[name="change_request.justification"]') ||
                             document.querySelector('[aria-label="Motivo da mudança"]');
        if (justificationEl) {
          description = justificationEl.value || justificationEl.textContent || '';
        }

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
    ` + "`" + `;
    const chgData = await targetFrame.evaluate(extractScript) as {
      changeNumber: string;
      shortDescription: string;
      description: string;
      state: string;
    };

    if (!chgData.description) {
      console.log('[ServiceNow] Tentando método alternativo para extrair description...');

      try {
        const descriptionTab = await targetFrame.$('text=Descrição');
        if (descriptionTab) {
          await descriptionTab.click();
          await page.waitForTimeout(1000);
        }
      } catch (e) {
        // Ignorar erro de clique
      }

      const altScript = ` + "`" + `
        (function() {
          var justEl = document.querySelector('#sys_readonly\\.change_request\\.justification') ||
                      document.querySelector('[name="change_request.justification"]') ||
                      document.querySelector('[aria-label="Motivo da mudança"]');
          if (justEl && (justEl.value || justEl.textContent)) {
            return justEl.value || justEl.textContent;
          }

          var textareas = document.querySelectorAll('textarea');
          for (var i = 0; i < textareas.length; i++) {
            var val = textareas[i].value || '';
            if (val.length > 100 && (val.indexOf('Aplicação') >= 0 || val.indexOf('Versão') >= 0 || val.indexOf('Squad') >= 0)) {
              return val;
            }
          }
          return '';
        })()
      ` + "`" + `;
      const altDescription = await targetFrame.evaluate(altScript) as string;

      if (altDescription) {
        chgData.description = altDescription;
      }
    }

    const extracted = parseDescription(chgData.description);
    const confidence = calculateConfidence(extracted);

    console.log('[ServiceNow] Dados extraídos com sucesso!');
    console.log('  - CHG: ' + chgData.changeNumber);
    console.log('  - Aplicação: ' + extracted.application);
    console.log('  - Versão: ' + extracted.version);
    console.log('  - Repositório: ' + extracted.repository);

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
    if (context && !browser) {
      await context.close();
    }
    if (browser) {
      await browser.close();
    }
  }
}

async function doLogin(options: {
  timeout?: number;
} = {}): Promise<{ success: boolean; message: string }> {
  const { timeout = 180000 } = options;

  console.log('[ServiceNow] Iniciando login - abrindo browser visível...');

  if (!fs.existsSync(USER_DATA_DIR)) {
    fs.mkdirSync(USER_DATA_DIR, { recursive: true });
  }

  let context: BrowserContext | null = null;

  try {
    context = await chromium.launchPersistentContext(USER_DATA_DIR, {
      headless: false,
      args: [
        '--disable-blink-features=AutomationControlled',
        '--no-first-run',
        '--disable-default-apps',
      ],
      viewport: { width: 1280, height: 800 },
    });

    const page = context.pages()[0] || await context.newPage();

    console.log('[ServiceNow] Navegando para ServiceNow...');
    await page.goto('https://viavarejo.service-now.com', { waitUntil: 'networkidle', timeout: 60000 });

    const loginPatterns = [
      'login.microsoftonline.com',
      'login.windows.net',
      'login.live.com',
      'adfs',
      'saml',
      'oauth',
      'signin',
      'sso',
    ];

    let attempts = 0;
    const maxAttempts = Math.ceil(timeout / 5000);

    while (attempts < maxAttempts) {
      const currentUrl = page.url();

      if (currentUrl.includes('service-now.com') &&
          !loginPatterns.some(p => currentUrl.toLowerCase().includes(p.toLowerCase()))) {
        console.log('[ServiceNow] Login realizado com sucesso!');
        await context.close();
        return { success: true, message: 'Login realizado com sucesso. Sessão salva.' };
      }

      if (attempts === 0) {
        console.log('[ServiceNow] Aguardando login do usuário no Azure AD...');
        console.log('[ServiceNow] Complete o login na janela do navegador.');
      }

      attempts++;
      await page.waitForTimeout(5000);
    }

    await context.close();
    return { success: false, message: 'Timeout aguardando login. Tente novamente.' };

  } catch (error) {
    if (context) {
      try { await context.close(); } catch (e) { /* ignorar */ }
    }
    const errorMsg = error instanceof Error ? error.message : 'Erro desconhecido';
    console.error('[ServiceNow] Erro no login:', errorMsg);
    return { success: false, message: errorMsg };
  }
}

// CLI
const isMainModule = import.meta.url === ` + "`" + `file://${process.argv[1]}` + "`" + ` ||
                     process.argv[1]?.endsWith('servicenow-extractor.ts');

if (isMainModule) {
  const command = process.argv[2];

  if (command === '--login') {
    doLogin()
      .then((result) => {
        console.log('\n=== RESULTADO ===');
        console.log(JSON.stringify(result, null, 2));
        process.exit(result.success ? 0 : 1);
      })
      .catch((err) => {
        console.error('Erro fatal:', err);
        process.exit(1);
      });
  } else if (command && command.includes('service-now.com')) {
    extractCHGData(command, { reuseSession: true })
      .then((result) => {
        console.log('\n=== RESULTADO ===');
        console.log(JSON.stringify(result, null, 2));
        process.exit(result.success ? 0 : 1);
      })
      .catch((err) => {
        console.error('Erro fatal:', err);
        process.exit(1);
      });
  } else {
    console.log('Uso:');
    console.log('  Extração: npx tsx servicenow-extractor.ts <URL_DA_CHG>');
    console.log('  Login:    npx tsx servicenow-extractor.ts --login');
    process.exit(1);
  }
}
`

// PackageJSON contém as dependências necessárias para o script
const PackageJSON = `{
  "name": "servicenow-extractor",
  "version": "1.0.0",
  "type": "module",
  "dependencies": {
    "playwright": "^1.40.0",
    "tsx": "^4.7.0"
  }
}
`

// GetPlaywrightDir retorna o diretório onde o script Playwright será instalado
func GetPlaywrightDir() string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE") // Windows
	}
	return filepath.Join(homeDir, ".k8s-hpa-manager", "playwright")
}

// EnsurePlaywrightScript garante que o script TypeScript exista no diretório do usuário
// Retorna o caminho do script e erro se houver
func EnsurePlaywrightScript() (string, error) {
	playwrightDir := GetPlaywrightDir()
	scriptPath := filepath.Join(playwrightDir, "servicenow-extractor.ts")
	packagePath := filepath.Join(playwrightDir, "package.json")

	// Criar diretório se não existir
	if err := os.MkdirAll(playwrightDir, 0755); err != nil {
		return "", fmt.Errorf("erro ao criar diretório playwright: %v", err)
	}

	// Verificar se script já existe
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		// Criar script
		if err := os.WriteFile(scriptPath, []byte(ServiceNowExtractorScript), 0644); err != nil {
			return "", fmt.Errorf("erro ao criar script: %v", err)
		}
	}

	// Verificar se package.json já existe
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		// Criar package.json
		if err := os.WriteFile(packagePath, []byte(PackageJSON), 0644); err != nil {
			return "", fmt.Errorf("erro ao criar package.json: %v", err)
		}
	}

	return scriptPath, nil
}

// IsPlaywrightInstalled verifica se as dependências npm estão instaladas
func IsPlaywrightInstalled() bool {
	playwrightDir := GetPlaywrightDir()
	nodeModules := filepath.Join(playwrightDir, "node_modules")

	// Verificar se node_modules existe
	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		return false
	}

	// Verificar se playwright existe
	playwrightModule := filepath.Join(nodeModules, "playwright")
	if _, err := os.Stat(playwrightModule); os.IsNotExist(err) {
		return false
	}

	return true
}
