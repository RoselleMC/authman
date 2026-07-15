package dialog

import (
	"bytes"
	"encoding/json"

	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/component/codec"
)

const (
	TypeNotice       = "minecraft:notice"
	TypeConfirmation = "minecraft:confirmation"
	TypeMultiAction  = "minecraft:multi_action"
	TypeDialogList   = "minecraft:dialog_list"
	TypeServerLinks  = "minecraft:server_links"

	BodyPlainMessage = "minecraft:plain_message"
	BodyItem         = "minecraft:item"

	InputText         = "minecraft:text"
	InputBoolean      = "minecraft:boolean"
	InputSingleOption = "minecraft:single_option"
	InputNumberRange  = "minecraft:number_range"

	ActionRunCommand        = "run_command"
	ActionSuggestCommand    = "suggest_command"
	ActionOpenURL           = "open_url"
	ActionCopyToClipboard   = "copy_to_clipboard"
	ActionChangePage        = "change_page"
	ActionShowDialog        = "show_dialog"
	ActionCustom            = "custom"
	ActionDynamicRunCommand = "minecraft:dynamic/run_command"
	ActionDynamicCustom     = "minecraft:dynamic/custom"

	AfterActionClose           = "close"
	AfterActionNone            = "none"
	AfterActionWaitForResponse = "wait_for_response"
)

// Dialog is any vanilla dialog payload that can be encoded as JSON, then sent
// to dialog-aware clients as anonymous NBT.
type Dialog interface {
	json.Marshaler
}

// Raw is an escape hatch for exact vanilla fields, future dialog extensions, or
// data loaded directly from configuration.
type Raw map[string]any

// MarshalJSON implements Dialog.
func (r Raw) MarshalJSON() ([]byte, error) {
	return json.Marshal(normalizeJSONValue(r))
}

type componentJSON struct {
	value component.Component
}

func (c componentJSON) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	encoder := codec.Json{
		EmitCompactTextComponent:                false,
		EmitHoverShowEntityIdAsIntArray:         true,
		EmitDefaultItemHoverQuantity:            true,
		EmitHoverShowEntityKeyAsTypeAndUuidAsId: false,
		NoDownsampleColor:                       true,
		StdJson:                                 true,
	}
	if err := encoder.Marshal(&out, c.value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func normalizeJSONValue(value any) any {
	if value == nil {
		return nil
	}
	if rich, ok := value.(component.Component); ok {
		return componentJSON{value: rich}
	}
	switch typed := value.(type) {
	case Raw:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = normalizeJSONValue(child)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = normalizeJSONValue(child)
		}
		return out
	case []Raw:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, normalizeJSONValue(child))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, normalizeJSONValue(child))
		}
		return out
	default:
		return typed
	}
}

// Text returns a rich text component for plain string literals.
func Text(value string) component.Component {
	return &component.Text{Content: value}
}

// Bool returns a bool pointer for optional vanilla fields.
func Bool(value bool) *bool {
	return &value
}

// Float returns a float pointer for optional numeric vanilla fields.
func Float(value float64) *float64 {
	return &value
}

// Common contains fields shared by vanilla dialog types.
type Common struct {
	Title              component.Component
	ExternalTitle      component.Component
	Body               []Raw
	Inputs             []Raw
	CanCloseWithEscape *bool
	Pause              *bool
	AfterAction        string
}

func (c Common) apply(out Raw) Raw {
	if c.Title != nil {
		out["title"] = c.Title
	}
	if c.ExternalTitle != nil {
		out["external_title"] = c.ExternalTitle
	}
	if len(c.Body) > 0 {
		out["body"] = c.Body
	}
	if len(c.Inputs) > 0 {
		out["inputs"] = c.Inputs
	}
	if c.CanCloseWithEscape != nil {
		out["can_close_with_escape"] = *c.CanCloseWithEscape
	}
	if c.Pause != nil {
		out["pause"] = *c.Pause
	}
	if c.AfterAction != "" {
		out["after_action"] = c.AfterAction
	}
	return out
}

// Notice creates a minecraft:notice dialog.
func Notice(common Common, action ActionButton) Raw {
	out := common.apply(Raw{"type": TypeNotice})
	if raw := action.raw(); raw != nil {
		out["action"] = raw
	}
	return out
}

// Confirmation creates a minecraft:confirmation dialog.
func Confirmation(common Common, yes, no ActionButton) Raw {
	out := common.apply(Raw{"type": TypeConfirmation})
	out["yes"] = yes.raw()
	out["no"] = no.raw()
	return out
}

// MultiAction creates a minecraft:multi_action dialog.
func MultiAction(common Common, actions []ActionButton, columns int) Raw {
	return MultiActionWithExit(common, actions, ActionButton{}, columns)
}

