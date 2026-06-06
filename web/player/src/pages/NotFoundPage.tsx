import { useNavigate } from "react-router-dom";
import { Button, EmptyState, useI18n } from "@authman/shared";

export function NotFoundPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
      <EmptyState
        icon="alert"
        title="404"
        description={t("common.notFound")}
        action={
          <Button variant="primary" onClick={() => navigate("/")}>
            {t("common.continue")}
          </Button>
        }
        testId="not-found"
      />
    </div>
  );
}
