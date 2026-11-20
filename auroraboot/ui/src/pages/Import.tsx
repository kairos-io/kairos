import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { listGroups, type Group } from "@/api/groups";
import { getRegistrationToken } from "@/api/settings";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/PageHeader";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Copy, Check } from "lucide-react";
import { PRODUCT_NAME } from "@/lib/branding";

export function Import() {
  const [searchParams] = useSearchParams();
  const [groups, setGroups] = useState<Group[]>([]);
  const [selectedGroup, setSelectedGroup] = useState(
    searchParams.get("group") || "__none__"
  );
  const [token, setToken] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    listGroups().then(setGroups).catch(() => {});
    getRegistrationToken()
      .then((t) => setToken(t.registrationToken))
      .catch(() => {});
  }, []);

  const baseUrl = window.location.origin;
  const groupName = selectedGroup && selectedGroup !== "__none__" ? selectedGroup : "";
  const curlCommand = `curl -sfL ${baseUrl}/api/v1/install-agent | AURORABOOT_URL=${baseUrl} REGISTRATION_TOKEN=${token || "<token>"}${groupName ? ` GROUP=${groupName}` : ""} sh`;

  function handleCopy() {
    navigator.clipboard.writeText(curlCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <div>
      <PageHeader title="Import Nodes" description={`Register new machines with ${PRODUCT_NAME}`} />

      <Card className="mb-6">
        <CardHeader>
          <CardTitle className="text-sm font-medium">Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 max-w-md">
            <div className="grid gap-2">
              <Label>Target Group</Label>
              <Select value={selectedGroup} onValueChange={setSelectedGroup}>
                <SelectTrigger>
                  <SelectValue placeholder="Select group (optional)" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">No group</SelectItem>
                  {groups.map((g) => (
                    <SelectItem key={g.id} value={g.id}>
                      {g.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            Install Command
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground mb-4">
            Run this command on each node you want to register with {PRODUCT_NAME}.
          </p>
          <div className="relative">
            <pre className="terminal-output rounded-md p-4 text-sm font-mono overflow-x-auto">
              {curlCommand}
            </pre>
            <Button
              variant="outline"
              size="icon"
              className="absolute top-2 right-2"
              onClick={handleCopy}
            >
              {copied ? (
                <Check className="h-4 w-4" />
              ) : (
                <Copy className="h-4 w-4" />
              )}
            </Button>
          </div>
          <div className="mt-6 space-y-2 text-sm text-muted-foreground">
            <p>
              The install script will download and configure the {PRODUCT_NAME} agent
              on the target node. The agent will register with this server and
              appear in the Nodes list.
            </p>
            <p>
              Make sure the target node can reach{" "}
              <code className="text-foreground">{baseUrl}</code> over the
              network.
            </p>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
