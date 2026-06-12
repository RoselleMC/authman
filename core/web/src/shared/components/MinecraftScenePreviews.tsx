import type { ReactNode } from "react";
import { MinecraftRichTextPreview } from "./MinecraftRichTextPreview";
import { cx } from "../utils/cx";

/**
 * In-game scene mockups (1.21.6 dialog screen, disconnect screen, chat, HUD)
 * used by the Player Messages editors for live MiniMessage previews. All text
 * props are MiniMessage source strings with sample placeholder values already
 * substituted by the caller.
 */

interface SelectableProps {
  selectKey?: string;
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
  className?: string;
  children: ReactNode;
  testId?: string;
}

function Selectable({ selectKey, selectedKey, onSelect, className, children, testId }: SelectableProps) {
  if (!selectKey || !onSelect) {
    return <div className={className} data-testid={testId}>{children}</div>;
  }
  return (
    <button
      type="button"
      className={cx(className, "mc-sel", selectedKey === selectKey ? "is-selected" : null)}
      onClick={() => onSelect(selectKey)}
      data-testid={testId}
    >
      {children}
    </button>
  );
}

export function MinecraftSceneFrame({ children, className, testId }: { children: ReactNode; className?: string; testId?: string }) {
  return (
    <div className={cx("mc-scene", className)} data-testid={testId}>
      {children}
    </div>
  );
}

export type DialogPreviewBody =
  | { key: string; kind: "text"; text: string }
  | { key: string; kind: "item"; item: string; count: number; description?: string };

export type DialogPreviewInput =
  | { key: string; kind: "text"; label: string; labelVisible: boolean; multiline?: boolean }
  | { key: string; kind: "boolean"; label: string; value: boolean; onValue: string }
  | { key: string; kind: "option"; label: string; labelVisible: boolean; display: string }
  | { key: string; kind: "range"; label: string; valueText: string; ratio: number };

export interface DialogPreviewButton {
  key: string;
  label: string;
  tooltip?: string;
}

interface DialogPreviewProps {
  title: string;
  body: DialogPreviewBody[];
  inputs: DialogPreviewInput[];
  buttons: DialogPreviewButton[];
  columns?: number;
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
  footer?: ReactNode;
  compact?: boolean;
  testId?: string;
}

export function MinecraftDialogPreview({ title, body, inputs, buttons, columns, selectedKey, onSelect, footer, compact, testId }: DialogPreviewProps) {
  const buttonColumns = buttons.length > 1 ? Math.max(1, Math.min(columns || 2, buttons.length)) : 1;
  return (
    <MinecraftSceneFrame className={cx("mc-scene--dialog", compact ? "mc-scene--compact" : null)} testId={testId}>
      <div className="mc-dialog">
        <Selectable selectKey="dialog" selectedKey={selectedKey} onSelect={onSelect} className="mc-dialog__title" testId={testId ? `${testId}-title` : undefined}>
          <MinecraftRichTextPreview value={title} metadata={false} />
        </Selectable>
        {body.map((el) => (
          <Selectable
            key={el.key}
            selectKey={el.key}
            selectedKey={selectedKey}
            onSelect={onSelect}
            className={cx("mc-dialog__line", el.kind === "item" ? "mc-dialog__line--item" : null)}
            testId={testId ? `${testId}-${el.key}` : undefined}
          >
            {el.kind === "item" ? (
              <span className="mc-dialog__item">
                <span className="mc-dialog__item-frame" aria-hidden="true">
                  <span className="mc-dialog__item-cube" />
                  {el.count > 1 ? <span className="mc-dialog__item-count">{el.count}</span> : null}
                </span>
                <span className="mc-dialog__item-main">
                  <span className="mc-dialog__item-id">{el.item || "minecraft:?"}</span>
                  {el.description ? <MinecraftRichTextPreview value={el.description} metadata={false} /> : null}
                </span>
              </span>
            ) : (
              <MinecraftRichTextPreview value={el.text} metadata={false} />
            )}
          </Selectable>
        ))}
        {inputs.map((input) => (
          <Selectable
            key={input.key}
            selectKey={input.key}
            selectedKey={selectedKey}
            onSelect={onSelect}
            className="mc-dialog__input"
            testId={testId ? `${testId}-${input.key}` : undefined}
          >
            {input.kind === "boolean" ? (
              <span className="mc-dialog__inline">
                <span className="mc-dialog__input-label">
                  <MinecraftRichTextPreview value={input.label} metadata={false} />
                </span>
                <span className={cx("mc-dialog__checkbox", input.value ? "is-on" : null)} aria-hidden="true">
                  {input.value ? "✔" : ""}
                </span>
                {input.onValue ? <span className="mc-dialog__bool-value">{input.onValue}</span> : null}
              </span>
            ) : input.kind === "option" ? (
              <span className="mc-dialog__option-btn">
                {input.labelVisible ? (
                  <>
                    <MinecraftRichTextPreview value={input.label} metadata={false} />
                    <span className="mc-dialog__option-sep">: </span>
                  </>
                ) : null}
                <MinecraftRichTextPreview value={input.display} metadata={false} />
              </span>
            ) : input.kind === "range" ? (
              <span className="mc-dialog__range">
                <span className="mc-dialog__range-track" aria-hidden="true">
                  <span className="mc-dialog__range-fill" style={{ width: `${Math.round(Math.max(0, Math.min(1, input.ratio)) * 100)}%` }} />
                  <span className="mc-dialog__range-handle" style={{ left: `${Math.round(Math.max(0, Math.min(1, input.ratio)) * 100)}%` }} />
                </span>
                <span className="mc-dialog__range-text">
                  <MinecraftRichTextPreview value={input.label} metadata={false} />
                  <span className="mc-dialog__option-sep">: </span>
                  <span>{input.valueText}</span>
                </span>
              </span>
            ) : (
              <>
                {input.labelVisible ? (
                  <span className="mc-dialog__input-label">
                    <MinecraftRichTextPreview value={input.label} metadata={false} />
                  </span>
                ) : null}
                <span className={cx("mc-dialog__input-box", input.multiline ? "mc-dialog__input-box--multiline" : null)} aria-hidden="true">
                  <span className="mc-dialog__caret" />
                </span>
              </>
            )}
          </Selectable>
        ))}
        <div className={cx("mc-dialog__buttons", buttonColumns > 1 ? "mc-dialog__buttons--grid" : null)} style={buttonColumns > 1 ? { gridTemplateColumns: `repeat(${buttonColumns}, minmax(0, 1fr))` } : undefined}>
          {buttons.map((button) => (
            <Selectable
              key={button.key}
              selectKey={button.key}
              selectedKey={selectedKey}
              onSelect={onSelect}
              className="mc-dialog__btn"
              testId={testId ? `${testId}-${button.key}` : undefined}
            >
              <MinecraftRichTextPreview value={button.label} metadata={false} />
            </Selectable>
          ))}
        </div>
        {footer}
      </div>
    </MinecraftSceneFrame>
  );
}

