import { Badge, Field, Input, Select, formatAbsTime, useI18n } from "@authman/shared";
import type { PlayerBan } from "../api/admin";

export type DurationUnit = "s" | "min" | "h" | "d" | "w" | "m" | "y";

export function BanStateCell({ ban }: { ban?: PlayerBan | null }) {
  const { t } = useI18n();
  if (!ban) return <span className="muted-cell">{t("common.none")}</span>;
  const expiry = ban.expires_at ? formatAbsTime(ban.expires_at) : t("admin.bans.permanent");
  return (
    <span className="state-cell">
      <Badge tone="danger" dot>{t("admin.bans.active")}</Badge>
      <span className="muted-cell">{expiry}</span>
    </span>
  );
}

export function LockUntilCell({ lockedUntil }: { lockedUntil?: string | null }) {
  const { t } = useI18n();
  if (!lockedUntil) return <span className="muted-cell">{t("common.none")}</span>;
  return (
    <span className="state-cell">
      <Badge tone="warning" dot>{t("status.locked")}</Badge>
      <span className="muted-cell">{formatAbsTime(lockedUntil)}</span>
    </span>
  );
}

export function BanDurationFields({
  reason,
  value,
  unit,
  onReasonChange,
  onValueChange,
  onUnitChange,
}: {
  reason: string;
  value: string;
  unit: DurationUnit;
  onReasonChange: (next: string) => void;
  onValueChange: (next: string) => void;
  onUnitChange: (next: DurationUnit) => void;
}) {
  const { t } = useI18n();
  return (
    <>
      <Field label={t("admin.bans.reason")}>
        <Input value={reason} onChange={(e) => onReasonChange(e.target.value)} autoFocus />
      </Field>
      <Field label={t("admin.bans.duration")}>
        <div className="duration-input-row">
          <Input type="number" min={1} step={1} value={value} onChange={(e) => onValueChange(e.target.value)} />
          <Select<DurationUnit> value={unit} options={durationUnitOptions(t)} onChange={onUnitChange} ariaLabel={t("admin.bans.duration")} />
        </div>
      </Field>
    </>
  );
}

export function durationSeconds(value: string, unit: DurationUnit) {
  const parsed = Number(value);
  const multipliers: Record<DurationUnit, number> = {
    s: 1,
    min: 60,
    h: 60 * 60,
    d: 24 * 60 * 60,
    w: 7 * 24 * 60 * 60,
    m: 30 * 24 * 60 * 60,
    y: 365 * 24 * 60 * 60,
  };
  return Math.max(1, Math.floor(parsed * multipliers[unit]));
}

export function isValidDuration(value: string) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0;
}

function durationUnitOptions(t: (key: string, fallback?: string) => string) {
  return (["s", "min", "h", "d", "w", "m", "y"] as DurationUnit[]).map((unit) => ({
    value: unit,
    label: t(`admin.bans.unit.${unit}`),
  }));
}
