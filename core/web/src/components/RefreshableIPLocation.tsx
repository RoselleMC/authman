import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { IPLocation, useI18n, useToast, type IPGeo } from "@authman/shared";
import { refreshIPGeo } from "../api/admin";

interface Props {
  ip?: string | null;
  geo?: IPGeo | null;
  compact?: boolean;
}

export function RefreshableIPLocation({ ip, geo, compact = false }: Props) {
  const { t } = useI18n();
  const toast = useToast();
  const queryClient = useQueryClient();
  const displayIP = (ip || geo?.ip || "").trim();
  const queryKey = ["admin.ipGeo.display", displayIP] as const;
  const display = useQuery<IPGeo | null>({
    queryKey,
    queryFn: async () => geo ?? null,
    initialData: geo ?? null,
    enabled: false,
    staleTime: Infinity,
  });
  const refresh = useMutation({
    mutationFn: () => refreshIPGeo(displayIP),
    onSuccess: (result) => {
      if (!result.geo) {
        toast.push({ tone: "danger", title: t("geo.refresh.failed"), msg: t("geo.refresh.kept") });
        return;
      }
      queryClient.setQueryData(queryKey, result.geo);
      toast.push({ tone: "success", title: t("geo.refresh.success"), msg: displayIP });
    },
    onError: () => {
      toast.push({ tone: "danger", title: t("geo.refresh.failed"), msg: t("geo.refresh.kept") });
    },
  });

  return (
    <IPLocation
      ip={ip}
      geo={display.data ?? geo}
      compact={compact}
      onRefresh={displayIP ? () => refresh.mutate() : undefined}
      refreshing={refresh.isPending}
      refreshLabel={t("geo.refresh.action")}
    />
  );
}
