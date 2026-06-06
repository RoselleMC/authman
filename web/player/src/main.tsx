import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider, ThemeProvider, ToastProvider, getRuntimeConfig } from "@authman/shared";
import "@authman/shared/tokens.css";
import "@authman/shared/components.css";
import "@authman/shared/layout.css";
import "@authman/shared/animations.css";
import { App } from "./App";
import { SessionProvider } from "./auth/SessionContext";

const cfg = getRuntimeConfig();
document.title = "Authman Player Portal";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
});

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("root element missing");

// StrictMode is intentionally not used here: the link-login flow performs a
// one-shot read-and-clear of the URL fragment in useEffect, which StrictMode's
// dev double-mount would race. Production builds are unaffected (StrictMode is
// transparent in production), and other dev-time checks are covered by
// React Query's devtools and TypeScript strict mode.
ReactDOM.createRoot(rootEl).render(
  <QueryClientProvider client={queryClient}>
    <I18nProvider defaultLocale={cfg.defaultLocale}>
      <ThemeProvider>
        <ToastProvider>
          <BrowserRouter>
            <SessionProvider>
              <App />
            </SessionProvider>
          </BrowserRouter>
        </ToastProvider>
      </ThemeProvider>
    </I18nProvider>
  </QueryClientProvider>,
);
