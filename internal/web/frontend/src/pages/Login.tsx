import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { apiClient } from "@/lib/api/client";
import { Shield, Loader2 } from "lucide-react";

interface LoginProps {
  onLogin: () => void;
}

export const Login = ({ onLogin }: LoginProps) => {
  // jwtMode=true → tenta /auth/login automaticamente
  // jwtMode=false → backend respondeu 501 (JWT não configurado), usa token estático
  const [jwtMode, setJwtMode] = useState(true);
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      if (jwtMode) {
        // Tenta login JWT (Azure CLI no servidor)
        await apiClient.login();
        onLogin();
      } else {
        // Fallback: token estático
        apiClient.setToken(token);
        const response = await fetch("/api/v1/clusters", {
          headers: { Authorization: `Bearer ${token}` },
        });
        if (!response.ok) throw new Error("Token inválido");
        onLogin();
      }
    } catch (err: any) {
      if (err?.status === 501 || err?.code === "JWT_NOT_CONFIGURED") {
        // Backend sem JWT configurado → mudar para modo de token estático
        setJwtMode(false);
        setError("Servidor sem JWT configurado. Insira o token de acesso.");
      } else if (err?.status === 401 || err?.code === "AZ_CLI_ERROR") {
        setError("Azure CLI não autenticado no servidor. Execute `az login` no servidor.");
      } else {
        setError(err?.message || "Falha na autenticação.");
        if (!jwtMode) apiClient.clearToken();
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex items-center justify-center min-h-screen bg-gradient-to-br from-background to-muted">
      <Card className="w-full max-w-md shadow-xl">
        <CardHeader className="space-y-4 text-center">
          <div className="mx-auto w-16 h-16 bg-primary/10 rounded-full flex items-center justify-center">
            <Shield className="w-8 h-8 text-primary" />
          </div>
          <CardTitle className="text-2xl">k8s HPA Manager</CardTitle>
          <CardDescription>
            {jwtMode
              ? "Autenticação via Azure AD"
              : "Token de acesso estático"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleLogin} className="space-y-4">
            {!jwtMode && (
              <div className="space-y-2">
                <label htmlFor="token" className="text-sm font-medium">
                  Token de autenticação
                </label>
                <Input
                  id="token"
                  type="password"
                  placeholder="Insira seu token"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  disabled={loading}
                  className="font-mono"
                />
              </div>
            )}

            {error && (
              <div className="p-3 text-sm text-destructive bg-destructive/10 border border-destructive/20 rounded-md">
                {error}
              </div>
            )}

            <Button type="submit" className="w-full" disabled={loading || (!jwtMode && !token)}>
              {loading ? (
                <><Loader2 className="w-4 h-4 mr-2 animate-spin" /> Autenticando...</>
              ) : jwtMode ? (
                "Entrar com Azure AD"
              ) : (
                "Entrar"
              )}
            </Button>

            {!jwtMode && (
              <Button
                type="button"
                variant="ghost"
                className="w-full text-xs text-muted-foreground"
                onClick={() => { setJwtMode(true); setError(""); }}
              >
                Tentar autenticação Azure AD
              </Button>
            )}
          </form>
        </CardContent>
      </Card>
    </div>
  );
};
