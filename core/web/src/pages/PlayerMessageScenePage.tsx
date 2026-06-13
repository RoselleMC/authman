import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  ApiError,
  BackLink,
  Badge,
  Button,
  Card,
  ErrorState,
  MinecraftHudPreview,
  PageHeader,
  PageShell,
  Skeleton,
  useBackTarget,
  useI18n,
  useToast,
} from "@authman/shared";
import { PLAYER_DIALOG_SCREENS, fetchPlayerMessages, updatePlayerMessages } from "../api/admin";
import {
  CHAT_MESSAGE_KEYS,
  DIALOG_ERROR_KEYS,
  GATE_KICK_KEYS,
  LIMBO_KICK_KEYS,
  PROFILE_ERROR_KEYS,
  MessageRow,
  SourceEditor,
  applySample,
  playerMessagesQueryKey,
  sampleVars,
} from "../components/playerMessages";

type Scene = "errors" | "success" | "gate";

function sceneOf(raw: string | undefined): Scene {
  return raw === "success" || raw === "gate" ? raw : "errors";
}

export function PlayerMessageScenePage() {
  const { t, tError } = useI18n();
  const toast = useToast();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const params = useParams<{ id?: string; scene?: string }>();
  const serverId = (params.id ?? "").trim() || undefined;
  const scene = sceneOf(params.scene);
  const serverBackTarget = serverId ? `/nodes/${encodeURIComponent(serverId)}?tab=messages` : "/login-portals/messages";
  const backTarget = useBackTarget(serverBackTarget);
  const queryKey = playerMessagesQueryKey(serverId);

  const q = useQuery({ queryKey, queryFn: () => fetchPlayerMessages(serverId) });
  const [messages, setMessages] = useState<Record<string, string>>({});
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (q.data) {
      setMessages({ ...q.data.messages.overrides });
      setFieldErrors({});
    }
  }, [q.data]);

  const save = useMutation({
    mutationFn: async () => {
      // Refetch right before saving: PUT is full-replace, so the dialog half
      // must come from the freshest server state, not a stale cache.
      const fresh = await fetchPlayerMessages(serverId);
      const dialogs = Object.fromEntries(
        PLAYER_DIALOG_SCREENS.map((entry) => [entry, fresh.dialogs[entry].override]),
      );
      return updatePlayerMessages({ messages, dialogs }, serverId);
    },
    onSuccess: (next) => {
      setMessages({ ...next.messages.overrides });
      setFieldErrors({});
      toast.push({ tone: "success", title: t("admin.playerMessages.saved.toast") });
      void qc.invalidateQueries({ queryKey });
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        const fields = (err.details?.fields ?? {}) as Record<string, string>;
        setFieldErrors(fields);
        toast.danger(Object.keys(fields).length ? t("admin.playerMessages.invalid.toast") : tError(err.code));
        return;
      }
      toast.danger(t("common.unknown"));
    },
  });

  const vars = useMemo(() => sampleVars(t), [t]);

  if (q.error) {
    return <PageShell><ErrorState error={q.error} onRetry={() => q.refetch()} /></PageShell>;
  }
  if (!q.data) {
    return <PageShell><Skeleton height={240} /></PageShell>;
  }
  const data = q.data;
  const defaults = data.messages.defaults;
  const placeholders = data.messages.placeholders;

  const setMessage = (key: string, value: string) => {
    setMessages((prev) => {
      const next = { ...prev };
      if (value.trim() === "") delete next[key];
      else next[key] = value;
      return next;
    });
  };

  const dirty = JSON.stringify(sorted(messages)) !== JSON.stringify(sorted(data.messages.overrides));

  const messageGroup = (keys: readonly string[], kind: "chat" | "kick") => (
    <div className="pm-msg-list">
      {keys.map((key) => (
        <MessageRow
          key={key}
          msgKey={key}
          draft={messages[key] ?? ""}
          defaultValue={defaults[key] ?? ""}
          placeholders={placeholders[key] ?? []}
          kind={kind}
          vars={vars}
          fieldError={fieldErrors[`messages.${key}`]}
          onChange={setMessage}
        />
      ))}
    </div>
  );

  const errorEntries = Object.entries(fieldErrors);

  return (
    <PageShell testId={`pm-scene-${scene}`}>
      <div className="pm-editor-top">
        <BackLink onClick={() => navigate(backTarget)}>{t("admin.playerMessages.heading")}</BackLink>
        <span className="pm-editor-top__spacer" />
        <Button
          variant="primary"
          icon="save"
          loading={save.isPending}
          disabled={!dirty}
          onClick={() => save.mutate()}
          data-testid="pm-save"
        >
          {t("common.save")}
        </Button>
      </div>
      <PageHeader title={t(`admin.playerMessages.scene.${scene}`)} desc={t(`admin.playerMessages.scene.${scene}.desc`)} />
      {errorEntries.length ? (
        <Alert tone="danger" title={t("admin.playerMessages.invalid.toast")} testId="pm-field-errors">
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {errorEntries.slice(0, 8).map(([path, message]) => (
              <li key={path}><code>{path}</code> — {message}</li>
            ))}
          </ul>
        </Alert>
      ) : null}
      {scene === "errors" ? (
        <div className="pm-msg-list">
          <h4 className="section-subhead">{t("admin.playerMessages.group.dialogErrors")}</h4>
          <p className="section-copy">{t("admin.playerMessages.group.dialogErrors.desc")}</p>
          {messageGroup(DIALOG_ERROR_KEYS, "chat")}
          <h4 className="section-subhead">{t("admin.playerMessages.group.chat")}</h4>
          <p className="section-copy">{t("admin.playerMessages.group.chat.desc")}</p>
          {messageGroup(CHAT_MESSAGE_KEYS, "chat")}
          <h4 className="section-subhead">{t("admin.playerMessages.group.profileErrors")}</h4>
          <p className="section-copy">{t("admin.playerMessages.group.profileErrors.desc")}</p>
          {messageGroup(PROFILE_ERROR_KEYS, "chat")}
          <h4 className="section-subhead">{t("admin.playerMessages.group.limboKick")}</h4>
          <p className="section-copy">{t("admin.playerMessages.group.limboKick.desc")}</p>
          {messageGroup(LIMBO_KICK_KEYS, "kick")}
        </div>
      ) : null}
      {scene === "success" ? (
        <SuccessSceneEditor
          messages={messages}
          defaults={defaults}
          placeholders={placeholders}
          vars={vars}
          fieldErrors={fieldErrors}
          onChange={setMessage}
        />
      ) : null}
      {scene === "gate" ? (
        <div className="pm-msg-list">
          <p className="section-copy">{t("admin.playerMessages.group.gate.desc")}</p>
          {messageGroup(GATE_KICK_KEYS, "kick")}
        </div>
      ) : null}
    </PageShell>
  );
}

