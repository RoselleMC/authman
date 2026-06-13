import { useRef } from "react";
import { Badge, Button, Field, MinecraftChatPreview, MinecraftKickPreview, useI18n, type DialogPreviewBody, type DialogPreviewButton, type DialogPreviewInput } from "@authman/shared";
import type { PlayerMessageDialogDoc, PlayerMessageDialogScreen, PlayerMessageDialogWhen, PlayerMessagesData } from "../api/admin";

export type DialogBranch = "offline" | "premium_passthrough" | "premium_unverified";

export interface DialogPreviewState {
  branch: DialogBranch;
  hasError: boolean;
}

export const PLAYER_MESSAGES_QUERY_KEY = ["admin.player-messages"];

export function playerMessagesQueryKey(serverId?: string): readonly unknown[] {
  const id = (serverId ?? "").trim();
  return id ? ["admin.downstreamServer.player-messages", id] : PLAYER_MESSAGES_QUERY_KEY;
}

export const DIALOG_ERROR_KEYS = [
  "limbo.error.password_required",
  "limbo.error.invalid_password",
  "limbo.error.passwords_mismatch",
  "limbo.error.register_failed",
] as const;

export const CHAT_MESSAGE_KEYS = [
  "limbo.error.resolve_failed",
  "limbo.error.target_failed",
  "limbo.error.grant_failed",
  "limbo.error.dialog_payload",
  "limbo.error.portal_locked",
] as const;

export const PROFILE_ERROR_KEYS = [
  "limbo.error.profile_name_invalid",
  "limbo.error.profile_name_taken",
  "limbo.error.profile_limit_reached",
  "limbo.error.profile_create_failed",
  "limbo.error.profile_selection_invalid",
] as const;

export const LIMBO_KICK_KEYS = [
  "limbo.kick.client_too_old",
  "limbo.kick.transfer_unsupported",
] as const;

export const SUCCESS_KEYS = [
  "limbo.success.title",
  "limbo.success.subtitle",
  "limbo.success.actionbar",
] as const;

export const GATE_KICK_KEYS = [
  "gate.kick.unavailable",
  "gate.kick.already_online",
  "gate.kick.locked",
  "gate.kick.banned",
  "gate.kick.default_disconnect",
] as const;

export function sampleVars(t: (key: string, fallback?: string) => string): Record<string, string> {
  return {
    player: "Steve",
    server: t("admin.playerMessages.sample.server"),
    reason: t("admin.playerMessages.sample.reason"),
    count: "1",
    max: "3",
  };
}

export const SAMPLE_PROFILE_OPTIONS = [
  { id: "p1", display: "Steve", initial: true },
  { id: "p2", display: "Herobrine_2" },
];

export function applySample(text: string, vars: Record<string, string>): string {
  return text.replace(/\{(\w+)\}/g, (raw, name: string) => vars[name] ?? raw);
}

export function cloneDoc<T>(doc: T): T {
  return JSON.parse(JSON.stringify(doc)) as T;
}

export function newElementId(prefix: string): string {
  return `${prefix}${Date.now().toString(36)}${Math.floor(Math.random() * 1296).toString(36)}`;
}

export function effectiveMessage(data: PlayerMessagesData, drafts: Record<string, string>, key: string): string {
  const draft = drafts[key];
  if (draft && draft.trim() !== "") return draft;
  return data.messages.defaults[key] ?? "";
}

export function visibleWhen(when: PlayerMessageDialogWhen | undefined, st: DialogPreviewState): boolean {
  switch (when ?? "always") {
    case "always":
      return true;
    case "auth_required":
      return st.branch !== "premium_passthrough";
    case "premium_passthrough":
      return st.branch === "premium_passthrough";
    case "premium_unverified":
      // Runtime semantics: AuthRequired && !Verified — true for offline
      // password players as well, not only unverified premium names.
      return st.branch !== "premium_passthrough";
    case "error":
      return st.hasError;
    default:
      return true;
  }
}

export interface DialogPreviewModel {
  title: string;
  body: DialogPreviewBody[];
  inputs: DialogPreviewInput[];
  buttons: DialogPreviewButton[];
  columns?: number;
}

