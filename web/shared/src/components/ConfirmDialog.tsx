import type { ReactNode } from "react";
import { Dialog, type DialogIconTone } from "./Dialog";
import { Button } from "./Button";
import { useI18n } from "../i18n/I18nProvider";
import type { IconName } from "./Icon";

interface Props {
  open: boolean;
  title: ReactNode;
  body?: ReactNode;
  confirmLabel?: ReactNode;
  cancelLabel?: ReactNode;
  destructive?: boolean;
  loading?: boolean;
  icon?: IconName | string;
  iconTone?: DialogIconTone;
  onConfirm: () => void;
  onCancel: () => void;
  testId?: string;
}

export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  cancelLabel,
  destructive,
  loading,
  icon,
  iconTone,
  onConfirm,
  onCancel,
  testId,
}: Props) {
  const { t } = useI18n();
  return (
    <Dialog
      open={open}
      onClose={() => {
        if (!loading) onCancel();
      }}
      icon={icon ?? (destructive ? "alert" : "info")}
      iconTone={iconTone ?? (destructive ? "danger" : "primary")}
      title={title}
      desc={typeof body === "string" ? body : undefined}
      testId={testId}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel} disabled={loading} data-testid="confirm-cancel">
            {cancelLabel ?? t("common.cancel")}
          </Button>
          <Button
            variant={destructive ? "danger" : "primary"}
            onClick={onConfirm}
            loading={loading}
            data-testid="confirm-confirm"
          >
            {confirmLabel ?? t("common.confirm")}
          </Button>
        </>
      }
    >
      {typeof body !== "string" ? body : null}
    </Dialog>
  );
}
