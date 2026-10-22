// Reconhece erros do `az` que se resolvem com um novo `az login` e extrai o que ajuda a explicar.
const AZ_LOGIN_RE = /az login|AADSTS\d+|refresh token has expired|sign-in frequency/i;

export function isAzureLoginError(text: string): boolean {
  return AZ_LOGIN_RE.test(text);
}

export interface AzureLoginErrorInfo {
  code?: string; // ex.: AADSTS70043
  issuedAt?: string;
  maxLifetimeHours?: number;
  signInFrequency: boolean;
}

export function parseAzureLoginError(text: string): AzureLoginErrorInfo {
  const seconds = text.match(/maximum allowed lifetime for this request is (\d+)/i)?.[1];
  return {
    code: text.match(/AADSTS\d+/i)?.[0].toUpperCase(),
    issuedAt: text.match(/issued on ([0-9T:.-]+Z)/i)?.[1],
    maxLifetimeHours: seconds ? Math.round((Number(seconds) / 3600) * 10) / 10 : undefined,
    signInFrequency: /sign-in frequency/i.test(text),
  };
}
