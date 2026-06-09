import { cx } from "../utils/cx";
import { previewPassword } from "../validation/password";
import { useI18n } from "../i18n/I18nProvider";

export type StrengthTone = "danger" | "warning" | "success";

interface Props {
  password: string;
  /** When false, render nothing. Pass `false` while the input is empty. */
  visible?: boolean;
}

/**
 * 4-segment strength bar with a textual label. The score is derived from the
 * shared previewPassword helper so admin and player pages share rules.
 */
export function StrengthMeter({ password, visible = true }: Props) {
  const { t } = useI18n();
  if (!visible || password.length === 0) return null;
  const preview = previewPassword(password);
  const score = Math.max(0, Math.min(4, 4 - preview.hints.length));
  const tone: StrengthTone = score <= 1 ? "danger" : score === 2 ? "warning" : "success";
  const label = tone === "danger" ? t("common.weak") : tone === "warning" ? t("common.ok") : t("common.strong");
  return (
    <div className="strength">
      <div className="strength-bars">
        {[0, 1, 2, 3].map((i) => (
          <span key={i} className={cx("strength-bar", i < score && `is-on strength--${tone}`)} />
        ))}
      </div>
      <span className={cx("strength-label", `strength-label--${tone}`)}>{label}</span>
    </div>
  );
}
