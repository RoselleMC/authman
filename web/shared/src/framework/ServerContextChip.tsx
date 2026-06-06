import type { ReactNode } from "react";
import { Badge } from "../components/Badge";
import { useI18n } from "../i18n/I18nProvider";

interface ServerLike {
  display_name: string;
  description?: string;
  primary_color?: string;
  registration_open?: boolean;
}

interface Props {
  server: ServerLike;
  /** Test ID attached to the chip. Defaults to the test ID Playwright already
   *  scrolls for ("server-context-banner"). */
  testId?: string;
  children?: ReactNode;
}

/**
 * Compact "you are on server X" chip shown inside auth cards in server context
 * routes. Accepts any object with at least display_name + registration_open;
 * a missing primary_color falls back to the brand green so the chip never
 * renders a transparent badge.
 */
export function ServerContextChip({ server, testId = "server-context-banner" }: Props) {
  const { t } = useI18n();
  const primary = server.primary_color ?? "var(--color-primary)";
  const open = server.registration_open === true;
  return (
    <div className="server-ctx-chip" style={{ ["--ctx-color" as string]: primary }} data-testid={testId}>
      <span className="ctx-badge" style={{ background: primary }}>{server.display_name[0]}</span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="ctx-name">{server.display_name}</div>
        {server.description ? <div className="ctx-desc">{server.description}</div> : null}
      </div>
      <Badge tone={open ? "success" : "neutral"} dot>
        {open ? t("common.open") : t("common.closed")}
      </Badge>
    </div>
  );
}