export function MinecraftKickPreview({ value, compact, testId }: { value: string; compact?: boolean; testId?: string }) {
  return (
    <MinecraftSceneFrame className={cx("mc-scene--kick", compact ? "mc-scene--compact" : null)} testId={testId}>
      <div className="mc-kick">
        <div className="mc-kick__heading">Disconnected</div>
        <div className="mc-kick__message">
          <MinecraftRichTextPreview value={value} metadata={false} testId={testId ? `${testId}-rich` : undefined} />
        </div>
        <div className="mc-kick__btn" aria-hidden="true">Back to server list</div>
      </div>
    </MinecraftSceneFrame>
  );
}

export function MinecraftChatPreview({ value, testId }: { value: string; testId?: string }) {
  return (
    <MinecraftSceneFrame className="mc-scene--chat" testId={testId}>
      <div className="mc-chat">
        <div className="mc-chat__line">
          <MinecraftRichTextPreview value={value} metadata={false} testId={testId ? `${testId}-rich` : undefined} />
        </div>
      </div>
    </MinecraftSceneFrame>
  );
}

interface HudPreviewProps {
  title: string;
  subtitle: string;
  actionbar: string;
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
  compact?: boolean;
  testId?: string;
}

export function MinecraftHudPreview({ title, subtitle, actionbar, selectedKey, onSelect, compact, testId }: HudPreviewProps) {
  return (
    <MinecraftSceneFrame className={cx("mc-scene--hud", compact ? "mc-scene--compact" : null)} testId={testId}>
      <div className="mc-hud">
        <Selectable selectKey="limbo.success.title" selectedKey={selectedKey} onSelect={onSelect} className="mc-hud__title" testId={testId ? `${testId}-title` : undefined}>
          <MinecraftRichTextPreview value={title} metadata={false} />
        </Selectable>
        <Selectable selectKey="limbo.success.subtitle" selectedKey={selectedKey} onSelect={onSelect} className="mc-hud__subtitle" testId={testId ? `${testId}-subtitle` : undefined}>
          <MinecraftRichTextPreview value={subtitle} metadata={false} />
        </Selectable>
        <Selectable selectKey="limbo.success.actionbar" selectedKey={selectedKey} onSelect={onSelect} className="mc-hud__actionbar" testId={testId ? `${testId}-actionbar` : undefined}>
          <MinecraftRichTextPreview value={actionbar} metadata={false} />
        </Selectable>
      </div>
    </MinecraftSceneFrame>
  );
}
