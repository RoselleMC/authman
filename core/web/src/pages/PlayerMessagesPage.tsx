import { useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  ErrorState,
  MinecraftDialogPreview,
  MinecraftHudPreview,
  MinecraftKickPreview,
  PageHeader,
  PageShell,
  Skeleton,
  navigateWithBack,
  useI18n,
} from "@authman/shared";
import { fetchPlayerMessages, type PlayerMessageDialogScreen, type PlayerMessagesData } from "../api/admin";
import {
  GATE_KICK_KEYS,
  LIMBO_KICK_KEYS,
  CHAT_MESSAGE_KEYS,
  DIALOG_ERROR_KEYS,
  PROFILE_ERROR_KEYS,
  SUCCESS_KEYS,
  applySample,
  dialogToPreview,
  effectiveMessage,
  playerMessagesQueryKey,
  sampleVars,
} from "../components/playerMessages";

function hasMessageOverride(data: PlayerMessagesData, keys: readonly string[]): boolean {
  return keys.some((key) => (data.messages.overrides[key] ?? "").trim() !== "");
}

interface SceneCardProps {
  title: string;
  desc: string;
  overridden: boolean;
  to: string;
  preview: React.ReactNode;
  testId: string;
}

function SceneCard({ title, desc, overridden, to, preview, testId }: SceneCardProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <button type="button" className="pm-card" onClick={() => navigateWithBack(navigate, to, location)} data-testid={testId}>
      <div className="pm-card__preview">{preview}</div>
      <div className="pm-card__meta">
        <div className="pm-card__meta-main">
          <span className="pm-card__title">{title}</span>
          <span className="pm-card__desc">{desc}</span>
        </div>
        {overridden
          ? <Badge tone="info">{t("admin.playerMessages.overridden")}</Badge>
          : <Badge tone="neutral">{t("admin.playerMessages.default")}</Badge>}
      </div>
    </button>
  );
}

interface PlayerMessagesPageProps {
  embedded?: boolean;
  serverId?: string;
  basePath?: string;
}

function trimPath(value: string): string {
  return value.replace(/\/+$/, "");
}

export function PlayerMessagesPage({ embedded = false, serverId, basePath = "/login-portals/messages" }: PlayerMessagesPageProps = {}) {
  const { t } = useI18n();
  const q = useQuery({ queryKey: playerMessagesQueryKey(serverId), queryFn: () => fetchPlayerMessages(serverId) });
  const vars = useMemo(() => sampleVars(t), [t]);
  const messagesBasePath = trimPath(basePath);

  if (q.error) {
    const body = <ErrorState error={q.error} onRetry={() => q.refetch()} />;
    return embedded ? body : <PageShell>{body}</PageShell>;
  }
  if (!q.data) {
    const body = <Skeleton height={240} />;
    return embedded ? body : <PageShell>{body}</PageShell>;
  }
  const data = q.data;

  const dialogCard = (screen: PlayerMessageDialogScreen) => {
    const doc = data.dialogs[screen].override ?? data.dialogs[screen].default;
    const model = dialogToPreview(doc, screen, { branch: "offline", hasError: false }, vars, "");
    return (
      <SceneCard
        key={screen}
        title={t(`admin.playerMessages.scene.${screen}`)}
        desc={t(`admin.playerMessages.scene.${screen}.desc`)}
        overridden={Boolean(data.dialogs[screen].override)}
        to={`${messagesBasePath}/dialogs/${screen}`}
        testId={`pm-card-dialog-${screen}`}
        preview={(
          <MinecraftDialogPreview
            compact
            title={model.title}
            body={model.body}
            inputs={model.inputs}
            buttons={model.buttons}
            columns={model.columns}
          />
        )}
      />
    );
  };

  const content = (
    <>
      {embedded ? null : <PageHeader title={t("admin.playerMessages.heading")} desc={t("admin.playerMessages.desc")} />}
      <p className="section-copy" style={{ marginTop: 0 }}>{t("admin.playerMessages.help")}</p>
      <div className="pm-cards" data-testid="pm-cards">
        {dialogCard("login")}
        {dialogCard("register")}
        {dialogCard("profile_create")}
        {dialogCard("profile_select")}
        <SceneCard
          title={t("admin.playerMessages.scene.errors")}
          desc={t("admin.playerMessages.scene.errors.desc")}
          overridden={hasMessageOverride(data, [...DIALOG_ERROR_KEYS, ...CHAT_MESSAGE_KEYS, ...PROFILE_ERROR_KEYS, ...LIMBO_KICK_KEYS])}
          to={`${messagesBasePath}/scenes/errors`}
          testId="pm-card-errors"
          preview={<MinecraftKickPreview compact value={applySample(effectiveMessage(data, data.messages.overrides, "limbo.kick.client_too_old"), vars)} />}
        />
        <SceneCard
          title={t("admin.playerMessages.scene.success")}
          desc={t("admin.playerMessages.scene.success.desc")}
          overridden={hasMessageOverride(data, SUCCESS_KEYS)}
          to={`${messagesBasePath}/scenes/success`}
          testId="pm-card-success"
          preview={(
            <MinecraftHudPreview
              compact
              title={applySample(effectiveMessage(data, data.messages.overrides, "limbo.success.title"), vars)}
              subtitle={applySample(effectiveMessage(data, data.messages.overrides, "limbo.success.subtitle"), vars)}
              actionbar={applySample(effectiveMessage(data, data.messages.overrides, "limbo.success.actionbar"), vars)}
            />
          )}
        />
        <SceneCard
          title={t("admin.playerMessages.scene.gate")}
          desc={t("admin.playerMessages.scene.gate.desc")}
          overridden={hasMessageOverride(data, GATE_KICK_KEYS)}
          to={`${messagesBasePath}/scenes/gate`}
          testId="pm-card-gate"
          preview={<MinecraftKickPreview compact value={applySample(effectiveMessage(data, data.messages.overrides, "gate.kick.banned"), vars)} />}
        />
      </div>
    </>
  );

  return embedded ? content : <PageShell testId="player-messages-page">{content}</PageShell>;
}
