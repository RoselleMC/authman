import type { ExtensionData, ExtensionField, ExtensionTone } from "./types";
import { Badge } from "../components/Badge";
import { Icon } from "../components/Icon";
import { useI18n } from "../i18n/I18nProvider";

interface Props {
  data: ExtensionData;
  /** Admin view: shows admin-only fields too. */
  admin?: boolean;
  testId?: string;
}

const SAFE_TONES: ReadonlyArray<ExtensionTone> = ["neutral", "success", "warning", "danger", "info"];

function safeTone(t: unknown): ExtensionTone {
  if (typeof t === "string" && (SAFE_TONES as readonly string[]).includes(t)) {
    return t as ExtensionTone;
  }
  return "neutral";
}

function formatNumber(value: unknown, format?: string): string {
  if (typeof value !== "number" || Number.isNaN(value)) return String(value ?? "");
  if (format === "integer") return Math.trunc(value).toLocaleString();
  if (format === "percent") return `${(value * 100).toFixed(1)}%`;
  return value.toLocaleString();
}

function formatTime(value: unknown): { rel: string; abs: string } {
  if (typeof value !== "string" && typeof value !== "number") return { rel: "", abs: "" };
  try {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return { rel: String(value), abs: "" };
    return { rel: d.toLocaleDateString(), abs: d.toLocaleTimeString() };
  } catch {
    return { rel: String(value), abs: "" };
  }
}

export function SchemaRenderer({ data, admin, testId }: Props) {
  const { t } = useI18n();
  const fields = data.schema.fields.filter((f) => admin || !f.visibility || f.visibility === "player_visible" || f.visibility === "public");

  return (
    <div data-testid={testId ?? "schema-renderer"} className="ext-fields">
      {fields.map((field) => (
        <div className="ext-field" key={field.key}>
          <div className="ext-flabel" data-testid={`ext-label-${field.key}`}>
            <span>{t(`extensions.field.${field.key}`, field.label)}</span>
            {admin && field.visibility === "admin_only" ? (
              <span className="ext-adminonly">
                <Icon name="lock" size={10} /> {t("admin.extensions.visibility.adminOnly")}
              </span>
            ) : null}
          </div>
          <div className="ext-fvalue" data-testid={`ext-value-${field.key}`}>
            <FieldValue
              field={field}
              value={data.values[field.key]}
              unsafeLinkLabel={t("extensions.unsafeLink")}
              yesLabel={t("common.yes")}
              noLabel={t("common.no")}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

interface FieldValueProps {
  field: ExtensionField;
  value: unknown;
  unsafeLinkLabel: string;
  yesLabel: string;
  noLabel: string;
}

export function FieldValue({ field, value, unsafeLinkLabel, yesLabel, noLabel }: FieldValueProps) {
  if (value === null || value === undefined || value === "") {
    return <span className="ext-empty">—</span>;
  }
  switch (field.type) {
    case "text":
      return <span>{String(value)}</span>;
    case "number":
      return <span className="ext-number mono">{formatNumber(value, field.format)}</span>;
    case "boolean":
      return value ? <Badge tone="success" dot>{yesLabel}</Badge> : <Badge tone="neutral">{noLabel}</Badge>;
    case "time": {
      const { rel, abs } = formatTime(value);
      return (
        <span className="ext-time">
          {rel}
          {abs ? <span className="ext-time-abs"> · {abs}</span> : null}
        </span>
      );
    }
    case "badge":
      return <Badge tone={safeTone(field.tone)}>{String(value)}</Badge>;
    case "list":
      if (!Array.isArray(value)) return <span className="ext-empty">{String(value ?? "")}</span>;
      return (
        <div className="ext-list">
          {value.slice(0, 6).map((item, idx) => (
            <span className="ext-tag" key={idx}>
              {typeof item === "string" || typeof item === "number" ? String(item) : JSON.stringify(item)}
            </span>
          ))}
        </div>
      );
    case "link": {
      const url = typeof value === "string" ? value : "";
      if (!field.safe || !url) {
        return <span className="ext-unknown">{unsafeLinkLabel}</span>;
      }
      try {
        const parsed = new URL(url);
        if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
          return <span className="ext-unknown">{unsafeLinkLabel}</span>;
        }
        return (
          <a href={parsed.toString()} target="_blank" rel="noopener noreferrer" className="ext-link">
            {parsed.host}
            <Icon name="external" size={12} />
          </a>
        );
      } catch {
        return <span className="ext-unknown">{unsafeLinkLabel}</span>;
      }
    }
    default:
      return <span className="ext-unknown">{String(value)}</span>;
  }
}
