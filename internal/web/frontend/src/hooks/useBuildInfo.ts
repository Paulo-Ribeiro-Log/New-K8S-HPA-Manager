import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { isFrontendStale, loadedBundleName, type ServerBuild } from "@/lib/buildInfo";

const FIVE_MIN = 5 * 60 * 1000;

// Build do servidor + se esta página é de um build anterior (o bundle carregado difere do embutido no
// servidor). Reconsulta a cada 5 min e ao voltar para a janela — é nesses momentos que um servidor
// reiniciado com outro binário aparece.
export function useBuildInfo(): { build?: ServerBuild; loadedBundle: string; stale: boolean } {
  const { data } = useQuery({
    queryKey: ["build-info"],
    queryFn: () => apiClient.getBuildInfo(),
    refetchInterval: FIVE_MIN,
    refetchOnWindowFocus: true,
    staleTime: 60_000,
    retry: false,
  });
  const loadedBundle = loadedBundleName(Array.from(document.querySelectorAll("script[src]"), (s) => (s as HTMLScriptElement).src));
  return { build: data?.build, loadedBundle, stale: isFrontendStale(loadedBundle, data?.build?.frontend_bundle) };
}
