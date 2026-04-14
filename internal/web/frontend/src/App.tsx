import { useState, useEffect } from "react";
import { Toaster } from "@/components/ui/toaster";
import { Toaster as Sonner } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import Index from "./pages/Index";
import { Login } from "./pages/Login";
import NotFound from "./pages/NotFound";
import AlertsPage from "./pages/AlertsPage";
import { AIAnalysisPage } from "./pages/AIAnalysisPage";
import { apiClient } from "./lib/api/client";
import { StagingProvider } from "./contexts/StagingContext";
import { TabProvider } from "./contexts/TabContext";
import { ThemeProvider } from "@/components/theme-provider";
import { useHeartbeat } from "./hooks/useHeartbeat";

const queryClient = new QueryClient();

const App = () => {
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [isChecking, setIsChecking] = useState(true);

  // Ativar heartbeat para manter servidor vivo
  useHeartbeat();

  useEffect(() => {
    // Check if token exists in localStorage
    const token = localStorage.getItem("auth_token");
    if (token) {
      apiClient.setToken(token);
      // JWT expirado → forçar novo login
      if (apiClient.isTokenExpired()) {
        apiClient.clearToken();
      } else {
        setIsAuthenticated(true);
      }
    }
    setIsChecking(false);

    // Evento disparado pelo APIClient quando refresh falha (sessão expirada)
    const onExpired = () => {
      setIsAuthenticated(false);
    };
    window.addEventListener("jwt-expired", onExpired);
    return () => window.removeEventListener("jwt-expired", onExpired);
  }, []);

  const handleLogin = () => {
    setIsAuthenticated(true);
  };

  const handleLogout = () => {
    apiClient.clearToken();
    setIsAuthenticated(false);
  };

  if (isChecking) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-muted-foreground">Loading...</div>
      </div>
    );
  }

  return (
    <ThemeProvider defaultTheme="system" storageKey="k8s-hpa-theme">
      <QueryClientProvider client={queryClient}>
        <TabProvider>
          <StagingProvider>
            <TooltipProvider>
              <Toaster />
              <Sonner />
              <BrowserRouter>
              <Routes>
              <Route
                path="/login"
                element={
                  isAuthenticated ? (
                    <Navigate to="/" replace />
                  ) : (
                    <Login onLogin={handleLogin} />
                  )
                }
              />
              <Route
                path="/"
                element={
                  isAuthenticated ? (
                    <Index onLogout={handleLogout} />
                  ) : (
                    <Navigate to="/login" replace />
                  )
                }
              />
              {/* Rota para página de alertas de cluster */}
              <Route
                path="/alerts/:cluster"
                element={
                  isAuthenticated ? (
                    <AlertsPage />
                  ) : (
                    <Navigate to="/login" replace />
                  )
                }
              />
              {/* Rota para página de alertas específicos de HPA */}
              <Route
                path="/alerts/:cluster/:namespace/:hpaName"
                element={
                  isAuthenticated ? (
                    <AlertsPage />
                  ) : (
                    <Navigate to="/login" replace />
                  )
                }
              />
              {/* Rota para análise de AI em página dedicada */}
              <Route
                path="/ai-analysis/:id"
                element={
                  isAuthenticated ? (
                    <AIAnalysisPage />
                  ) : (
                    <Navigate to="/login" replace />
                  )
                }
              />
              {/* ADD ALL CUSTOM ROUTES ABOVE THE CATCH-ALL "*" ROUTE */}
              <Route path="*" element={<NotFound />} />
              </Routes>
            </BrowserRouter>
          </TooltipProvider>
        </StagingProvider>
        </TabProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
};

export default App;
