// Validation utilities for Kubernetes resources

export interface ValidationError {
  field: string;
  value: string;
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
}

// CPU Validation
// Valid formats: "100m", "0.5", "1", "2.5"
const cpuRegex = /^(\d+\.?\d*|\.\d+)m?$/;

export function validateCPU(value: string): ValidationError | null {
  if (!value || value.trim() === "") {
    return null; // Empty is valid (optional field)
  }

  const trimmed = value.trim();
  if (!cpuRegex.test(trimmed)) {
    return {
      field: "cpu",
      value,
      message: "Formato inválido. Use: 100m (milicores), 0.5, 1, ou 2 (cores)",
    };
  }

  // Parse value to check if it's positive
  const numStr = trimmed.replace("m", "");
  const num = parseFloat(numStr);
  if (isNaN(num) || num <= 0) {
    return {
      field: "cpu",
      value,
      message: "Valor deve ser positivo",
    };
  }

  // Check reasonable limits (max 128 cores)
  if (trimmed.endsWith("m")) {
    if (num > 128000) {
      return {
        field: "cpu",
        value,
        message: "Valor muito alto (máximo: 128 cores ou 128000m)",
      };
    }
  } else {
    if (num > 128) {
      return {
        field: "cpu",
        value,
        message: "Valor muito alto (máximo: 128 cores)",
      };
    }
  }

  return null;
}

// Memory Validation
// Valid formats: "128Mi", "1Gi", "512Mi", "2Gi"
const memoryRegex = /^(\d+)(Mi|Gi|M|G|Ki|K|Ti|T|Pi|P|Ei|E)$/;

export function validateMemory(value: string): ValidationError | null {
  if (!value || value.trim() === "") {
    return null; // Empty is valid (optional field)
  }

  const trimmed = value.trim();
  if (!memoryRegex.test(trimmed)) {
    return {
      field: "memory",
      value,
      message: "Formato inválido. Use: 128Mi, 1Gi, 512Mi, 2Gi (K8s format)",
    };
  }

  const matches = trimmed.match(memoryRegex);
  if (!matches || matches.length < 3) {
    return {
      field: "memory",
      value,
      message: "Formato inválido",
    };
  }

  const num = parseInt(matches[1], 10);
  const unit = matches[2];

  // Check if value is positive
  if (num <= 0) {
    return {
      field: "memory",
      value,
      message: "Valor deve ser positivo",
    };
  }

  // Convert to bytes for validation
  let bytes = num;
  switch (unit) {
    case "Ki":
    case "K":
      bytes *= 1024;
      break;
    case "Mi":
    case "M":
      bytes *= 1024 * 1024;
      break;
    case "Gi":
    case "G":
      bytes *= 1024 * 1024 * 1024;
      break;
    case "Ti":
    case "T":
      bytes *= 1024 * 1024 * 1024 * 1024;
      break;
    case "Pi":
    case "P":
      bytes *= 1024 * 1024 * 1024 * 1024 * 1024;
      break;
    case "Ei":
    case "E":
      bytes *= 1024 * 1024 * 1024 * 1024 * 1024 * 1024;
      break;
  }

  // Check reasonable limits (max 1Ti)
  const maxBytes = 1024 * 1024 * 1024 * 1024; // 1Ti
  if (bytes > maxBytes) {
    return {
      field: "memory",
      value,
      message: "Valor muito alto (máximo: 1Ti)",
    };
  }

  // Warn about very small values (< 64Mi)
  const minBytes = 64 * 1024 * 1024; // 64Mi
  if (bytes < minBytes) {
    return {
      field: "memory",
      value,
      message: "Valor muito baixo (mínimo recomendado: 64Mi)",
    };
  }

  return null;
}

// Target CPU/Memory Utilization (1-100%)
export function validateTargetPercentage(
  value: number,
  fieldName: string
): ValidationError | null {
  if (value < 1 || value > 100) {
    return {
      field: fieldName,
      value: value.toString(),
      message: "Valor deve estar entre 1 e 100 (%)",
    };
  }
  return null;
}

