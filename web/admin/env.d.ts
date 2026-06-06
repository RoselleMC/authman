/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_AUTHMAN_API_BASE?: string;
  readonly VITE_AUTHMAN_APP_KIND?: string;
  readonly VITE_AUTHMAN_DEFAULT_LOCALE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
