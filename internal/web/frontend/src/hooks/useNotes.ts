import { useEffect, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import type { Note } from '../lib/api/types';

// Aba reservada (nunca corresponde a um activeTab real da UI) para notas "gerais" de um
// cluster — reminders que devem ficar visíveis independente de qual aba está aberta.
// Reaproveita 100% do backend/CRUD de notas por cluster+aba já existente, sem schema novo.
export const GENERAL_NOTES_TAB = '__general__';

// Cluster reservado (nunca corresponde a um cluster real do kubeconfig — contexts reais nunca
// começam com "__") usado SÓ pelos "Lembretes gerais": eles precisam sobreviver a troca de
// cluster e a reinícios do app, então não podem ficar amarrados ao `selectedCluster` do momento
// em que foram criados — diferente das notas normais (por cluster+aba), que continuam escopadas
// de propósito. Sem esse sentinel, um lembrete "geral" ficava invisível assim que o usuário
// trocava de cluster (e o app não lembra o último cluster selecionado entre reinícios), dando a
// falsa impressão de que o lembrete tinha sido perdido — ele nunca saiu do SQLite, só ficou
// fora do escopo de cluster+aba sendo consultado.
export const GENERAL_NOTES_CLUSTER = '__all_clusters__';

// Hook para listar notas de um escopo (cluster+aba)
export function useNotes(cluster: string, tab: string) {
  return useQuery({
    queryKey: ['notes', cluster, tab],
    queryFn: () => apiClient.getNotes(cluster, tab),
    enabled: !!cluster && !!tab,
    staleTime: 30000,
    // refetchInterval garante recuperação automática quando o servidor reinicia (make build,
    // rebuild-web.sh) com a aba já aberta: o retry padrão do React Query desiste depois de só
    // algumas tentativas com backoff (poucos segundos), tempo menor que o de um rebuild+restart
    // típico — sem isso, a query ficava presa em erro até um F5 manual, dando a impressão de que
    // "as notas não persistem" quando na verdade só não recarregavam sozinhas.
    refetchInterval: 60000,
    // Retry mais insistente/rápido do que o padrão do React Query (3 tentativas, backoff até
    // 30s) especificamente pro PRIMEIRO carregamento: se a página é aberta/recarregada bem no
    // momento em que o backend acabou de ser morto (`kill <PID>`) e ainda não voltou a escutar
    // na porta (janela típica de poucos segundos do fluxo `make build && kill && web -f`), a
    // query nunca teve dados — cai direto em "Nenhuma nota ainda", indistinguível de uma nota
    // de fato nunca criada. Sem isso, o usuário só via a lista recuperar no próximo tick do
    // refetchInterval (até 60s depois), dando a falsa impressão de perda de dados logo depois
    // de reiniciar o servidor.
    retry: 6,
    retryDelay: (attempt) => Math.min(1000 * (attempt + 1), 5000),
  });
}

// Hook para buscar notas por conteúdo em TODOS os clusters/abas (não escopado).
// Debounce de 400ms para não disparar uma request por tecla digitada.
export function useSearchNotes(query: string) {
  const [debounced, setDebounced] = useState(query);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(query), 400);
    return () => clearTimeout(t);
  }, [query]);

  const trimmed = debounced.trim();
  return useQuery({
    queryKey: ['notes-search', trimmed],
    queryFn: () => apiClient.searchNotes(trimmed),
    enabled: trimmed.length >= 2,
    staleTime: 30000,
  });
}

// Hook para criar uma nova entrada de nota (modelo diário — nunca sobrescreve)
export function useCreateNote(cluster: string, tab: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (content: string) => apiClient.createNote(cluster, tab, content),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notes', cluster, tab] });
    },
  });
}

// Hook para editar uma entrada específica (só o autor pode editar — backend retorna 403)
export function useUpdateNote(cluster: string, tab: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, content }: { id: number; content: string }) =>
      apiClient.updateNote(id, content),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notes', cluster, tab] });
    },
  });
}

// Hook para excluir uma entrada específica (só o autor pode excluir — backend retorna 403)
export function useDeleteNote(cluster: string, tab: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: number) => apiClient.deleteNote(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notes', cluster, tab] });
    },
  });
}
