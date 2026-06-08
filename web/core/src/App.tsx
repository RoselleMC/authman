import { Navigate, Route, Routes, useParams } from "react-router-dom";
import { useSession } from "./auth/SessionContext";
import { CoreShell } from "./layout/CoreShell";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { PassportsPage } from "./pages/PassportsPage";
import { PassportDetailPage } from "./pages/PassportDetailPage";
import { ProfilesPage } from "./pages/ProfilesPage";
import { ProfileDetailPage } from "./pages/ProfileDetailPage";
import { NodesPage } from "./pages/NodesPage";
import { NodeDetailPage } from "./pages/NodeDetailPage";
import { ProxyPoolPage } from "./pages/MojangPage";
import { PortalSettingsPage } from "./pages/PortalSettingsPage";
import { AuditPage } from "./pages/AuditPage";
import { AuditDetailPage } from "./pages/AuditDetailPage";
import { SettingsPage } from "./pages/SettingsPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { LoadingScreen } from "./components/LoadingScreen";

function RequireAuth({ children }: { children: JSX.Element }) {
  const { user, resolved } = useSession();
  if (!resolved) return <LoadingScreen />;
  if (!user) return <Navigate to="/login" replace />;
  return children;
}

function LegacyPlayerRedirect() {
  const { id = "" } = useParams<{ id: string }>();
  return <Navigate to={`/profiles/${encodeURIComponent(id)}`} replace />;
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <RequireAuth>
            <CoreShell />
          </RequireAuth>
        }
      >
        <Route index element={<OverviewPage />} />
        <Route path="/passports" element={<PassportsPage />} />
        <Route path="/passports/:id" element={<PassportDetailPage />} />
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="/profiles/:id" element={<ProfileDetailPage />} />
        <Route path="/players" element={<Navigate to="/passports" replace />} />
        <Route path="/players/:id" element={<LegacyPlayerRedirect />} />
        <Route path="/nodes" element={<NodesPage />} />
        <Route path="/nodes/:id" element={<NodeDetailPage />} />
        <Route path="/portal" element={<PortalSettingsPage />} />
        <Route path="/proxies" element={<ProxyPoolPage />} />
        <Route path="/mojang" element={<Navigate to="/settings/mojang" replace />} />
        <Route path="/audit" element={<AuditPage />} />
        <Route path="/audit/:id" element={<AuditDetailPage />} />
        <Route path="/settings" element={<Navigate to="/settings/account" replace />} />
        <Route path="/settings/:section" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
