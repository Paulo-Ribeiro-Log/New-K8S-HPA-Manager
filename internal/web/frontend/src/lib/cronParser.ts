/**
 * Utilitário para interpretar e explicar expressões Cron
 * Formato: minuto hora dia mês dia-da-semana
 */

export interface CronParts {
  minute: string;
  hour: string;
  dayOfMonth: string;
  month: string;
  dayOfWeek: string;
}

export interface CronExplanation {
  readable: string;
  parts: {
    minute: string;
    hour: string;
    dayOfMonth: string;
    month: string;
    dayOfWeek: string;
  };
}

const MONTHS = [
  'Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho',
  'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'
];

const DAYS_OF_WEEK = [
  'Domingo', 'Segunda', 'Terça', 'Quarta', 'Quinta', 'Sexta', 'Sábado'
];

/**
 * Parse uma expressão cron em suas partes
 */
export function parseCronExpression(cronExp: string): CronParts | null {
  const parts = cronExp.trim().split(/\s+/);

  if (parts.length !== 5) {
    return null;
  }

  return {
    minute: parts[0],
    hour: parts[1],
    dayOfMonth: parts[2],
    month: parts[3],
    dayOfWeek: parts[4],
  };
}

/**
 * Explica um valor de campo cron
 */
function explainCronField(value: string, type: 'minute' | 'hour' | 'dayOfMonth' | 'month' | 'dayOfWeek'): string {
  // Asterisco significa "todos"
  if (value === '*') {
    switch (type) {
      case 'minute': return 'todo minuto';
      case 'hour': return 'toda hora';
      case 'dayOfMonth': return 'todo dia';
      case 'month': return 'todo mês';
      case 'dayOfWeek': return 'toda semana';
    }
  }

  // Range (ex: 1-5)
  if (value.includes('-')) {
    const [start, end] = value.split('-');
    if (type === 'dayOfWeek') {
      return `de ${DAYS_OF_WEEK[parseInt(start)]} a ${DAYS_OF_WEEK[parseInt(end)]}`;
    }
    if (type === 'month') {
      return `de ${MONTHS[parseInt(start) - 1]} a ${MONTHS[parseInt(end) - 1]}`;
    }
    return `de ${start} a ${end}`;
  }

  // Lista (ex: 1,3,5)
  if (value.includes(',')) {
    const items = value.split(',');
    if (type === 'dayOfWeek') {
      return items.map(i => DAYS_OF_WEEK[parseInt(i)]).join(', ');
    }
    if (type === 'month') {
      return items.map(i => MONTHS[parseInt(i) - 1]).join(', ');
    }
    return items.join(', ');
  }

  // Step (ex: */5 = a cada 5)
  if (value.includes('/')) {
    const [base, step] = value.split('/');
    const stepNum = parseInt(step);

    switch (type) {
      case 'minute': return `a cada ${stepNum} minuto${stepNum > 1 ? 's' : ''}`;
      case 'hour': return `a cada ${stepNum} hora${stepNum > 1 ? 's' : ''}`;
      case 'dayOfMonth': return `a cada ${stepNum} dia${stepNum > 1 ? 's' : ''}`;
      case 'month': return `a cada ${stepNum} mês${stepNum > 1 ? 'es' : ''}`;
      case 'dayOfWeek': return `a cada ${stepNum} dia${stepNum > 1 ? 's' : ''} da semana`;
    }
  }

  // Valor específico
  const num = parseInt(value);

  switch (type) {
    case 'minute':
      return `no minuto ${value.padStart(2, '0')}`;
    case 'hour':
      return `às ${value.padStart(2, '0')}h`;
    case 'dayOfMonth':
      return `no dia ${value}`;
    case 'month':
      return `em ${MONTHS[num - 1]}`;
    case 'dayOfWeek':
      return `às ${DAYS_OF_WEEK[num]}s`;
  }

  return value;
}

/**
 * Gera uma explicação legível em português para uma expressão cron
 */
