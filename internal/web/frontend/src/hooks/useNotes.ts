import { useEffect, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import type { Note } from '../lib/api/types';

// Aba reservada (nunca corresponde a um activeTab real da UI) para notas "gerais" de um
// cluster — reminders que devem ficar visíveis independente de qual aba está aberta.
// Reaproveita 100% do backend/CRUD de notas por cluster+aba já existente, sem schema novo.
export const GENERAL_NOTES_TAB = '__general__';

// Hook para listar notas de um escopo (cluster+aba)
export function useNotes(cluster: string, tab: string) {
  return useQuery({
    queryKey: ['notes', cluster, tab],
    queryFn: () => apiClient.getNotes(cluster, tab),
    enabled: !!cluster && !!tab,
    staleTime: 30000,
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
