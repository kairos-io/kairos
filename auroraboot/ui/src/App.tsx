import { useEffect, useState } from "react";
import { Routes, Route, Navigate } from "react-router";
import { Layout } from "./components/Layout";
import { Login } from "./pages/Login";
import { Dashboard } from "./pages/Dashboard";
import { Groups } from "./pages/Groups";
import { GroupDetail } from "./pages/GroupDetail";
import { Batches } from "./pages/Batches";
import { BatchDetail } from "./pages/BatchDetail";
import { Nodes } from "./pages/Nodes";
import { NodeDetail } from "./pages/NodeDetail";
import { Artifacts } from "./pages/Artifacts";
import { ArtifactBuilder } from "./pages/ArtifactBuilder";
import { ArtifactDetail } from "./pages/ArtifactDetail";
import { Extensions } from "./pages/Extensions";
import { ExtensionBuilder } from "./pages/ExtensionBuilder";
import { ExtensionDetail } from "./pages/ExtensionDetail";
import { Deployments } from "./pages/Deployments";
import { BMCRegistration } from "./pages/BMCRegistration";
import { Certificates } from "./pages/Certificates";
import { Import } from "./pages/Import";
import { Settings } from "./pages/Settings";
import { clearLegacyToken, hasSession } from "./api/client";

function RequireAuth({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<"checking" | "in" | "out">("checking");
  useEffect(() => {
    let live = true;
    hasSession().then((ok) => {
      if (live) setState(ok ? "in" : "out");
    });
    return () => {
      live = false;
    };
  }, []);
  if (state === "checking") return null;
  if (state === "out") return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export default function App() {
  useEffect(() => {
    clearLegacyToken();
  }, []);

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<Dashboard />} />
        <Route path="groups" element={<Groups />} />
        <Route path="groups/:id" element={<GroupDetail />} />
        <Route path="batches" element={<Batches />} />
        <Route path="batches/:id" element={<BatchDetail />} />
        <Route path="nodes" element={<Nodes />} />
        <Route path="nodes/:id" element={<NodeDetail />} />
        <Route path="artifacts" element={<Artifacts />} />
        <Route path="artifacts/new" element={<ArtifactBuilder />} />
        <Route path="artifacts/:id" element={<ArtifactDetail />} />
        <Route path="bmc" element={<BMCRegistration />} />
        <Route path="extensions" element={<Extensions />} />
        <Route path="extensions/new" element={<ExtensionBuilder />} />
        <Route path="extensions/:id" element={<ExtensionDetail />} />
        <Route path="deployments" element={<Deployments />} />
        <Route path="certificates" element={<Certificates />} />
        <Route path="import" element={<Import />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  );
}
