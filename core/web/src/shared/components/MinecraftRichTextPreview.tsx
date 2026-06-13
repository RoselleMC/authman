import { useEffect, useMemo, useRef, useState } from "react";
import { MiniMessage, type Component as MiniMessageComponent } from "minimessage-js";
import { Button } from "./Button";
import { Dialog } from "./Dialog";
import { useI18n } from "../i18n/I18nProvider";
import { minecraftFontInfo } from "../minecraft/fonts";
import { cx } from "../utils/cx";

const mini = MiniMessage.miniMessage();
const MOTD_MAX_LINES = 2;
const MINI_MESSAGE_LINE_BREAK_RE = /\r\n|\r|\n|<\s*(?:newline|br)\s*\/?>/gi;

export function limitMiniMessageLines(value: string, maxLines = MOTD_MAX_LINES): string {
  if (!value || maxLines <= 0) return "";
  MINI_MESSAGE_LINE_BREAK_RE.lastIndex = 0;
  let lastIndex = 0;
  let lines = 1;
  let out = "";
  while (lines < maxLines) {
    const match = MINI_MESSAGE_LINE_BREAK_RE.exec(value);
    if (!match) return value;
    out += value.slice(lastIndex, match.index) + match[0];
    lastIndex = match.index + match[0].length;
    lines += 1;
  }
  const nextBreak = MINI_MESSAGE_LINE_BREAK_RE.exec(value);
  if (!nextBreak) return out + value.slice(lastIndex);
  return out + value.slice(lastIndex, nextBreak.index);
}

const TAG_LABELS: Record<string, string> = {
  b: "bold",
  em: "italic",
  i: "italic",
  st: "strikethrough",
  u: "underlined",
  tr: "lang",
  translate: "lang",
  translate_or: "lang_or",
  key: "keybind",
  insert: "insertion",
};

const INTERACTION_TAGS = new Set([
  "click",
  "hover",
  "insertion",
  "font",
  "keybind",
  "selector",
  "score",
  "nbt",
  "lang",
  "lang_or",
  "gradient",
  "rainbow",
  "pride",
  "transition",
  "shadow",
  "shadow_color",
  "head",
  "sprite",
]);

interface RichTextPreviewProps {
  value: string;
  placeholder?: string;
  className?: string;
  metadata?: boolean;
  testId?: string;
}

