import { useState } from "react";
import { Icon } from "./Icon";
import { cx } from "../utils/cx";
import { useI18n } from "../i18n/I18nProvider";

interface Props {
  value: string;
  display?: string;
  truncate?: number;
  mono?: boolean;
  testId?: string;
}

export function Copyable({ value, display, truncate, mono = true, testId }: Props) {
  const [copied, setCopied] = useState(false);
  const { t } = useI18n();
  const text = display ?? value;
  const shown = truncate && text.length > truncate ? `${text.slice(0, truncate - 3)}…` : text;

  async function handle(e: React.MouseEvent) {
    e.stopPropagation();
    try {
      await navigator.clipboard?.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      setCopied(false);
    }
  }

  return (
    <button
      type="button"
      onClick={handle}
      className={cx("copyable", copied && "is-copied")}
      title={copied ? t("common.copied") : t("common.copyValue").replace("{value}", value)}
      data-testid={testId}
      style={{ fontFamily: mono ? "var(--font-mono)" : "var(--font-sans)" }}
    >
      <span>{shown}</span>
      <Icon name={copied ? "check" : "copy"} size={13} className="copy-ico" />
    </button>
  );
}
