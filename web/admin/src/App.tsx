import { Navigate, Route, Routes } from "react-router-dom";
import { useSession } from "./auth/SessionContext";
import { AdminShell } from "./layout/AdminShell";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { PlayersListPage } from "./pages/PlayersListPage";
import { PlayerDetailPage } from "./pages/PlayerDetailPage";
import { NodesPage } from "./pages/NodesPage";
import { MojangPage } from "./pages/MojangPage";
import { ServersPage } from "./pages/ServersPage";
import { ServerDetailPage } from "./pages/ServerDetailPage";
import { ExtensionsPage } from "./pages/ExtensionsPage";
import { AuditPage } from "./pages/AuditPage";
import { SettingsPage } from "./pages/SettingsPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { LoadingScreen } from "./components/LoadingScreen";

function RequireAuth({ children }: { children: JSX.Element }) {
  const { user, resolved } = useSession();
  if (!resolved) return <LoadingScreen />;
  if (!user) return <Navigate to="/login" replace />;
  return children;
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <RequireAuth>
            <AdminShell />
          </RequireAuth>
        }
      >
        <Route index element={<OverviewPage />} />
        <Route path="/players" element={<PlayersListPage />} />
        <Route path="/players/:id" element={<PlayerDetailPage />} />
        <Route path="/nodes" element={<NodesPage />} />
        <Route path="/mojang" element={<MojangPage />} />
        <Route path="/servers" element={<ServersPage />} />
        <Route path="/servers/:id" element={<ServerDetailPage />} />
        <Route path="/extensions" element={<ExtensionsPage />} />
        <Route path="/audit" element={<AuditPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
