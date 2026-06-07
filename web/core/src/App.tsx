import { Navigate, Route, Routes } from "react-router-dom";
import { useSession } from "./auth/SessionContext";
import { CoreShell } from "./layout/CoreShell";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { PlayersListPage } from "./pages/PlayersListPage";
import { PlayerDetailPage } from "./pages/PlayerDetailPage";
import { NodesPage } from "./pages/NodesPage";
import { MojangPage } from "./pages/MojangPage";
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
            <CoreShell />
          </RequireAuth>
        }
      >
        <Route index element={<OverviewPage />} />
        <Route path="/players" element={<PlayersListPage />} />
        <Route path="/players/:id" element={<PlayerDetailPage />} />
        <Route path="/nodes" element={<NodesPage />} />
        <Route path="/mojang" element={<MojangPage />} />
        <Route path="/audit" element={<AuditPage />} />
        <Route path="/settings" element={<Navigate to="/settings/account" replace />} />
        <Route path="/settings/:section" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
