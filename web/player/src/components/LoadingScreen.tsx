import { useI18n } from "@authman/shared";

export function LoadingScreen() {
  const { t } = useI18n();
  return (
    <div data-testid="loading-screen" style={{ padding: 24, color: "var(--color-text-muted)" }}>
      {t("common.loading")}
    </div>
  );
}
