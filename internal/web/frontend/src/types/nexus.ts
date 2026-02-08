// Types for Nexus integration
export interface NexusConfig {
  baseUrl: string;
  repository: string;
  username: string;
  password: string;
  tempDir: string;
  urlPattern?: string; // Padrão de URL customizado
  // Placeholders: {baseUrl}, {repository}, {release}, {version}, {environment}, {type}
}

// Padrão de URL default
export const DEFAULT_URL_PATTERN = '{baseUrl}/repository/{repository}/{release}/{version}/{environment}/values/{type}-values.yaml';

export interface ValuesFileRequest {
  release: string;
  version: string;
  environment?: string;
  type?: string;
  repository?: string;
  filePath?: string; // Path real do arquivo (release/version/file.yaml)
}

export interface ValuesFileResponse {
  content: string;
  filePath: string;
  fullUrl: string;
  size: number;
  error?: string;
}

export interface CompareValuesRequest {
  file1: ValuesFileRequest;
  file2: ValuesFileRequest;
}

export interface CompareValuesResponse {
  file1: ValuesFileResponse;
  file2: ValuesFileResponse;
  error?: string;
}

export interface TestConnectionResponse {
  success: boolean;
  message: string;
  version?: string;
}

export interface NexusStatus {
  configured: boolean;
  baseUrl?: string;
  repository?: string;
  username?: string;
  tempDir?: string;
  urlPattern?: string;
}

export const VALID_ENVIRONMENTS = ['default', 'dev', 'sit', 'uat', 'hlg', 'prd'] as const;
export const VALID_TYPES = ['base', 'sit', 'prd', 'hlg', 'dev'] as const;

export interface BrowseItem {
  name: string;
  path: string;
  versions?: string[];
  repository?: string;
  files?: Record<string, string[]>; // versão → lista de arquivos reais
}

export interface BrowseResponse {
  items: BrowseItem[];
  path: string;
}
