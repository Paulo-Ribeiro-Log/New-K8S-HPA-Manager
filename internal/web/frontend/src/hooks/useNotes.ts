import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../lib/api/client';
import type { Note } from '../lib/api/types';

// Hook para listar notas de um escopo (cluster+aba)
export function useNotes(cluster: string, tab: string) {
  return useQuery({
    queryKey: ['notes', cluster, tab],
    queryFn: () => apiClient.getNotes(cluster, tab),
    enabled: !!cluster && !!tab,
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