export function explainCronExpression(cronExp: string): CronExplanation | null {
  const parts = parseCronExpression(cronExp);

  if (!parts) {
    return null;
  }

  // Explicações individuais de cada parte
  const explanations = {
    minute: explainCronField(parts.minute, 'minute'),
    hour: explainCronField(parts.hour, 'hour'),
    dayOfMonth: explainCronField(parts.dayOfMonth, 'dayOfMonth'),
    month: explainCronField(parts.month, 'month'),
    dayOfWeek: explainCronField(parts.dayOfWeek, 'dayOfWeek'),
  };

  // Gerar descrição legível completa
  let readable = '';

  // Casos especiais comuns primeiro
  if (cronExp === '* * * * *') {
    readable = 'A cada minuto';
  } else if (cronExp === '0 * * * *') {
    readable = 'A cada hora (no início da hora)';
  } else if (cronExp === '0 0 * * *') {
    readable = 'Todos os dias à meia-noite';
  } else if (parts.minute === '0' && parts.hour !== '*' && parts.dayOfMonth === '*' && parts.month === '*' && parts.dayOfWeek === '*') {
    // Ex: 0 5 * * * -> "Todos os dias às 05:00"
    readable = `Todos os dias às ${parts.hour.padStart(2, '0')}:00`;
  } else if (parts.minute !== '*' && parts.hour !== '*' && parts.dayOfMonth === '*' && parts.month === '*' && parts.dayOfWeek === '*') {
    // Ex: 30 14 * * * -> "Todos os dias às 14:30"
    readable = `Todos os dias às ${parts.hour.padStart(2, '0')}:${parts.minute.padStart(2, '0')}`;
  } else if (parts.dayOfWeek !== '*' && parts.dayOfMonth === '*' && parts.month === '*') {
    // Ex: 0 9 * * 1 -> "Todas as Segundas às 09:00"
    const dayName = DAYS_OF_WEEK[parseInt(parts.dayOfWeek)];
    const time = `${parts.hour.padStart(2, '0')}:${parts.minute.padStart(2, '0')}`;
    readable = `Todas as ${dayName}s às ${time}`;
  } else if (parts.dayOfMonth !== '*' && parts.month === '*' && parts.dayOfWeek === '*') {
    // Ex: 0 8 1 * * -> "No dia 1 de cada mês às 08:00"
    const time = `${parts.hour.padStart(2, '0')}:${parts.minute.padStart(2, '0')}`;
    readable = `No dia ${parts.dayOfMonth} de cada mês às ${time}`;
  } else {
    // Construir descrição genérica
    const timePart = parts.minute === '*' && parts.hour === '*'
      ? ''
      : parts.minute === '*'
        ? `às ${parts.hour.padStart(2, '0')}h`
        : `às ${parts.hour.padStart(2, '0')}:${parts.minute.padStart(2, '0')}`;

    const dayPart = parts.dayOfMonth === '*' && parts.dayOfWeek === '*'
      ? 'todos os dias'
      : parts.dayOfWeek !== '*'
        ? explanations.dayOfWeek
        : explanations.dayOfMonth;

    const monthPart = parts.month === '*' ? '' : explanations.month;

    readable = [timePart, dayPart, monthPart]
      .filter(p => p)
      .join(' ')
      .replace(/^./, c => c.toUpperCase());
  }

  return {
    readable,
    parts: explanations,
  };
}

/**
 * Valida se uma expressão cron é válida
 */
export function isValidCronExpression(cronExp: string): boolean {
  const parts = parseCronExpression(cronExp);

  if (!parts) {
    return false;
  }

  // Validar ranges básicos
  const isValidField = (value: string, min: number, max: number): boolean => {
    if (value === '*') return true;
    if (value.includes('/')) {
      const [, step] = value.split('/');
      const stepNum = parseInt(step);
      return !isNaN(stepNum) && stepNum > 0;
    }
    if (value.includes('-')) {
      const [start, end] = value.split('-').map(v => parseInt(v));
      return !isNaN(start) && !isNaN(end) && start >= min && end <= max && start <= end;
    }
    if (value.includes(',')) {
      return value.split(',').every(v => {
        const num = parseInt(v);
        return !isNaN(num) && num >= min && num <= max;
      });
    }

    const num = parseInt(value);
    return !isNaN(num) && num >= min && num <= max;
  };

  return (
    isValidField(parts.minute, 0, 59) &&
    isValidField(parts.hour, 0, 23) &&
    isValidField(parts.dayOfMonth, 1, 31) &&
    isValidField(parts.month, 1, 12) &&
    isValidField(parts.dayOfWeek, 0, 6)
  );
}