/** Projects a free-form dialog document into render-ready preview props. */
export function dialogToPreview(
  doc: PlayerMessageDialogDoc,
  screen: PlayerMessageDialogScreen,
  st: DialogPreviewState,
  vars: Record<string, string>,
  errorSample: string,
): DialogPreviewModel {
  const state: DialogPreviewState = screen === "login" ? st : { branch: "offline", hasError: st.hasError };
  const errorText = state.hasError ? applySample(errorSample, vars) : "";
  const sub = (text: string) => applySample(text.split("{error}").join(errorText), vars);
  const body: DialogPreviewBody[] = [];
  for (const el of doc.body ?? []) {
    if (!visibleWhen(el.when, state)) continue;
    if (el.kind === "item") {
      body.push({ key: `body:${el.id}`, kind: "item", item: el.item ?? "", count: el.count ?? 1, description: el.description ? sub(el.description) : undefined });
    } else {
      body.push({ key: `body:${el.id}`, kind: "text", text: sub(el.text ?? "") });
    }
  }
  const inputs: DialogPreviewInput[] = [];
  for (const input of doc.inputs ?? []) {
    if (!visibleWhen(input.when, state)) continue;
    const key = `input:${input.id}`;
    const labelVisible = input.label_visible !== false;
    if (input.kind === "boolean") {
      inputs.push({ key, kind: "boolean", label: sub(input.label), value: Boolean(input.initial_bool), onValue: input.initial_bool ? input.on_true ?? "" : input.on_false ?? "" });
    } else if (input.kind === "option") {
      const options = input.role === "profile_choice" && (input.options ?? []).length === 0 ? SAMPLE_PROFILE_OPTIONS : input.options ?? [];
      const selected = options.find((o) => o.initial) ?? options[0];
      inputs.push({ key, kind: "option", label: sub(input.label), labelVisible, display: sub(selected?.display ?? "") });
    } else if (input.kind === "range") {
      const start = input.start ?? 0;
      const end = input.end ?? 1;
      // Vanilla defaults a missing initial to the midpoint of the range.
      const value = input.initial_num ?? (start + end) / 2;
      const ratio = end > start ? (value - start) / (end - start) : 0;
      const format = input.label_format && input.label_format.includes("%s") ? input.label_format : "%s";
      inputs.push({ key, kind: "range", label: sub(input.label), valueText: format.replace("%s", String(value)), ratio });
    } else {
      inputs.push({ key, kind: "text", label: sub(input.label), labelVisible, multiline: input.multiline });
    }
  }
  const buttons: DialogPreviewButton[] = [];
  for (const button of doc.buttons ?? []) {
    if (!visibleWhen(button.when, state)) continue;
    buttons.push({ key: `button:${button.id}`, label: sub(button.label), tooltip: button.tooltip ? sub(button.tooltip) : undefined });
  }
  return { title: sub(doc.title), body, inputs, buttons, columns: doc.columns };
}

/** Frontend mirror of the Go-side document validation (structure only; the
 * MiniMessage parse feedback comes live from the preview components). */