function sorted(value: Record<string, string>): Array<[string, string]> {
  return Object.entries(value).filter(([, v]) => v.trim() !== "").sort(([a], [b]) => a.localeCompare(b));
}

interface SuccessSceneEditorProps {
  messages: Record<string, string>;
  defaults: Record<string, string>;
  placeholders: Record<string, string[]>;
  vars: Record<string, string>;
  fieldErrors: Record<string, string>;
  onChange: (key: string, value: string) => void;
}

function SuccessSceneEditor({ messages, defaults, placeholders, vars, fieldErrors, onChange }: SuccessSceneEditorProps) {
  const { t } = useI18n();
  const [selected, setSelected] = useState<string>("limbo.success.title");
  const effective = (key: string) => {
    const draft = messages[key] ?? "";
    return draft.trim() !== "" ? draft : defaults[key] ?? "";
  };
  const draft = messages[selected] ?? "";
  const overridden = draft.trim() !== "";
  return (
    <div className="pm-builder">
      <div className="pm-builder__stage">
        <MinecraftHudPreview
          title={applySample(effective("limbo.success.title"), vars)}
          subtitle={applySample(effective("limbo.success.subtitle"), vars)}
          actionbar={applySample(effective("limbo.success.actionbar"), vars)}
          selectedKey={selected}
          onSelect={setSelected}
          testId="pm-hud"
        />
        <p className="card-foot-note">{t("admin.playerMessages.success.hint")}</p>
      </div>
      <Card title={t("admin.playerMessages.inspector")}>
        <div className="pm-inspector">
          <div className="pm-inspector__title">
            {t(`admin.playerMessages.key.${selected}`, selected)}
            {overridden ? <Badge tone="info">{t("admin.playerMessages.overridden")}</Badge> : null}
          </div>
          <SourceEditor
            value={draft}
            onChange={(next) => onChange(selected, next)}
            placeholders={placeholders[selected] ?? []}
            error={fieldErrors[`messages.${selected}`]}
            testId={`pm-input-${selected}`}
          />
          {overridden ? (
            <Button variant="ghost" icon="refresh" onClick={() => onChange(selected, "")} data-testid={`pm-reset-${selected}`}>
              {t("admin.playerMessages.resetDefault")}
            </Button>
          ) : (
            <p className="card-foot-note">{t("admin.playerMessages.usingDefault")}</p>
          )}
        </div>
      </Card>
    </div>
  );
}