// Replicas Validation
export function validateReplicas(
  min?: number,
  max?: number,
  current?: number
): ValidationResult {
  const result: ValidationResult = { valid: true, errors: [] };

  // Min replicas
  if (min !== undefined) {
    if (min < 1) {
      result.valid = false;
      result.errors.push({
        field: "min_replicas",
        value: min.toString(),
        message: "Mínimo deve ser >= 1",
      });
    }
    if (min > 100) {
      result.valid = false;
      result.errors.push({
        field: "min_replicas",
        value: min.toString(),
        message: "Mínimo muito alto (máximo recomendado: 100)",
      });
    }
  }

  // Max replicas
  if (max !== undefined) {
    if (max < 1) {
      result.valid = false;
      result.errors.push({
        field: "max_replicas",
        value: max.toString(),
        message: "Máximo deve ser >= 1",
      });
    }
    if (max > 1000) {
      result.valid = false;
      result.errors.push({
        field: "max_replicas",
        value: max.toString(),
        message: "Máximo muito alto (máximo recomendado: 1000)",
      });
    }
  }

  // Min <= Max
  if (min !== undefined && max !== undefined && min > max) {
    result.valid = false;
    result.errors.push({
      field: "min_replicas",
      value: min.toString(),
      message: "Mínimo não pode ser maior que máximo",
    });
  }

  // Current replicas
  if (current !== undefined) {
    if (current < 0) {
      result.valid = false;
      result.errors.push({
        field: "replicas",
        value: current.toString(),
        message: "Réplicas não podem ser negativas",
      });
    }
    if (current > 1000) {
      result.valid = false;
      result.errors.push({
        field: "replicas",
        value: current.toString(),
        message: "Réplicas muito alto (máximo recomendado: 1000)",
      });
    }
  }

  return result;
}

// Node Pool Validation
export function validateNodePool(
  nodeCount?: number,
  minCount?: number,
  maxCount?: number
): ValidationResult {
  const result: ValidationResult = { valid: true, errors: [] };

  // Node count
  if (nodeCount !== undefined) {
    if (nodeCount < 0) {
      result.valid = false;
      result.errors.push({
        field: "node_count",
        value: nodeCount.toString(),
        message: "Node count não pode ser negativo",
      });
    }
    if (nodeCount > 100) {
      result.valid = false;
      result.errors.push({
        field: "node_count",
        value: nodeCount.toString(),
        message: "Node count muito alto (máximo recomendado: 100)",
      });
    }
  }

  // Min count
  if (minCount !== undefined) {
    if (minCount < 0) {
      result.valid = false;
      result.errors.push({
        field: "min_count",
        value: minCount.toString(),
        message: "Min count não pode ser negativo",
      });
    }
    if (minCount > 100) {
      result.valid = false;
      result.errors.push({
        field: "min_count",
        value: minCount.toString(),
        message: "Min count muito alto (máximo recomendado: 100)",
      });
    }
  }

  // Max count
  if (maxCount !== undefined) {
    if (maxCount < 1) {
      result.valid = false;
      result.errors.push({
        field: "max_count",
        value: maxCount.toString(),
        message: "Max count deve ser >= 1",
      });
    }
    if (maxCount > 100) {
      result.valid = false;
      result.errors.push({
        field: "max_count",
        value: maxCount.toString(),
        message: "Max count muito alto (máximo recomendado: 100)",
      });
    }
  }

  // Min <= Max
  if (minCount !== undefined && maxCount !== undefined && minCount > maxCount) {
    result.valid = false;
    result.errors.push({
      field: "min_count",
      value: minCount.toString(),
      message: "Min count não pode ser maior que max count",
    });
  }

  return result;
}