export function MinecraftRichTextPreview({ value, placeholder, className, metadata = true, testId }: RichTextPreviewProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const tags = useMemo(() => collectMiniMessageTags(value), [value]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    host.replaceChildren();
    setError(null);
    if (!value.trim()) {
      host.textContent = placeholder ?? "";
      return;
    }
    try {
      const component = mini.deserialize(value);
      mini.toHTML(component, host);
      annotateMinecraftFontSpans(component, host);
    } catch (err) {
      host.textContent = value;
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [placeholder, value]);

  return (
    <div className={cx("mc-rich", className)} data-testid={testId}>
      <div ref={hostRef} className={cx("mc-rich__content", !value.trim() ? "is-placeholder" : null)} />
      {metadata && tags.length ? (
        <div className="mc-rich__tags" aria-label="MiniMessage tags">
          {tags.map((tag) => <span key={tag} className="mc-rich__tag">{tag}</span>)}
        </div>
      ) : null}
      {error ? <div className="mc-rich__error">{error}</div> : null}
    </div>
  );
}

interface MotdPreviewProps {
  value: string;
  iconUrl?: string;
  serverName?: string;
  address?: string;
  placeholder?: string;
  onClick?: () => void;
  metadata?: boolean;
  testId?: string;
}

export function MinecraftMotdPreview({ value, iconUrl, serverName, address, placeholder, onClick, metadata = false, testId }: MotdPreviewProps) {
  const { t } = useI18n();
  const motd = useMemo(() => limitMiniMessageLines(value), [value]);
  const body = (
    <>
      <div className="mc-motd__icon">{iconUrl ? <img src={iconUrl} alt="" aria-hidden="true" /> : "A"}</div>
      <div className="mc-motd__main">
        <div className="mc-motd__top">
          <span className="mc-motd__name">{serverName || t("common.server")}</span>
          <span className="mc-motd__ping" />
        </div>
        <MinecraftRichTextPreview
          value={motd}
          placeholder={placeholder ?? t("minecraftText.empty")}
          className="mc-motd__message"
          metadata={metadata}
          testId={testId ? `${testId}-rich` : undefined}
        />
        {address ? <div className="mc-motd__address">{address}</div> : null}
      </div>
    </>
  );
  if (!onClick) {
    return <div className="mc-motd" data-testid={testId}>{body}</div>;
  }
  return (
    <button type="button" className="mc-motd mc-motd--button" onClick={onClick} data-testid={testId}>
      {body}
    </button>
  );
}

interface EditorDialogProps {
  open: boolean;
  title: string;
  desc?: string;
  value: string;
  serverName?: string;
  address?: string;
  iconUrl?: string;
  onClose: () => void;
  onSave: (value: string) => void;
  testId?: string;
}

export function MiniMessageEditorDialog({ open, title, desc, value, serverName, address, iconUrl, onClose, onSave, testId }: EditorDialogProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState(value);

  useEffect(() => {
    if (open) setDraft(value);
  }, [open, value]);

  return (
    <Dialog
      open={open}
      onClose={onClose}
      icon="settings"
      iconTone="primary"
      title={title}
      desc={desc}
      size="xl"
      testId={testId}
      footer={(
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" icon="check" onClick={() => onSave(limitMiniMessageLines(draft))} data-testid={testId ? `${testId}-save` : undefined}>{t("common.save")}</Button>
        </>
      )}
    >
      <div className="mm-editor">
        <section className="mm-editor__preview" aria-label={t("minecraftText.preview")}>
          <MinecraftMotdPreview value={draft} iconUrl={iconUrl} serverName={serverName} address={address} placeholder={t("minecraftText.empty")} testId={testId ? `${testId}-preview` : undefined} />
        </section>
        <label className="mm-editor__input">
          <span>{t("minecraftText.source")}</span>
          <textarea
            className="textarea textarea--mono mm-editor__textarea"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            wrap="off"
            spellCheck={false}
            data-testid={testId ? `${testId}-textarea` : undefined}
          />
        </label>
      </div>
    </Dialog>
  );
}

function collectMiniMessageTags(value: string): string[] {
  const found = new Set<string>();
  const re = /<\/?\s*([!#]?[a-zA-Z0-9_.-]+)((?::[^>\s]+)*)/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(value))) {
    if (value[match.index + 1] === "/") continue;
    const rawTag = match[1];
    if (!rawTag) continue;
    let tag = rawTag.toLowerCase();
    if (tag.startsWith("!")) tag = tag.slice(1);
    if (tag.startsWith("#")) tag = "hex";
    tag = TAG_LABELS[tag] ?? tag;
    if (tag === "font") {
      const font = fontFromTagArgs(match[2] ?? "");
      if (font) {
        const info = minecraftFontInfo(font);
        found.add(info ? `font:${info.short}` : "font");
      } else {
        found.add("font");
      }
      continue;
    }
    if (INTERACTION_TAGS.has(tag)) found.add(tag);
  }
  return [...found].sort();
}

function fontFromTagArgs(rawArgs: string): string {
  const parts = rawArgs.split(":").filter(Boolean).map((part) => part.trim()).filter(Boolean);
  if (!parts.length) return "";
  const first = parts[0] ?? "";
  if (parts.length >= 2 && /^[a-z0-9_.-]+$/i.test(first)) {
    return `${first}:${parts.slice(1).join(":")}`;
  }
  return parts.join(":");
}

function annotateMinecraftFontSpans(component: MiniMessageComponent, host: HTMLElement) {
  const spans = Array.from(host.querySelectorAll("span"));
  let index = 0;
  const visit = (node: MiniMessageComponent) => {
    const span = spans[index++] as HTMLElement | undefined;
    const rawFont = node.font()?.asString() ?? "";
    if (span && rawFont) {
      const info = minecraftFontInfo(rawFont);
      span.dataset.mcFontId = info?.id ?? rawFont;
      span.dataset.mcFont = info?.vanilla ? info.short : "custom";
    }
    for (const child of node.children()) visit(child);
  };
  visit(component);
}