// MultiActionWithExit creates a minecraft:multi_action dialog with an optional
// footer exit action.
func MultiActionWithExit(common Common, actions []ActionButton, exitAction ActionButton, columns int) Raw {
	out := common.apply(Raw{"type": TypeMultiAction})
	if len(actions) > 0 {
		raw := make([]Raw, 0, len(actions))
		for _, action := range actions {
			raw = append(raw, action.raw())
		}
		out["actions"] = raw
	}
	if raw := exitAction.raw(); raw != nil {
		out["exit_action"] = raw
	}
	if columns > 0 {
		out["columns"] = columns
	}
	return out
}

// DialogList creates a minecraft:dialog_list dialog. The dialogs value may be
// a vanilla registry reference list, raw dialog payloads, or any future shape.
func DialogList(common Common, dialogs any, action ActionButton, columns, buttonWidth int) Raw {
	out := common.apply(Raw{"type": TypeDialogList})
	if dialogs != nil {
		out["dialogs"] = dialogs
	}
	if raw := action.raw(); raw != nil {
		out["exit_action"] = raw
	}
	if columns > 0 {
		out["columns"] = columns
	}
	if buttonWidth > 0 {
		out["button_width"] = buttonWidth
	}
	return out
}

// ServerLinks creates a minecraft:server_links dialog.
func ServerLinks(common Common, action ActionButton) Raw {
	return ServerLinksWithOptions(common, action, 0, 0)
}

// ServerLinksWithOptions creates a minecraft:server_links dialog with layout
// controls.
func ServerLinksWithOptions(common Common, action ActionButton, columns, buttonWidth int) Raw {
	out := common.apply(Raw{"type": TypeServerLinks})
	if raw := action.raw(); raw != nil {
		out["exit_action"] = raw
	}
	if columns > 0 {
		out["columns"] = columns
	}
	if buttonWidth > 0 {
		out["button_width"] = buttonWidth
	}
	return out
}

// PlainMessage creates a minecraft:plain_message body.
func PlainMessage(contents component.Component, width int) Raw {
	out := Raw{"type": BodyPlainMessage, "contents": contents}
	if width > 0 {
		out["width"] = width
	}
	return out
}

// Item creates a minecraft:item body. The item value is intentionally raw so
// callers can provide the exact version-specific item stack shape.
func Item(item any, description component.Component, showDecorations, showTooltip *bool, width, height int) Raw {
	return ItemWithDescription(item, description, showDecorations, showTooltip, width, height)
}

// ItemDescription creates the optional rich description object for item bodies.
func ItemDescription(contents component.Component, width int) Raw {
	out := Raw{}
	if contents != nil {
		out["contents"] = contents
	}
	if width > 0 {
		out["width"] = width
	}
	return out
}

// ItemWithDescription creates a minecraft:item body. description may be either
// a text component or the object returned by ItemDescription.
func ItemWithDescription(item any, description any, showDecorations, showTooltip *bool, width, height int) Raw {
	out := Raw{"type": BodyItem, "item": item}
	if description != nil {
		out["description"] = description
	}
	if showDecorations != nil {
		out["show_decorations"] = *showDecorations
	}
	if showTooltip != nil {
		out["show_tooltip"] = *showTooltip
	}
	if width > 0 {
		out["width"] = width
	}
	if height > 0 {
		out["height"] = height
	}
	return out
}

// TextInputOptions contains optional minecraft:text input fields.
type TextInputOptions struct {
	Initial         string
	MaxLength       int
	Width           int
	LabelVisible    *bool
	Multiline       bool
	MultilineLines  int
	MultilineHeight int
}

// TextInput creates a minecraft:text input.
func TextInput(key string, label component.Component, options TextInputOptions) Raw {
	out := Raw{"type": InputText, "key": key}
	if label != nil {
		out["label"] = label
	}
	if options.Initial != "" {
		out["initial"] = options.Initial
	}
	if options.MaxLength > 0 {
		out["max_length"] = options.MaxLength
	}
	if options.Width > 0 {
		out["width"] = options.Width
	}
	if options.LabelVisible != nil {
		out["label_visible"] = *options.LabelVisible
	}
	if options.Multiline {
		multiline := Raw{}
		if options.MultilineLines > 0 {
			multiline["max_lines"] = options.MultilineLines
		}
		if options.MultilineHeight > 0 {
			multiline["height"] = options.MultilineHeight
		}
		out["multiline"] = multiline
	}
	return out
}