// Validate HPA Update (complete validation)
export function validateHPAUpdate(data: {
  minReplicas?: number;
  maxReplicas?: number;
  targetCPU?: number;
  targetMemory?: number;
  cpuRequest?: string;
  memoryRequest?: string;
  cpuLimit?: string;
  memoryLimit?: string;
}): ValidationResult {
  const result: ValidationResult = { valid: true, errors: [] };

  // Validate replicas
  const replicasResult = validateReplicas(data.minReplicas, data.maxReplicas);
  if (!replicasResult.valid) {
    result.valid = false;
    result.errors.push(...replicasResult.errors);
  }

  // Validate target percentages
  if (data.targetCPU !== undefined) {
    const error = validateTargetPercentage(data.targetCPU, "target_cpu");
    if (error) {
      result.valid = false;
      result.errors.push(error);
    }
  }
  if (data.targetMemory !== undefined) {
    const error = validateTargetPercentage(data.targetMemory, "target_memory");
    if (error) {
      result.valid = false;
      result.errors.push(error);
    }
  }

  // Validate resources
  if (data.cpuRequest) {
    const error = validateCPU(data.cpuRequest);
    if (error) {
      result.valid = false;
      result.errors.push({ ...error, field: "cpu_request" });
    }
  }
  if (data.memoryRequest) {
    const error = validateMemory(data.memoryRequest);
    if (error) {
      result.valid = false;
      result.errors.push({ ...error, field: "memory_request" });
    }
  }
  if (data.cpuLimit) {
    const error = validateCPU(data.cpuLimit);
    if (error) {
      result.valid = false;
      result.errors.push({ ...error, field: "cpu_limit" });
    }
  }
  if (data.memoryLimit) {
    const error = validateMemory(data.memoryLimit);
    if (error) {
      result.valid = false;
      result.errors.push({ ...error, field: "memory_limit" });
    }
  }

  // Validate request <= limit
  if (data.cpuRequest && data.cpuLimit) {
    const reqVal = parseCPUToMillicores(data.cpuRequest);
    const limVal = parseCPUToMillicores(data.cpuLimit);
    if (reqVal > limVal) {
      result.valid = false;
      result.errors.push({
        field: "cpu_request",
        value: data.cpuRequest,
        message: "CPU request não pode ser maior que CPU limit",
      });
    }
  }
  if (data.memoryRequest && data.memoryLimit) {
    const reqVal = parseMemoryToBytes(data.memoryRequest);
    const limVal = parseMemoryToBytes(data.memoryLimit);
    if (reqVal > limVal) {
      result.valid = false;
      result.errors.push({
        field: "memory_request",
        value: data.memoryRequest,
        message: "Memory request não pode ser maior que memory limit",
      });
    }
  }

  return result;
}

// Helper: Convert CPU string to millicores
function parseCPUToMillicores(cpu: string): number {
  const trimmed = cpu.trim();
  if (trimmed.endsWith("m")) {
    return parseInt(trimmed.replace("m", ""), 10);
  }
  return parseFloat(trimmed) * 1000;
}

// Helper: Convert memory string to bytes
function parseMemoryToBytes(memory: string): number {
  const trimmed = memory.trim();
  const matches = trimmed.match(memoryRegex);
  if (!matches || matches.length < 3) {
    return 0;
  }

  let num = parseInt(matches[1], 10);
  const unit = matches[2];

  switch (unit) {
    case "Ki":
    case "K":
      num *= 1024;
      break;
    case "Mi":
    case "M":
      num *= 1024 * 1024;
      break;
    case "Gi":
    case "G":
      num *= 1024 * 1024 * 1024;
      break;
    case "Ti":
    case "T":
      num *= 1024 * 1024 * 1024 * 1024;
      break;
  }
  return num;
}

// Format validation errors for display
export function formatValidationErrors(errors: ValidationError[]): string {
  return errors.map((err) => `${err.field}: ${err.message}`).join("\n");
}
