import { Card, PageHeader, PageShell, useI18n } from "@authman/shared";
import { AuditEventList } from "../components/AuditEventList";

export function AuditPage() {
  const { t } = useI18n();
  return (
    <PageShell>
      <PageHeader title={t("admin.audit.heading")} desc={t("admin.audit.desc")} />
      <Card noBody className="table-card">
        <AuditEventList filterable testId="audit" urlPrefix="a" />
      </Card>
    </PageShell>
  );
}
