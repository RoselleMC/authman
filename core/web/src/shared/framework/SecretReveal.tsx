import { useState, type ReactNode } from "react";
import { Alert } from "../components/Alert";
import { Button } from "../components/Button";
import { useI18n } from "../i18n/I18nProvider";

interface Props {
  value: string;
  warning?: ReactNode;
  copyLabel?: string;
  copiedLabel?: string;
  /** Test id on the .secret-reveal wrapper. */
  testId?: string;
  /** Test id on the inner <code> showing the secret value. Defaults to "secret-value".
   *  Pages that already have a specific test id (e.g. "node-secret") should pass it
   *  here so existing Playwright selectors keep working. */
  valueTestId?: string;
}

/**
 * Standardised "secret shown once" panel used by node-token create/rotate and
 * the admin one-time-link generator. The value is rendered inside a
 * .secret-box with a copy button; a warning Alert below reminds the user
 * that closing the dialog erases the secret.
 *
 * The actual dialog framing and close button live in the calling page so the
 * caller can choose icon tone / wording per flow.
 */
export function SecretReveal({
  value,
  warning,
  copyLabel,
  copiedLabel,
  testId,
  valueTestId = "secret-value",
}: Props) {
  const [copied, setCopied] = useState(false);
  const { t } = useI18n();
  async function copy() {
    try {
      await navigator.clipboard?.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }
  return (
    <div className="secret-reveal" data-testid={testId}>
      <div className="secret-box">
        <code className="mono secret-value" data-testid={valueTestId}>
          {value}
        </code>
        <Button variant={copied ? "primary" : "secondary"} size="sm" icon={copied ? "check" : "copy"} onClick={copy}>
          {copied ? (copiedLabel ?? t("common.copied")) : (copyLabel ?? t("common.copy"))}
        </Button>
      </div>
      <Alert tone="warning">
        {warning ?? (
          <p>{t("admin.nodes.secret.warning")}</p>
        )}
      </Alert>
    </div>
  );
}
