import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { login, type LoginResult } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { KairosLogo } from "@/components/KairosLogo";
import { applyTheme, getStoredTheme } from "@/lib/theme";
import { PRODUCT_NAME } from "@/lib/branding";

function loginErrorMessage(result: LoginResult | null): string {
  if (result && !result.ok) {
    if (result.status === 401) return "Invalid password";
    if (result.status === 403) {
      return "Sign-in refused: this page's address does not match the server's public URL (--url / AURORABOOT_URL).";
    }
  }
  return "Sign-in failed";
}

export function Login() {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  // Match the theme the operator chose in the app shell.
  useEffect(() => {
    applyTheme(getStoredTheme());
  }, []);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!password.trim()) return;

    setError("");
    setLoading(true);

    let result: LoginResult | null;
    try {
      result = await login(password);
    } catch {
      result = null;
    }
    if (result?.ok) {
      navigate("/");
    } else {
      setError(loginErrorMessage(result));
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-navy to-navy">
      <div className="w-full max-w-sm space-y-6">
        <div className="flex flex-col items-center gap-2">
          <KairosLogo className="h-12 w-12" />
          <h1 className="text-2xl font-bold text-white">{PRODUCT_NAME}</h1>
        </div>
        <Card className="w-full">
          <CardHeader className="text-center">
            <CardTitle className="text-lg">Sign In</CardTitle>
            <CardDescription>Enter your admin password to continue</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="grid gap-4">
              <div className="grid gap-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  placeholder="Admin password"
                  value={password}
                  onChange={(e) => {
                    setPassword(e.target.value);
                    setError("");
                  }}
                  autoFocus
                />
                {error && (
                  <div className="rounded-md bg-red-50 border border-red-200 px-3 py-2 text-sm text-red-700">
                    {error}
                  </div>
                )}
              </div>
              <Button
                type="submit"
                className="w-full"
                disabled={loading}
              >
                {loading ? "Signing in..." : "Sign In"}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