export function validateDialogDocTS(screen: PlayerMessageDialogScreen, doc: PlayerMessageDialogDoc, t: (key: string, fallback?: string) => string): string[] {
  const errors: string[] = [];
  if (!doc.title.trim()) errors.push(t("admin.playerMessages.v.titleRequired"));
  const requiredRoles: string[] = screen === "register"
    ? ["password", "confirm"]
    : screen === "profile_create"
      ? ["profile_name"]
      : screen === "profile_select"
        ? ["profile_choice"]
        : ["password"];
  const roleCounts: Record<string, number> = {};
  const keys = new Set<string>();
  for (const input of doc.inputs ?? []) {
    const key = (input.key ?? "").trim();
    if (!/^[a-z0-9_]{1,32}$/.test(key)) errors.push(t("admin.playerMessages.v.badKey") + ` (${input.label || input.id})`);
    else if (keys.has(key)) errors.push(t("admin.playerMessages.v.dupKey") + ` (${key})`);
    keys.add(key);
    if (!input.label.trim()) errors.push(t("admin.playerMessages.v.labelRequired"));
    if (input.role) {
      roleCounts[input.role] = (roleCounts[input.role] ?? 0) + 1;
      if (!requiredRoles.includes(input.role)) errors.push(t("admin.playerMessages.v.roleScreen"));
      if (input.role === "profile_choice") {
        if (input.kind !== "option") errors.push(t("admin.playerMessages.v.profileChoiceKind"));
      } else if (input.kind !== "text") {
        errors.push(t("admin.playerMessages.v.passwordKind"));
      }
      if (input.when && input.when !== "always" && input.when !== "auth_required") errors.push(t("admin.playerMessages.v.passwordHidden"));
      if (input.multiline) errors.push(t("admin.playerMessages.v.multilineRole"));
    }
    if (input.kind === "text" && (input.initial ?? "").length > 0) {
      const effectiveMax = (input.max_length ?? 0) > 0 ? input.max_length! : 32;
      if ([...(input.initial ?? "")].length > effectiveMax) errors.push(t("admin.playerMessages.v.initialTooLong") + ` (${effectiveMax})`);
    }
    if (input.kind === "option") {
      const options = input.options ?? [];
      if (options.length === 0 && input.role !== "profile_choice") errors.push(t("admin.playerMessages.v.optionsRequired"));
      if (options.length > 16) errors.push(t("admin.playerMessages.v.optionsCap"));
      if (options.filter((o) => o.initial).length > 1) errors.push(t("admin.playerMessages.v.optionInitial"));
      const optionIds = new Set<string>();
      for (const o of options) {
        const id = (o.id ?? "").trim();
        if (!id) errors.push(t("admin.playerMessages.v.optionId"));
        else if (optionIds.has(id)) errors.push(t("admin.playerMessages.v.optionDupId") + ` (${id})`);
        optionIds.add(id);
        if (!(o.display ?? "").trim()) errors.push(t("admin.playerMessages.v.optionDisplay"));
      }
    }
    if (input.kind === "range" && (input.end ?? 0) <= (input.start ?? 0)) errors.push(t("admin.playerMessages.v.rangeInvalid"));
  }
  for (const role of requiredRoles) {
    if ((roleCounts[role] ?? 0) !== 1) errors.push(t(`admin.playerMessages.v.role.${role}`));
  }
  let submit = 0;
  let clientActions = 0;
  const buttons = doc.buttons ?? [];
  if (buttons.length === 0) errors.push(t("admin.playerMessages.v.buttonRequired"));
  for (const button of buttons) {
    if (!button.label.trim()) errors.push(t("admin.playerMessages.v.labelRequired"));
    if (button.action.kind === "submit") {
      submit += 1;
      if (button.when && button.when !== "always") errors.push(t("admin.playerMessages.v.submitHidden"));
    } else if (button.action.kind === "open_screen") {
      const targets = screen === "profile_select" ? ["profile_create"] : screen === "profile_create" ? ["profile_select"] : [];
      if (!targets.includes(button.action.screen ?? "")) errors.push(t("admin.playerMessages.v.badScreenTarget"));
    } else {
      clientActions += 1;
      if (button.action.kind === "open_url" && !/^https?:\/\/.+/.test((button.action.url ?? "").trim())) errors.push(t("admin.playerMessages.v.badUrl"));
      if (button.action.kind === "copy_to_clipboard" && !(button.action.value ?? "").trim()) errors.push(t("admin.playerMessages.v.clipboardRequired"));
    }
  }
  if (buttons.length > 0 && submit === 0) errors.push(t("admin.playerMessages.v.submitRequired"));
  if (clientActions > 0 && (doc.after_action ?? "wait_for_response") === "wait_for_response") {
    errors.push(t("admin.playerMessages.v.afterActionGuard"));
  }
  if (doc.pause && doc.after_action === "none") {
    errors.push(t("admin.playerMessages.v.pauseGuard"));
  }
  for (const body of doc.body ?? []) {
    if (body.kind === "text" && !(body.text ?? "").trim()) errors.push(t("admin.playerMessages.v.textRequired"));
    if (body.kind === "item") {
      if (!/^[a-z0-9_.-]+(:[a-z0-9_/.-]+)?$/.test((body.item ?? "").trim())) errors.push(t("admin.playerMessages.v.badItem"));
      if ((body.width ?? 0) > 256 || (body.height ?? 0) > 256) errors.push(t("admin.playerMessages.v.itemSize"));
    }
  }
  return [...new Set(errors)];
}

