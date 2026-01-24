import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './playwright',
  timeout: 120000, // 2 minutos para permitir login Azure AD
  retries: 0,
  use: {
    headless: false, // Não-headless para permitir login Azure AD
    viewport: { width: 1280, height: 800 },
    actionTimeout: 30000,
    navigationTimeout: 60000,
    // Persistir sessão entre execuções
    launchOptions: {
      args: ['--disable-blink-features=AutomationControlled'],
    },
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
