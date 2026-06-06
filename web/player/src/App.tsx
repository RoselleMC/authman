import { Navigate, Route, Routes } from "react-router-dom";
import { ServerContextProvider } from "./server-context/ServerContextProvider";
import { PlayerShell } from "./layout/PlayerShell";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { LinkPage } from "./pages/LinkPage";
import { AccountPage } from "./pages/AccountPage";
import { SecurityPage } from "./pages/SecurityPage";
import { ServersPage } from "./pages/ServersPage";
import { ExtensionsPage } from "./pages/ExtensionsPage";
import { LandingPage } from "./pages/LandingPage";
import { ServerLandingPage } from "./pages/ServerLandingPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { LoadingScreen } from "./components/LoadingScreen";
import { useSession } from "./auth/SessionContext";
import { useServerContext } from "./server-context/ServerContextProvider";

function GlobalLayout() {
  return (
    <ServerContextProvider>
      <PlayerShell />
    </ServerContextProvider>
  );
}

function ServerLayout() {
  return (
    <ServerContextProvider>
      <PlayerShell />
    </ServerContextProvider>
  );
}

function RequireAuth({ children }: { children: JSX.Element }) {
  const { me, resolved } = useSession();
  const { slug } = useServerContext();
  if (!resolved) return <LoadingScreen />;
  if (!me) {
    return <Navigate to={slug ? `/server/${slug}/login` : "/login"} replace />;
  }
  return children;
}

export function App() {
  return (
    <Routes>
      <Route element={<GlobalLayout />}>
        <Route index element={<LandingPage />} />
        <Route path="login" element={<LoginPage />} />
        <Route path="register" element={<RegisterPage />} />
        <Route path="link" element={<LinkPage />} />
        <Route
          path="account"
          element={
            <RequireAuth>
              <AccountPage />
            </RequireAuth>
          }
        />
        <Route
          path="security"
          element={
            <RequireAuth>
              <SecurityPage />
            </RequireAuth>
          }
        />
        <Route path="servers" element={<ServersPage />} />
        <Route
          path="extensions"
          element={
            <RequireAuth>
              <ExtensionsPage />
            </RequireAuth>
          }
        />
      </Route>
      <Route path="server/:slug" element={<ServerLayout />}>
        <Route index element={<ServerLandingPage />} />
        <Route path="login" element={<LoginPage />} />
        <Route path="register" element={<RegisterPage />} />
        <Route path="link" element={<LinkPage />} />
        <Route
          path="account"
          element={
            <RequireAuth>
              <AccountPage />
            </RequireAuth>
          }
        />
        <Route
          path="security"
          element={
            <RequireAuth>
              <SecurityPage />
            </RequireAuth>
          }
        />
        <Route
          path="extensions"
          element={
            <RequireAuth>
              <ExtensionsPage />
            </RequireAuth>
          }
        />
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