interface PlaceholderChipsProps {
  names: string[];
  onInsert: (name: string) => void;
}

export function PlaceholderChips({ names, onInsert }: PlaceholderChipsProps) {
  const { t } = useI18n();
  if (!names.length) return null;
  return (
    <div className="pm-placeholders" aria-label={t("admin.playerMessages.placeholders")}>
      {names.map((name) => (
        <button key={name} type="button" className="pm-placeholder" onClick={() => onInsert(name)} data-testid={`pm-placeholder-${name}`}>
          {`{${name}}`}
        </button>
      ))}
    </div>
  );
}

interface SourceEditorProps {
  value: string;
  onChange: (next: string) => void;
  placeholders?: string[];
  rows?: number;
  error?: string;
  testId?: string;
}

export function SourceEditor({ value, onChange, placeholders = [], rows = 3, error, testId }: SourceEditorProps) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const replaceSelection = (nextValue: string, cursorStart: number, cursorEnd = cursorStart) => {
    onChange(nextValue);
    requestAnimationFrame(() => {
      const el = ref.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(cursorStart, cursorEnd);
    });
  };
  const insertPlaceholder = (name: string) => {
    const token = `{${name}}`;
    const el = ref.current;
    if (!el) {
      onChange(value + token);
      return;
    }
    const start = el.selectionStart ?? value.length;
    const end = el.selectionEnd ?? value.length;
    replaceSelection(value.slice(0, start) + token + value.slice(end), start + token.length);
  };
  return (
    <div className="field" style={{ gap: 6 }}>
      <textarea
        ref={ref}
        className="textarea textarea--mono"
        value={value}
        rows={rows}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
        data-testid={testId}
      />
      {error ? <div className="field__error">{error}</div> : null}
      <PlaceholderChips names={placeholders} onInsert={insertPlaceholder} />
    </div>
  );
}

interface MessageRowProps {
  msgKey: string;
  draft: string;
  defaultValue: string;
  placeholders: string[];
  kind: "chat" | "kick";
  vars: Record<string, string>;
  fieldError?: string;
  onChange: (key: string, value: string) => void;
}

export function MessageRow({ msgKey, draft, defaultValue, placeholders, kind, vars, fieldError, onChange }: MessageRowProps) {
  const { t } = useI18n();
  const effective = draft.trim() !== "" ? draft : defaultValue;
  const overridden = draft.trim() !== "";
  const preview = applySample(effective, vars);
  return (
    <div className="pm-msg-row" data-testid={`pm-row-${msgKey}`}>
      <Field
        label={(
          <span className="pm-inspector__title">
            {t(`admin.playerMessages.key.${msgKey}`, msgKey)}
            {overridden ? <Badge tone="info">{t("admin.playerMessages.overridden")}</Badge> : null}
          </span>
        )}
        hint={overridden ? undefined : t("admin.playerMessages.usingDefault")}
      >
        <SourceEditor
          value={draft}
          onChange={(next) => onChange(msgKey, next)}
          placeholders={placeholders}
          error={fieldError}
          testId={`pm-input-${msgKey}`}
        />
        {overridden ? (
          <Button variant="ghost" icon="refresh" onClick={() => onChange(msgKey, "")} data-testid={`pm-reset-${msgKey}`}>
            {t("admin.playerMessages.resetDefault")}
          </Button>
        ) : null}
      </Field>
      {kind === "kick"
        ? <MinecraftKickPreview value={preview} testId={`pm-preview-${msgKey}`} />
        : <MinecraftChatPreview value={preview} testId={`pm-preview-${msgKey}`} />}
    </div>
  );
}
