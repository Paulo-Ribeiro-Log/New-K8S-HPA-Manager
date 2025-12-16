/**
 * Componentes RBAC para controle de acesso baseado em Azure AD
 *
 * Exporta:
 * - ProtectedAction: Wrapper que oculta/desabilita ações para não-SREs
 * - ConditionalRender: Renderização condicional baseada em permissões
 * - SREBadge: Badge que exibe status SRE do usuário
 */

export { ProtectedAction, ConditionalRender } from './ProtectedAction';
export { SREBadge } from './SREBadge';
