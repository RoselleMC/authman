import { useI18n } from "@authman/shared";

export function LoadingScreen() {
  const { t } = useI18n();
  return (
    <div
      data-testid="loading-screen"
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: "var(--color-text-muted)",
      }}
    >
      {t("common.loading")}
    </div>
  );
}
