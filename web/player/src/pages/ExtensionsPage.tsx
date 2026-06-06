import { useQuery } from "@tanstack/react-query";
import { Card, EmptyState, SchemaRenderer, useI18n } from "@authman/shared";
import { portalExtensionData } from "../api/portal";
import { useServerContext } from "../server-context/ServerContextProvider";
import { ErrorBlock } from "../components/ErrorBlock";

export function ExtensionsPage() {
  const { t } = useI18n();
  const { slug } = useServerContext();
  const q = useQuery({
    queryKey: ["portal.extension-data", slug ?? "_global"],
    queryFn: () => portalExtensionData(slug ?? undefined),
  });

  return (
    <div className="pcontent">
      <div className="pcontent-head">
        <h1>{t("extensions.heading")}</h1>
        <p>{slug ? t("extensions.desc.server") : t("extensions.desc.global")}</p>
      </div>
      {q.error ? <ErrorBlock error={q.error} onRetry={() => q.refetch()} /> : null}
      {q.isLoading ? (
        <Card>{t("common.loading")}</Card>
      ) : (q.data ?? []).length === 0 ? (
        <Card testId="extensions-card">
          <EmptyState icon="box" title={t("extensions.empty")} testId="extensions-empty" />
        </Card>
      ) : (
        <div className="ext-grid" data-testid="extensions-card">
          {(q.data ?? []).map((d, idx) => (
            <Card
              key={`${d.server_slug ?? "global"}-${d.provider}-${idx}`}
              title={d.schema.title}
              actions={<span className="ext-provider mono">{d.provider}</span>}
              noBody
              testId={`extension-${d.server_slug ?? "global"}-${d.provider}`}
            >
              <SchemaRenderer data={d} />
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