// BooleanInput creates a minecraft:boolean input.
func BooleanInput(key string, label component.Component, initial bool, onTrue, onFalse string) Raw {
	out := Raw{"type": InputBoolean, "key": key}
	if label != nil {
		out["label"] = label
	}
	out["initial"] = initial
	if onTrue != "" {
		out["on_true"] = onTrue
	}
	if onFalse != "" {
		out["on_false"] = onFalse
	}
	return out
}

// Option describes one selectable option for a minecraft:single_option input.
type Option struct {
	ID      string
	Display component.Component
	Initial bool
}

func (o Option) raw() Raw {
	out := Raw{"id": o.ID}
	if o.Display != nil {
		out["display"] = o.Display
	}
	if o.Initial {
		out["initial"] = true
	}
	return out
}

// SingleOptionInput creates a minecraft:single_option input.
func SingleOptionInput(key string, label component.Component, options []Option, initial string, width int) Raw {
	return SingleOptionInputWithOptions(key, label, options, SingleOptionInputOptions{
		Initial: initial,
		Width:   width,
	})
}

// SingleOptionInputOptions contains optional minecraft:single_option fields.
type SingleOptionInputOptions struct {
	Initial      string
	Width        int
	LabelVisible *bool
}

// SingleOptionInputWithOptions creates a minecraft:single_option input.
func SingleOptionInputWithOptions(key string, label component.Component, options []Option, inputOptions SingleOptionInputOptions) Raw {
	out := Raw{"type": InputSingleOption, "key": key}
	if label != nil {
		out["label"] = label
	}
	if inputOptions.LabelVisible != nil {
		out["label_visible"] = *inputOptions.LabelVisible
	}
	if len(options) > 0 {
		raw := make([]Raw, 0, len(options))
		for _, option := range options {
			if inputOptions.Initial != "" && option.ID == inputOptions.Initial {
				option.Initial = true
			}
			raw = append(raw, option.raw())
		}
		out["options"] = raw
	}
	if inputOptions.Width > 0 {
		out["width"] = inputOptions.Width
	}
	return out
}

// NumberRangeOptions contains optional minecraft:number_range input fields.
type NumberRangeOptions struct {
	Start       float64
	End         float64
	Initial     *float64
	Step        *float64
	Width       int
	LabelFormat string
}

// NumberRangeInput creates a minecraft:number_range input.
func NumberRangeInput(key string, label component.Component, options NumberRangeOptions) Raw {
	out := Raw{"type": InputNumberRange, "key": key, "start": options.Start, "end": options.End}
	if label != nil {
		out["label"] = label
	}
	if options.LabelFormat != "" {
		out["label_format"] = options.LabelFormat
	}
	if options.Initial != nil {
		out["initial"] = *options.Initial
	}
	if options.Step != nil {
		out["step"] = *options.Step
	}
	if options.Width > 0 {
		out["width"] = options.Width
	}
	return out
}

// ActionButton is a vanilla dialog button with an optional click action.
type ActionButton struct {
	Label   component.Component
	Tooltip component.Component
	Width   int
	Action  Raw
}

func Button(label component.Component, action Raw) ActionButton {
	return ActionButton{Label: label, Action: action}
}

func (b ActionButton) raw() Raw {
	if b.Label == nil && b.Tooltip == nil && b.Width == 0 && b.Action == nil {
		return nil
	}
	out := Raw{}
	if b.Label != nil {
		out["label"] = b.Label
	}
	if b.Tooltip != nil {
		out["tooltip"] = b.Tooltip
	}
	if b.Width > 0 {
		out["width"] = b.Width
	}
	if b.Action != nil {
		out["action"] = b.Action
	}
	return out
}

func RunCommand(command string) Raw {
	return Raw{"type": ActionRunCommand, "command": command}
}

func SuggestCommand(command string) Raw {
	return Raw{"type": ActionSuggestCommand, "command": command}
}

func OpenURL(url string) Raw {
	return Raw{"type": ActionOpenURL, "url": url}
}

func CopyToClipboard(value string) Raw {
	return Raw{"type": ActionCopyToClipboard, "value": value}
}

func ChangePage(page int) Raw {
	return Raw{"type": ActionChangePage, "page": page}
}

func ShowDialog(dialog any) Raw {
	return Raw{"type": ActionShowDialog, "dialog": dialog}
}

func Custom(id string, payload any) Raw {
	out := Raw{"type": ActionCustom, "id": id}
	if payload != nil {
		out["payload"] = payload
	}
	return out
}

func DynamicRunCommand(template string) Raw {
	return Raw{"type": ActionDynamicRunCommand, "template": template}
}

func DynamicCustom(id string, additions any) Raw {
	out := Raw{"type": ActionDynamicCustom, "id": id}
	if additions != nil {
		out["additions"] = additions
	}
	return out
}
