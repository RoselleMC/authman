import { useEffect, useMemo, useRef, useState } from "react";
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
  Field,
  Input,
  MinecraftDialogPreview,
  PageShell,
  Segmented,
  Select,
  Skeleton,
  useBackTarget,
  useI18n,
  useToast,
} from "@authman/shared";
import {
  PLAYER_DIALOG_SCREENS,
  fetchPlayerMessages,
  updatePlayerMessages,
  type PlayerDialogBody,
  type PlayerDialogButton,
  type PlayerDialogInput,
  type PlayerDialogInputKind,
  type PlayerMessageDialogDoc,
  type PlayerMessageDialogScreen,
  type PlayerMessageDialogWhen,
} from "../api/admin";
import {
  PLAYER_MESSAGES_QUERY_KEY,
  SourceEditor,
  type DialogBranch,
  applySample,
  cloneDoc,
  dialogToPreview,
  newElementId,
  sampleVars,
  validateDialogDocTS,
} from "../components/playerMessages";

type Selection =
  | { kind: "dialog" }
  | { kind: "body"; id: string }
  | { kind: "input"; id: string }
  | { kind: "button"; id: string };

function selectionKey(sel: Selection): string {
  return sel.kind === "dialog" ? "dialog" : `${sel.kind}:${sel.id}`;
}

function stripMiniMessage(value: string): string {
  return value.replace(/<[^>]*>/g, "").trim();
}

/** Strips fields whose absence and zero-value are equivalent so no-op toggles
 * (e.g. esc on/off) do not leave the doc permanently "dirty"/"customized". */
function canonicalizeDoc(doc: PlayerMessageDialogDoc): unknown {
  const clean = (value: unknown): unknown => {
    if (Array.isArray(value)) return value.map(clean);
    if (value && typeof value === "object") {
      const out: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
        if (v === undefined || v === null || v === false || v === 0 || v === "") continue;
        if (k === "when" && v === "always") continue;
        if (k === "after_action" && v === "wait_for_response") continue;
        if (k === "label_visible" && v === true) continue;
        if (k === "version") continue;
        out[k] = clean(v);
      }
      return out;
    }
    return value;
  };
  return clean(doc);
}

function docsEqual(a: PlayerMessageDialogDoc, b: PlayerMessageDialogDoc): boolean {
  return JSON.stringify(canonicalizeDoc(a)) === JSON.stringify(canonicalizeDoc(b));
}

