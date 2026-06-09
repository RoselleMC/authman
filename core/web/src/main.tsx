import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider, ToastProvider, I18nProvider, getRuntimeConfig } from "@authman/shared";
import "@authman/shared/tokens.css";
import "@authman/shared/components.css";
import "@authman/shared/layout.css";
import "@authman/shared/admin_sections.css";
import "@authman/shared/animations.css";
import { App } from "./App";
import { SessionProvider } from "./auth/SessionContext";

const cfg = getRuntimeConfig();
document.title = "Authman Core";

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

ReactDOM.createRoot(rootEl).render(
  <React.StrictMode>
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
    </QueryClientProvider>
  </React.StrictMode>,
);
