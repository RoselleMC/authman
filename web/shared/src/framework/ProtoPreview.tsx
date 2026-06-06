import type { ReactNode } from "react";
import { useI18n } from "../i18n/I18nProvider";

interface Props {
  rawName: string;
  label?: ReactNode;
  note?: ReactNode;
}

/**
 * The green proto-preview card shown under the offline username field during
 * registration. Always renders a placeholder when the name is empty, so the
 * height does not change as the user types.
 */
export function ProtoPreview({ rawName, label, note }: Props) {
  const { t } = useI18n();
  return (
    <div className="proto-preview">
      <span className="proto-preview-label">{label ?? t("account.protocolName")}</span>
      <div className="proto-preview-value">
        {rawName ? (
          <span className="protoname" style={{ fontSize: 20 }}>
            <span className="proto-hash">#</span>
            <span>{rawName}</span>
          </span>
        ) : (
          <span className="proto-preview-placeholder">#…</span>
        )}
      </div>
      {note ? <p className="proto-preview-note">{note}</p> : null}
    </div>
  );
}