export function PlayerDialogEditorPage() {
  const { t, tError } = useI18n();
  const toast = useToast();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const params = useParams();
  const rawScreen = params.screen ?? "login";
  const screen: PlayerMessageDialogScreen = rawScreen === "register" || rawScreen === "profile_create" || rawScreen === "profile_select" ? rawScreen : "login";
  const backTarget = useBackTarget("/login-portals/messages");

  const q = useQuery({ queryKey: PLAYER_MESSAGES_QUERY_KEY, queryFn: fetchPlayerMessages });

  const [doc, setDoc] = useState<PlayerMessageDialogDoc | null>(null);
  const [selection, setSelection] = useState<Selection>({ kind: "dialog" });
  const [branch, setBranch] = useState<DialogBranch>("offline");
  const [showError, setShowError] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const loadedScreen = useRef<string | null>(null);
  useEffect(() => {
    if (q.data && (doc === null || loadedScreen.current !== screen)) {
      loadedScreen.current = screen;
      const entry = q.data.dialogs[screen];
      setDoc(cloneDoc(entry.override ?? entry.default));
      setSelection({ kind: "dialog" });
      setFieldErrors({});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.data, screen]);

  const save = useMutation({
    mutationFn: async () => {
      // Refetch right before saving: PUT is full-replace, so the untouched
      // half must come from the freshest server state, not a stale cache.
      const fresh = await fetchPlayerMessages();
      const isOverridden = !docsEqual(doc!, fresh.dialogs[screen].default);
      const dialogs = Object.fromEntries(
        PLAYER_DIALOG_SCREENS.map((entry) => [entry, entry === screen ? (isOverridden ? doc : null) : fresh.dialogs[entry].override]),
      );
      return updatePlayerMessages({ messages: fresh.messages.overrides, dialogs });
    },
    onSuccess: (next) => {
      const entry = next.dialogs[screen];
      setDoc(cloneDoc(entry.override ?? entry.default));
      setFieldErrors({});
      toast.push({ tone: "success", title: t("admin.playerMessages.saved.toast") });
      void qc.invalidateQueries({ queryKey: PLAYER_MESSAGES_QUERY_KEY });
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
  if (!q.data || !doc) {
    return <PageShell><Skeleton height={320} /></PageShell>;
  }
  const data = q.data;
  const defaultDoc = data.dialogs[screen].default;
  const serverDoc = data.dialogs[screen].override ?? defaultDoc;
  const dirty = !docsEqual(doc, serverDoc);
  const overridden = !docsEqual(doc, defaultDoc);

  const mutate = (recipe: (next: PlayerMessageDialogDoc) => void) => {
    const next = cloneDoc(doc);
    recipe(next);
    setDoc(next);
  };

  // Guard navigation away from unsaved edits (the component stays mounted on a
  // screen switch and would otherwise silently reload from server state).
  const confirmDiscard = () => !dirty || window.confirm(t("admin.playerMessages.discardConfirm"));

  const errorSample = data.messages.overrides["limbo.error.invalid_password"]?.trim()
    || data.messages.defaults["limbo.error.invalid_password"]
    || "";

  const previewState = { branch: screen === "register" ? ("offline" as DialogBranch) : branch, hasError: showError };
  const model = dialogToPreview(doc, screen, previewState, vars, errorSample);
  const validationErrors = validateDialogDocTS(screen, doc, t);

  const ensureVisible = (when?: PlayerMessageDialogWhen) => {
    switch (when) {
      case "error":
        setShowError(true);
        break;
      case "premium_passthrough":
        setBranch("premium_passthrough");
        break;
      case "premium_unverified":
        if (branch === "premium_passthrough") setBranch("offline");
        break;
      case "auth_required":
        if (branch === "premium_passthrough") setBranch("offline");
        break;
      default:
        break;
    }
  };

  const select = (sel: Selection, when?: PlayerMessageDialogWhen) => {
    setSelection(sel);
    ensureVisible(when);
  };

  const onPreviewSelect = (key: string) => {
    if (key === "dialog") return setSelection({ kind: "dialog" });
    const [kind, id] = key.split(":", 2);
    if ((kind === "body" || kind === "input" || kind === "button") && id) {
      setSelection({ kind, id } as Selection);
    }
  };

  const whenOptions: Array<{ value: PlayerMessageDialogWhen; label: string }> = screen === "login"
    ? [
      { value: "always", label: t("admin.playerMessages.when.always") },
      { value: "auth_required", label: t("admin.playerMessages.when.authRequired") },
      { value: "premium_passthrough", label: t("admin.playerMessages.when.premiumPassthrough") },
      { value: "premium_unverified", label: t("admin.playerMessages.when.premiumUnverified") },
      { value: "error", label: t("admin.playerMessages.when.error") },
    ]
    : [
      { value: "always", label: t("admin.playerMessages.when.always") },
      { value: "error", label: t("admin.playerMessages.when.error") },
    ];
  const screenRoles: Array<{ value: string; label: string }> = [
    { value: "", label: t("admin.playerMessages.role.none") },
    ...(screen === "login" || screen === "register" ? [{ value: "password", label: t("admin.playerMessages.role.password") }] : []),
    ...(screen === "register" ? [{ value: "confirm", label: t("admin.playerMessages.role.confirm") }] : []),
    ...(screen === "profile_create" ? [{ value: "profile_name", label: t("admin.playerMessages.role.profile_name") }] : []),
    ...(screen === "profile_select" ? [{ value: "profile_choice", label: t("admin.playerMessages.role.profile_choice") }] : []),
  ];
  const openScreenTargets: PlayerMessageDialogScreen[] = screen === "profile_select" ? ["profile_create"] : screen === "profile_create" ? ["profile_select"] : [];

  const whenBadge = (when?: PlayerMessageDialogWhen) => {
    if (!when || when === "always") return null;
    return <Badge tone={when === "error" ? "warning" : "neutral"}>{t(`admin.playerMessages.whenShort.${when}`, when)}</Badge>;
  };

  const addBody = (kind: "text" | "item") => {
    const id = newElementId("b");
    mutate((next) => {
      next.body.push(kind === "item"
        ? { id, kind: "item", item: "minecraft:diamond", count: 1, description: "", show_tooltip: true, when: "always" }
        : { id, kind: "text", text: t("admin.playerMessages.dialog.newLine"), width: 240, when: "always" });
    });
    setSelection({ kind: "body", id });
  };

  const addInput = (kind: PlayerDialogInputKind) => {
    const id = newElementId("i");
    mutate((next) => {
      const base: PlayerDialogInput = { id, kind, key: `field_${next.inputs.length + 1}`, label: t("admin.playerMessages.dialog.newInput"), when: "always" };
      if (kind === "text") Object.assign(base, { max_length: 64, width: 240 });
      if (kind === "boolean") Object.assign(base, { initial_bool: false });
      if (kind === "option") Object.assign(base, { options: [{ id: "a", display: "Option A", initial: true }, { id: "b", display: "Option B" }], width: 240 });
      if (kind === "range") Object.assign(base, { start: 0, end: 100, width: 240 });
      next.inputs.push(base);
    });
    setSelection({ kind: "input", id });
  };

  const addButton = () => {
    const id = newElementId("a");
    mutate((next) => {
      next.buttons.push({ id, label: t("admin.playerMessages.dialog.newButton"), action: { kind: "open_url", url: "https://example.com" }, when: "always" });
      if (!next.after_action || next.after_action === "wait_for_response") next.after_action = "none";
      next.pause = false;
    });
    setSelection({ kind: "button", id });
  };

  const moveElement = (list: "body" | "inputs" | "buttons", index: number, delta: number) => {
    mutate((next) => {
      const arr = next[list] as Array<unknown>;
      const target = index + delta;
      if (target < 0 || target >= arr.length) return;
      const [item] = arr.splice(index, 1);
      arr.splice(target, 0, item);
    });
  };

  const removeElement = (list: "body" | "inputs" | "buttons", index: number) => {
    mutate((next) => {
      (next[list] as Array<unknown>).splice(index, 1);
    });
    setSelection({ kind: "dialog" });
  };

  const numberField = (label: string, value: number | undefined | null, onValue: (next: number) => void, opts?: { min?: number; max?: number; hint?: string; testId?: string }) => (
    <Field label={label} hint={opts?.hint}>
      <input
        className="input"
        type="number"
        min={opts?.min ?? 0}
        max={opts?.max ?? 1024}
        value={value ?? 0}
        onChange={(e) => onValue(Number(e.target.value) || 0)}
        data-testid={opts?.testId}
      />
    </Field>
  );

  const toggleField = (label: string, hint: string | undefined, checked: boolean, onChecked: (next: boolean) => void, testId?: string) => (
    <label className="toggle-row">
      <input type="checkbox" checked={checked} onChange={(e) => onChecked(e.currentTarget.checked)} data-testid={testId} />
      <span>
        <strong>{label}</strong>
        {hint ? <small>{hint}</small> : null}
      </span>
    </label>
  );

  const whenField = (value: PlayerMessageDialogWhen | undefined, onValue: (next: PlayerMessageDialogWhen) => void) => (
    <Field label={t("admin.playerMessages.dialog.when")} hint={t("admin.playerMessages.dialog.when.hint")}>
      <Select
        value={value ?? "always"}
        onChange={(next) => {
          onValue(next as PlayerMessageDialogWhen);
          ensureVisible(next as PlayerMessageDialogWhen);
        }}
        options={whenOptions}
        testId="pm-el-when"
      />
    </Field>
  );

  const textPlaceholders = ["player", "server", "error"];

  let inspector: React.ReactNode = null;
  let inspectorTitle: React.ReactNode = t("admin.playerMessages.dialog.el.dialog");

  if (selection.kind === "dialog") {
    inspector = (
      <>
        <Field label={t("admin.playerMessages.dialog.title")}>
          <SourceEditor value={doc.title} onChange={(next) => mutate((d) => { d.title = next; })} placeholders={["player", "server"]} rows={2} error={fieldErrors[`dialogs.${screen}.title`]} testId="pm-dialog-title-input" />
        </Field>
        <Field label={t("admin.playerMessages.dialog.externalTitle")} hint={t("admin.playerMessages.dialog.externalTitle.hint")}>
          <SourceEditor value={doc.external_title ?? ""} onChange={(next) => mutate((d) => { d.external_title = next; })} placeholders={["player", "server"]} rows={2} testId="pm-dialog-exttitle-input" />
        </Field>
        <Field label={t("admin.playerMessages.dialog.afterAction")} hint={t("admin.playerMessages.dialog.afterAction.hint")}>
          <Select
            value={doc.after_action ?? "wait_for_response"}
            onChange={(next) => mutate((d) => { d.after_action = next as PlayerMessageDialogDoc["after_action"]; })}
            options={[
              { value: "wait_for_response", label: t("admin.playerMessages.afterAction.wait") },
              { value: "none", label: t("admin.playerMessages.afterAction.none") },
            ]}
            testId="pm-dialog-after-action"
          />
        </Field>
        {doc.buttons.length > 1 ? numberField(t("admin.playerMessages.dialog.columns"), doc.columns ?? 2, (v) => mutate((d) => { d.columns = v; }), { min: 1, max: 4, hint: t("admin.playerMessages.dialog.columns.hint"), testId: "pm-dialog-columns" }) : null}
        {toggleField(t("admin.playerMessages.dialog.escClose"), t("admin.playerMessages.dialog.escClose.hint"), Boolean(doc.can_close_with_escape), (v) => mutate((d) => { d.can_close_with_escape = v; }), "pm-dialog-esc")}
        {toggleField(t("admin.playerMessages.dialog.pause"), t("admin.playerMessages.dialog.pause.hint"), Boolean(doc.pause), (v) => mutate((d) => { d.pause = v; }), "pm-dialog-pause")}
      </>
    );
  } else if (selection.kind === "body") {
    const index = doc.body.findIndex((b) => b.id === selection.id);
    const el = index >= 0 ? doc.body[index] : undefined;
    if (el) {
      inspectorTitle = el.kind === "item" ? t("admin.playerMessages.dialog.el.item") : t("admin.playerMessages.dialog.el.textLine");
      const patch = (recipe: (next: PlayerDialogBody) => void) => mutate((d) => { const b = d.body[index]; if (b) recipe(b); });
      inspector = (
        <>
          <ElementToolbar
            onUp={index > 0 ? () => moveElement("body", index, -1) : undefined}
            onDown={index < doc.body.length - 1 ? () => moveElement("body", index, 1) : undefined}
            onDelete={() => removeElement("body", index)}
          />
          {el.kind === "item" ? (
            <>
              <Field label={t("admin.playerMessages.dialog.itemId")} hint={t("admin.playerMessages.dialog.itemId.hint")}>
                <Input value={el.item ?? ""} mono onChange={(e) => patch((b) => { b.item = e.target.value; })} placeholder="minecraft:diamond" data-testid="pm-item-id" />
              </Field>
              <div className="pm-prop-grid">
                {numberField(t("admin.playerMessages.dialog.itemCount"), el.count ?? 1, (v) => patch((b) => { b.count = v; }), { min: 1, max: 99 })}
                {numberField(t("admin.playerMessages.dialog.width"), el.width ?? 0, (v) => patch((b) => { b.width = v; }), { max: 256 })}
                {numberField(t("admin.playerMessages.dialog.itemHeight"), el.height ?? 0, (v) => patch((b) => { b.height = v; }), { max: 256 })}
              </div>
              <Field label={t("admin.playerMessages.dialog.itemDesc")}>
                <SourceEditor value={el.description ?? ""} onChange={(next) => patch((b) => { b.description = next; })} placeholders={textPlaceholders} testId="pm-item-desc" />
              </Field>
              {toggleField(t("admin.playerMessages.dialog.itemTooltip"), undefined, el.show_tooltip !== false, (v) => patch((b) => { b.show_tooltip = v; }))}
              {toggleField(t("admin.playerMessages.dialog.itemDecorations"), undefined, Boolean(el.show_decorations), (v) => patch((b) => { b.show_decorations = v; }))}
            </>
          ) : (
            <>
              <SourceEditor value={el.text ?? ""} onChange={(next) => patch((b) => { b.text = next; })} placeholders={textPlaceholders} error={fieldErrors[`dialogs.${screen}.body[${index}].text`]} testId="pm-dialog-block-input" />
              {numberField(t("admin.playerMessages.dialog.width"), el.width ?? 0, (v) => patch((b) => { b.width = v; }), { hint: t("admin.playerMessages.dialog.width.hint") })}
            </>
          )}
          {whenField(el.when, (next) => patch((b) => { b.when = next; }))}
        </>
      );
    }
  } else if (selection.kind === "input") {
    const index = doc.inputs.findIndex((i) => i.id === selection.id);
    const el = index >= 0 ? doc.inputs[index] : undefined;
    if (el) {
      inspectorTitle = t(`admin.playerMessages.dialog.el.input.${el.kind}`);
      const patch = (recipe: (next: PlayerDialogInput) => void) => mutate((d) => { const i = d.inputs[index]; if (i) recipe(i); });
      inspector = (
        <>
          <ElementToolbar
            onUp={index > 0 ? () => moveElement("inputs", index, -1) : undefined}
            onDown={index < doc.inputs.length - 1 ? () => moveElement("inputs", index, 1) : undefined}
            onDelete={() => removeElement("inputs", index)}
          />
          <Field label={t("admin.playerMessages.dialog.inputLabel")}>
            <SourceEditor value={el.label} onChange={(next) => patch((i) => { i.label = next; })} placeholders={["player", "server"]} rows={2} error={fieldErrors[`dialogs.${screen}.inputs[${index}].label`]} testId="pm-input-label" />
          </Field>
          {(el.kind === "text" || el.kind === "option") && screenRoles.length > 1 ? (
            <Field label={t("admin.playerMessages.role.label")} hint={t("admin.playerMessages.role.hint")}>
              <Select
                value={el.role ?? ""}
                onChange={(next) => mutate((d) => {
                  const role = next as PlayerDialogInput["role"];
                  for (const [j, input] of d.inputs.entries()) {
                    if (input.id !== el.id && role && input.role === role) {
                      input.role = "";
                      input.key = `field_${j + 1}_${input.id.slice(-3)}`;
                    }
                  }
                  const target = d.inputs[index];
                  if (!target) return;
                  target.role = role;
                  if (role) {
                    target.multiline = false;
                    target.multiline_lines = 0;
                  }
                  if (role === "password") target.key = "password";
                  if (role === "confirm") target.key = "confirm_password";
                  if (role === "profile_name") target.key = "profile_name";
                  if (role === "profile_choice") target.key = "profile_choice";
                })}
                options={screenRoles.filter((r) => !r.value || (r.value === "profile_choice") === (el.kind === "option"))}
                testId="pm-input-role"
              />
            </Field>
          ) : null}
          <div className="pm-prop-grid">
            <Field label={t("admin.playerMessages.dialog.inputKey")} hint={t("admin.playerMessages.dialog.inputKey.hint")}>
              <Input value={el.key} mono disabled={Boolean(el.role)} onChange={(e) => patch((i) => { i.key = e.target.value; })} data-testid="pm-input-key" />
            </Field>
            {el.kind !== "boolean" ? numberField(t("admin.playerMessages.dialog.width"), el.width ?? 0, (v) => patch((i) => { i.width = v; })) : null}
          </div>
          {el.kind !== "boolean" ? toggleField(t("admin.playerMessages.dialog.labelVisible"), undefined, el.label_visible !== false, (v) => patch((i) => { i.label_visible = v; })) : null}
          {el.kind === "text" ? (
            <>
              <div className="pm-prop-grid">
                {numberField(t("admin.playerMessages.dialog.maxLength"), el.max_length ?? 0, (v) => patch((i) => { i.max_length = v; }))}
                <Field label={t("admin.playerMessages.dialog.initial")}>
                  <Input value={el.initial ?? ""} onChange={(e) => patch((i) => { i.initial = e.target.value; })} />
                </Field>
              </div>
              {!el.role ? toggleField(t("admin.playerMessages.dialog.multiline"), undefined, Boolean(el.multiline), (v) => patch((i) => { i.multiline = v; })) : null}
              {el.multiline ? numberField(t("admin.playerMessages.dialog.multilineLines"), el.multiline_lines ?? 0, (v) => patch((i) => { i.multiline_lines = v; }), { max: 20 }) : null}
            </>
          ) : null}
          {el.kind === "boolean" ? (
            <>
              {toggleField(t("admin.playerMessages.dialog.initialOn"), undefined, Boolean(el.initial_bool), (v) => patch((i) => { i.initial_bool = v; }), "pm-input-initial-bool")}
              <div className="pm-prop-grid">
                <Field label={t("admin.playerMessages.dialog.onTrue")}>
                  <Input value={el.on_true ?? ""} onChange={(e) => patch((i) => { i.on_true = e.target.value; })} />
                </Field>
                <Field label={t("admin.playerMessages.dialog.onFalse")}>
                  <Input value={el.on_false ?? ""} onChange={(e) => patch((i) => { i.on_false = e.target.value; })} />
                </Field>
              </div>
            </>
          ) : null}
          {el.kind === "option" ? (
            <Field label={t("admin.playerMessages.dialog.options")} hint={t("admin.playerMessages.dialog.options.hint")}>
              <div className="pm-msg-list" style={{ gap: 8 }}>
                {(el.options ?? []).map((option, j) => (
                  <div className="pm-option-row" key={`${el.id}-${j}`}>
                    <input
                      type="radio"
                      name={`pm-option-initial-${el.id}`}
                      checked={Boolean(option.initial)}
                      onChange={() => patch((i) => {
                        for (const o of i.options ?? []) o.initial = false;
                        const target = (i.options ?? [])[j];
                        if (target) target.initial = true;
                      })}
                      title={t("admin.playerMessages.dialog.optionInitial")}
                    />
                    <Field label={undefined}>
                      <Input value={option.id} mono placeholder="id" onChange={(e) => patch((i) => { const o = (i.options ?? [])[j]; if (o) o.id = e.target.value; })} />
                    </Field>
                    <Field label={undefined}>
                      <Input value={option.display} placeholder={t("admin.playerMessages.dialog.optionDisplay")} onChange={(e) => patch((i) => { const o = (i.options ?? [])[j]; if (o) o.display = e.target.value; })} />
                    </Field>
                    <Button variant="ghost" icon="trash" aria-label={t("common.delete")} onClick={() => patch((i) => { (i.options ?? []).splice(j, 1); })} />
                  </div>
                ))}
                <Button variant="ghost" icon="plus" onClick={() => patch((i) => { const opts = i.options ?? []; let n = opts.length + 1; const taken = new Set(opts.map((o) => o.id)); while (taken.has(`opt${n}`)) n += 1; i.options = [...opts, { id: `opt${n}`, display: "Option" }]; })}>
                  {t("admin.playerMessages.dialog.addOption")}
                </Button>
              </div>
            </Field>
          ) : null}
          {el.kind === "range" ? (
            <>
              <div className="pm-prop-grid">
                {numberField(t("admin.playerMessages.dialog.rangeStart"), el.start ?? 0, (v) => patch((i) => { i.start = v; }), { min: -100000, max: 100000 })}
                {numberField(t("admin.playerMessages.dialog.rangeEnd"), el.end ?? 0, (v) => patch((i) => { i.end = v; }), { min: -100000, max: 100000 })}
                {numberField(t("admin.playerMessages.dialog.rangeStep"), el.step ?? 0, (v) => patch((i) => { i.step = v > 0 ? v : null; }), { min: 0, max: 100000, hint: t("admin.playerMessages.dialog.rangeStep.hint") })}
                {numberField(t("admin.playerMessages.dialog.rangeInitial"), el.initial_num ?? el.start ?? 0, (v) => patch((i) => { i.initial_num = v; }), { min: -100000, max: 100000 })}
              </div>
              <Field label={t("admin.playerMessages.dialog.rangeFormat")} hint={t("admin.playerMessages.dialog.rangeFormat.hint")}>
                <Input value={el.label_format ?? ""} mono placeholder="%s" onChange={(e) => patch((i) => { i.label_format = e.target.value; })} />
              </Field>
            </>
          ) : null}
          {whenField(el.when, (next) => patch((i) => { i.when = next; }))}
          {el.role
            ? <p className="card-foot-note">{t("admin.playerMessages.role.note")}</p>
            : <p className="card-foot-note">{t("admin.playerMessages.role.decorativeNote")}</p>}
        </>
      );
    }
  } else if (selection.kind === "button") {
    const index = doc.buttons.findIndex((b) => b.id === selection.id);
    const el = index >= 0 ? doc.buttons[index] : undefined;
    if (el) {
      inspectorTitle = t("admin.playerMessages.dialog.el.button");
      const patch = (recipe: (next: PlayerDialogButton) => void) => mutate((d) => { const b = d.buttons[index]; if (b) recipe(b); });
      inspector = (
        <>
          <ElementToolbar
            onUp={index > 0 ? () => moveElement("buttons", index, -1) : undefined}
            onDown={index < doc.buttons.length - 1 ? () => moveElement("buttons", index, 1) : undefined}
            onDelete={() => removeElement("buttons", index)}
          />
          <Field label={t("admin.playerMessages.dialog.buttonLabel")}>
            <SourceEditor value={el.label} onChange={(next) => patch((b) => { b.label = next; })} placeholders={["player", "server"]} rows={2} error={fieldErrors[`dialogs.${screen}.buttons[${index}].label`]} testId="pm-button-label" />
          </Field>
          <Field label={t("admin.playerMessages.dialog.tooltip")} hint={t("admin.playerMessages.dialog.tooltip.hint")}>
            <SourceEditor value={el.tooltip ?? ""} onChange={(next) => patch((b) => { b.tooltip = next; })} placeholders={["player", "server"]} rows={2} />
          </Field>
          <Field label={t("admin.playerMessages.action.label")} hint={t("admin.playerMessages.action.hint")}>
            <Select
              value={el.action.kind}
              onChange={(next) => patch((b) => {
                b.action = next === "open_url"
                  ? { kind: "open_url", url: b.action.url ?? "https://" }
                  : next === "copy_to_clipboard"
                    ? { kind: "copy_to_clipboard", value: b.action.value ?? "" }
                    : next === "open_screen"
                      ? { kind: "open_screen", screen: openScreenTargets[0] }
                      : { kind: "submit" };
                if (next === "submit") b.when = "always";
              })}
              options={[
                { value: "submit", label: t("admin.playerMessages.action.submit") },
                { value: "open_url", label: t("admin.playerMessages.action.openUrl") },
                { value: "copy_to_clipboard", label: t("admin.playerMessages.action.copy") },
                ...(openScreenTargets.length > 0 ? [{ value: "open_screen", label: t("admin.playerMessages.action.openScreen") }] : []),
              ]}
              testId="pm-button-action"
            />
          </Field>
          {el.action.kind === "open_url" ? (
            <Field label={t("admin.playerMessages.action.url")}>
              <Input value={el.action.url ?? ""} mono onChange={(e) => patch((b) => { b.action.url = e.target.value; })} placeholder="https://example.com" data-testid="pm-button-url" />
            </Field>
          ) : null}
          {el.action.kind === "copy_to_clipboard" ? (
            <Field label={t("admin.playerMessages.action.value")}>
              <Input value={el.action.value ?? ""} onChange={(e) => patch((b) => { b.action.value = e.target.value; })} data-testid="pm-button-value" />
            </Field>
          ) : null}
          {el.action.kind === "open_screen" ? (
            <Field label={t("admin.playerMessages.action.screenTarget")} hint={t("admin.playerMessages.action.screenTarget.hint")}>
              <Select
                value={el.action.screen ?? openScreenTargets[0] ?? ""}
                onChange={(next) => patch((b) => { b.action.screen = next; })}
                options={openScreenTargets.map((target) => ({ value: target, label: t(`admin.playerMessages.scene.${target}`) }))}
                testId="pm-button-screen"
              />
            </Field>
          ) : null}
          {numberField(t("admin.playerMessages.dialog.width"), el.width ?? 0, (v) => patch((b) => { b.width = v; }))}
          {el.action.kind !== "submit" ? whenField(el.when, (next) => patch((b) => { b.when = next; })) : null}
          {el.action.kind === "open_url" || el.action.kind === "copy_to_clipboard"
            ? <p className="card-foot-note">{t("admin.playerMessages.action.clientNote")}</p>
            : el.action.kind === "submit"
              ? <p className="card-foot-note">{t("admin.playerMessages.v.submitHidden")}</p>
              : null}
        </>
      );
    }
  }

  const treeRow = (sel: Selection, label: string, badges: React.ReactNode, testId: string, when?: PlayerMessageDialogWhen) => (
    <button
      type="button"
      key={selectionKey(sel)}
      className={`pm-tree__row${selectionKey(selection) === selectionKey(sel) ? " is-selected" : ""}`}
      onClick={() => select(sel, when)}
      data-testid={testId}
    >
      <span className="pm-tree__label">{label}</span>
      <span className="pm-tree__badges">{badges}</span>
    </button>
  );

  return (
    <PageShell testId={`pm-dialog-editor-${screen}`}>
      <div className="pm-editor-top">
        <BackLink onClick={() => { if (confirmDiscard()) navigate(backTarget); }}>{t("admin.playerMessages.heading")}</BackLink>
        <Segmented<PlayerMessageDialogScreen>
          value={screen}
          onChange={(next) => { if (confirmDiscard()) navigate(`/login-portals/messages/dialogs/${next}`, { replace: true }); }}
          options={[
            { value: "login", label: t("admin.playerMessages.scene.login") },
            { value: "register", label: t("admin.playerMessages.scene.register") },
            { value: "profile_create", label: t("admin.playerMessages.scene.profile_create") },
            { value: "profile_select", label: t("admin.playerMessages.scene.profile_select") },
          ]}
          ariaLabel={t("admin.playerMessages.heading")}
        />
        <span className="pm-editor-top__spacer" />
        {overridden
          ? <Badge tone="info">{t("admin.playerMessages.overridden")}</Badge>
          : <Badge tone="neutral">{t("admin.playerMessages.default")}</Badge>}
        {overridden ? (
          <Button variant="ghost" icon="refresh" onClick={() => { setDoc(cloneDoc(defaultDoc)); setSelection({ kind: "dialog" }); }} data-testid="pm-dialog-reset">
            {t("admin.playerMessages.resetDefault")}
          </Button>
        ) : null}
        <Button
          variant="primary"
          icon="save"
          loading={save.isPending}
          disabled={!dirty || validationErrors.length > 0}
          onClick={() => save.mutate()}
          data-testid="pm-save"
        >
          {t("common.save")}
        </Button>
      </div>
      {validationErrors.length > 0 ? (
        <Alert tone="warning" title={t("admin.playerMessages.v.heading")} testId="pm-validation">
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {validationErrors.slice(0, 6).map((message) => <li key={message}>{message}</li>)}
          </ul>
        </Alert>
      ) : null}
      {Object.keys(fieldErrors).length > 0 ? (
        <Alert tone="danger" title={t("admin.playerMessages.invalid.toast")} testId="pm-field-errors">
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {Object.entries(fieldErrors).slice(0, 8).map(([path, message]) => (
              <li key={path}><code>{path}</code> — {message}</li>
            ))}
          </ul>
        </Alert>
      ) : null}
      <div className="pm-studio">
        <Card title={t("admin.playerMessages.structure")} className="pm-tree-card">
          <div className="pm-tree">
            <div className="pm-tree__group">
              <div className="pm-tree__head">{t("admin.playerMessages.structure.dialog")}</div>
              {treeRow({ kind: "dialog" }, stripMiniMessage(doc.title) || t("admin.playerMessages.dialog.el.dialog"), null, "pm-tree-dialog")}
            </div>
            <div className="pm-tree__group">
              <div className="pm-tree__head">{t("admin.playerMessages.structure.body")}</div>
              {doc.body.map((el) => treeRow(
                { kind: "body", id: el.id },
                el.kind === "item" ? (el.item || "item") : stripMiniMessage(el.text ?? "") || t("admin.playerMessages.dialog.el.textLine"),
                whenBadge(el.when),
                `pm-tree-body-${el.id}`,
                el.when,
              ))}
              <div className="pm-tree__add pm-inspector__row">
                <Button variant="ghost" icon="plus" onClick={() => addBody("text")} data-testid="pm-add-text">{t("admin.playerMessages.add.text")}</Button>
                <Button variant="ghost" icon="plus" onClick={() => addBody("item")} data-testid="pm-add-item">{t("admin.playerMessages.add.item")}</Button>
              </div>
            </div>
            <div className="pm-tree__group">
              <div className="pm-tree__head">{t("admin.playerMessages.structure.inputs")}</div>
              {doc.inputs.map((el) => treeRow(
                { kind: "input", id: el.id },
                stripMiniMessage(el.label) || el.key,
                <>
                  {el.role ? <Badge tone="success">{t(`admin.playerMessages.role.${el.role}`)}</Badge> : null}
                  {whenBadge(el.when)}
                </>,
                `pm-tree-input-${el.id}`,
                el.when,
              ))}
              <div className="pm-tree__add pm-inspector__row">
                <Button variant="ghost" icon="plus" onClick={() => addInput("text")} data-testid="pm-add-input-text">{t("admin.playerMessages.add.inputText")}</Button>
                <Button variant="ghost" icon="plus" onClick={() => addInput("boolean")} data-testid="pm-add-input-boolean">{t("admin.playerMessages.add.inputBoolean")}</Button>
                <Button variant="ghost" icon="plus" onClick={() => addInput("option")} data-testid="pm-add-input-option">{t("admin.playerMessages.add.inputOption")}</Button>
                <Button variant="ghost" icon="plus" onClick={() => addInput("range")} data-testid="pm-add-input-range">{t("admin.playerMessages.add.inputRange")}</Button>
              </div>
            </div>
            <div className="pm-tree__group">
              <div className="pm-tree__head">{t("admin.playerMessages.structure.buttons")}</div>
              {doc.buttons.map((el) => treeRow(
                { kind: "button", id: el.id },
                stripMiniMessage(el.label) || t("admin.playerMessages.dialog.el.button"),
                <>
                  <Badge tone={el.action.kind === "submit" ? "success" : "neutral"}>{t(`admin.playerMessages.action.short.${el.action.kind}`)}</Badge>
                  {whenBadge(el.when)}
                </>,
                `pm-tree-button-${el.id}`,
                el.when,
              ))}
              <div className="pm-tree__add pm-inspector__row">
                <Button variant="ghost" icon="plus" onClick={addButton} data-testid="pm-add-button">{t("admin.playerMessages.add.button")}</Button>
              </div>
            </div>
          </div>
        </Card>
        <div className="pm-stage">
          <div className="pm-states">
            {screen === "login" ? (
              <Segmented<DialogBranch>
                value={branch}
                onChange={setBranch}
                options={[
                  { value: "offline", label: t("admin.playerMessages.branch.offline") },
                  { value: "premium_passthrough", label: t("admin.playerMessages.branch.premiumPassthrough") },
                ]}
                ariaLabel={t("admin.playerMessages.branch.label")}
              />
            ) : null}
            <Segmented<"hidden" | "shown">
              value={showError ? "shown" : "hidden"}
              onChange={(v) => setShowError(v === "shown")}
              options={[
                { value: "hidden", label: t("admin.playerMessages.branch.noError") },
                { value: "shown", label: t("admin.playerMessages.branch.withError") },
              ]}
              ariaLabel={t("admin.playerMessages.branch.errorLabel")}
            />
          </div>
          <MinecraftDialogPreview
            title={model.title}
            body={model.body}
            inputs={model.inputs}
            buttons={model.buttons}
            columns={model.columns}
            selectedKey={selectionKey(selection)}
            onSelect={onPreviewSelect}
            testId={`pm-dialog-${screen}`}
          />
          <p className="card-foot-note">{t("admin.playerMessages.stage.hint")}</p>
        </div>
        <Card title={inspectorTitle} className="pm-inspector-card">
          {inspector ?? <p className="card-foot-note">{t("admin.playerMessages.dialog.selectHint")}</p>}
        </Card>
      </div>
    </PageShell>
  );
}

function ElementToolbar({ onUp, onDown, onDelete }: { onUp?: () => void; onDown?: () => void; onDelete?: () => void }) {
  const { t } = useI18n();
  return (
    <div className="pm-inspector__row">
      <Button variant="ghost" onClick={onUp} disabled={!onUp} aria-label={t("admin.playerMessages.dialog.moveUp")} data-testid="pm-el-up">↑</Button>
      <Button variant="ghost" onClick={onDown} disabled={!onDown} aria-label={t("admin.playerMessages.dialog.moveDown")} data-testid="pm-el-down">↓</Button>
      <span className="pm-inspector__spacer" />
      {onDelete ? (
        <Button variant="ghost" icon="trash" onClick={onDelete} aria-label={t("common.delete")} data-testid="pm-el-delete" />
      ) : null}
    </div>
  );
}
