import { useState } from "react";
import { Button } from "./Button";
import { useI18n } from "../i18n/I18nProvider";

interface Props {
  value: string;
  label?: string;
  size?: "sm" | "md";
  variant?: "primary" | "secondary" | "ghost";
}

export function CopyButton({ value, label, size = "sm", variant = "secondary" }: Props) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard?.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return (
    <Button
      type="button"
      onClick={handleCopy}
      size={size}
      variant={copied ? "primary" : variant}
      icon={copied ? "check" : "copy"}
      data-testid="copy-button"
      aria-label={label ?? t("common.copy")}
    >
      {copied ? t("common.copied") : label ?? t("common.copy")}
    </Button>
  );
}
