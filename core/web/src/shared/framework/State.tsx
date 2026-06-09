import type { ReactNode } from "react";
import { Alert } from "../components/Alert";
import { Button } from "../components/Button";
import { ApiError } from "../api/envelope";
import { useI18n } from "../i18n/I18nProvider";

/**
 * Standard "request in flight" state for an entire page region. Use inside
 * a Card body (or as the only child) while waiting on a primary fetch.
 */
export function LoadingState({ label }: { label?: ReactNode }) {
  const { t } = useI18n();
  return (
    <div data-testid="loading-state" style={{ padding: "var(--space-5)", color: "var(--color-text-muted)" }}>
      {label ?? t("common.loading")}
    </div>
  );
}

interface ErrorStateProps {
  error: unknown;
  onRetry?: () => void;
  testId?: string;
}

/**
 * Standard error block that maps backend error.code to a localized message
 * and offers a retry action when one is provided. Always prefer this over
 * hand-rolled error markup so user-facing copy stays consistent.
 */
export function ErrorState({ error, onRetry, testId = "error-block" }: ErrorStateProps) {
  const { t, tError } = useI18n();
  let message: string = t("common.unknown");
  if (error instanceof ApiError) {
    message = tError(error.code);
  } else if (error instanceof Error) {
    message = error.message || message;
  }
  return (
    <div style={{ marginBottom: 16 }}>
      <Alert tone="danger" testId={testId}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
          <span>{message}</span>
          {onRetry ? (
            <Button size="sm" variant="ghost" onClick={onRetry} icon="refresh" data-testid="error-retry">
              {t("common.retry")}
            </Button>
          ) : null}
        </div>
      </Alert>
    </div>
  );
}
