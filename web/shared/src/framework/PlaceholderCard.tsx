import { Icon, type IconName } from "../components/Icon";
import { useI18n } from "../i18n/I18nProvider";

interface Props {
  icon: IconName | string;
  title: string;
  desc: string;
  badge?: string;
  testId?: string;
}

/**
 * Dashed "coming soon" card used for backend-driven features that are wired
 * but not yet implemented in the API (SMTP, 2FA). Always use this — never
 * hand-roll a placeholder card — so the dashed border and badge stay
 * consistent.
 */
export function PlaceholderCard({ icon, title, desc, badge, testId }: Props) {
  const { t } = useI18n();
  return (
    <div className="placeholder-card" data-testid={testId}>
      <div className="placeholder-ico">
        <Icon name={icon} size={18} />
      </div>
      <div style={{ flex: 1 }}>
        <div className="placeholder-title">{title}</div>
        <p className="placeholder-desc">{desc}</p>
      </div>
      <span className="placeholder-badge">{badge ?? t("admin.settings.comingSoon")}</span>
    </div>
  );
}

export function PlaceholderGrid({ children }: { children: React.ReactNode }) {
  return <div className="placeholder-grid">{children}</div>;
}

export function SettingsStack({ children }: { children: React.ReactNode }) {
  return <div className="settings-stack">{children}</div>;
}
