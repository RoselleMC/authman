import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { Icon } from "./Icon";
import { IconButton } from "./IconButton";
import { cx } from "../utils/cx";
import { useI18n } from "../i18n/I18nProvider";

export type ToastTone = "info" | "success" | "warning" | "danger";

interface Toast {
  id: number;
  tone: ToastTone;
  title: string;
  msg?: string;
}

interface PushArgs {
  tone?: ToastTone;
  title: string;
  msg?: string;
  duration?: number;
}

interface ToastContextValue {
  push: (args: PushArgs) => void;
  /** Backwards-compat: tone shorthand methods. */
  show: (tone: ToastTone, message: string) => void;
  info: (message: string) => void;
  success: (message: string) => void;
  warning: (message: string) => void;
  danger: (message: string) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

let counter = 0;

const TONE_ICON = { info: "info", success: "check", warning: "alert", danger: "alert" } as const;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const { t } = useI18n();

  const dismiss = useCallback((id: number) => {
    setToasts((list) => list.filter((t) => t.id !== id));
  }, []);

  const push = useCallback(
    (args: PushArgs) => {
      counter += 1;
      const id = counter;
      const tone = args.tone ?? "info";
      setToasts((list) => [...list, { id, tone, title: args.title, msg: args.msg }]);
      window.setTimeout(() => dismiss(id), args.duration ?? 3600);
    },
    [dismiss],
  );

  const value = useMemo<ToastContextValue>(
    () => ({
      push,
      show: (tone, message) => push({ tone, title: message }),
      info: (message) => push({ tone: "info", title: message }),
      success: (message) => push({ tone: "success", title: message }),
      warning: (message) => push({ tone: "warning", title: message }),
      danger: (message) => push({ tone: "danger", title: message }),
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-region" aria-live="polite" data-testid="toast-region">
        {toasts.map((toast) => (
          <div key={toast.id} className={cx("toast", toast.tone && `toast--${toast.tone}`)} data-testid={`toast-${toast.tone}`}>
            <Icon
              name={TONE_ICON[toast.tone]}
              size={16}
              style={{ marginTop: 1, color: `var(--color-${toast.tone === "success" ? "success" : toast.tone})` }}
            />
            <div style={{ flex: 1 }}>
              <p className="toast__title">{toast.title}</p>
              {toast.msg ? <p className="toast__msg">{toast.msg}</p> : null}
            </div>
            <IconButton
              name="close"
              size={14}
              label={t("common.dismiss")}
              onClick={() => dismiss(toast.id)}
              style={{ width: 26, height: 26 }}
            />
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used inside ToastProvider");
  return ctx;
}
