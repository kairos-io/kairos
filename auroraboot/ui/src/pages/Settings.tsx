import { useEffect, useState } from "react";
import { getRegistrationToken, rotateRegistrationToken } from "@/api/settings";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/PageHeader";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ExtensionCatalogsCard } from "@/components/settings/ExtensionCatalogsCard";
import { Eye, EyeOff, RefreshCw } from "lucide-react";

export function Settings() {
  const [token, setToken] = useState("");
  const [revealed, setRevealed] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    getRegistrationToken()
      .then((t) => setToken(t.registrationToken))
      .catch(() => {});
  }, []);

  async function handleRotate() {
    setRotating(true);
    try {
      const result = await rotateRegistrationToken();
      setToken(result.registrationToken);
    } finally {
      setRotating(false);
    }
  }

  const maskedToken = token ? token.slice(0, 8) + "..." + token.slice(-4) : "";

  return (
    <div>
      <PageHeader title="Settings" description="Server configuration and tokens" />

      <div className="grid gap-6 max-w-2xl">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">Registration Token</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4">
            <p className="text-sm text-muted-foreground">
              This token is used by nodes to register with the server. Keep it secret.
            </p>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  readOnly
                  value={revealed ? token : maskedToken}
                  className="font-mono pr-10"
                />
                <Button
                  variant="ghost"
                  size="icon"
                  className="absolute right-0 top-0 h-full"
                  onClick={() => setRevealed(!revealed)}
                >
                  {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <Button variant="outline" onClick={() => setConfirmOpen(true)} disabled={rotating}>
                <RefreshCw className={`h-4 w-4 mr-2 ${rotating ? "animate-spin" : ""}`} />
                Rotate
              </Button>
            </div>
          </CardContent>
        </Card>

        <ExtensionCatalogsCard />
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Rotate registration token"
        description="Rotate the registration token? Nodes that have not registered yet must use the new token."
        confirmLabel="Rotate token"
        destructive
        onConfirm={handleRotate}
      />
    </div>
  );
}
