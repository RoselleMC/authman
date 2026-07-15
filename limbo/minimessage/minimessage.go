package minimessage

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	mccolor "go.minekube.com/common/minecraft/color"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/key"
)

// Parse parses a lenient Adventure MiniMessage string into Minekube text
// components. Unknown or malformed tags are kept as literal text.
func Parse(input string) (component.Component, error) {
	nodes, _, err := parseNodes(input, 0, "")
	if err != nil {
		return nil, err
	}
	parts, _ := renderNodes(nodes, component.Style{}, nil)
	return compact(parts), nil
}

// ParseOrText parses input, falling back to a plain text component if parsing
// fails.
func ParseOrText(input string) component.Component {
	c, err := Parse(input)
	if err != nil {
		return &component.Text{Content: input}
	}
	return c
}

// MustParse parses input and panics if parsing fails.
func MustParse(input string) component.Component {
	c, err := Parse(input)
	if err != nil {
		panic(err)
	}
	return c
}

type node struct {
	text     string
	tag      tag
	children []node
}

type tag struct {
	name    string
	args    []string
	literal string
}

func parseNodes(input string, pos int, closing string) ([]node, int, error) {
	var out []node
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		out = append(out, node{text: text.String()})
		text.Reset()
	}

	for pos < len(input) {
		if input[pos] == '\\' {
			if pos+1 < len(input) && (input[pos+1] == '<' || input[pos+1] == '\\') {
				text.WriteByte(input[pos+1])
				pos += 2
				continue
			}
			text.WriteByte(input[pos])
			pos++
			continue
		}
		if input[pos] != '<' {
			r, size := utf8.DecodeRuneInString(input[pos:])
			text.WriteRune(r)
			pos += size
			continue
		}

		end := findTagEnd(input, pos+1)
		if end < 0 {
			text.WriteByte(input[pos])
			pos++
			continue
		}
		raw := input[pos+1 : end]
		parsed, closeTag, ok := parseTag(raw)
		if !ok {
			text.WriteString(input[pos : end+1])
			pos = end + 1
			continue
		}
		if closeTag {
			if closing != "" && tagBase(parsed.name) == closing {
				flush()
				return out, end + 1, nil
			}
			text.WriteString(input[pos : end+1])
			pos = end + 1
			continue
		}
		flush()
		pos = end + 1
		if isSelfClosingTag(parsed.name) || strings.HasSuffix(strings.TrimSpace(raw), "/") {
			out = append(out, node{tag: parsed})
			continue
		}
		children, next, err := parseNodes(input, pos, tagBase(parsed.name))
		if err != nil {
			return nil, 0, err
		}
		out = append(out, node{tag: parsed, children: children})
		pos = next
	}
	flush()
	return out, pos, nil
}

func findTagEnd(input string, pos int) int {
	var quote rune
	for pos < len(input) {
		r, size := utf8.DecodeRuneInString(input[pos:])
		if quote != 0 {
			if r == '\\' {
				pos += size
				if pos < len(input) {
					_, next := utf8.DecodeRuneInString(input[pos:])
					pos += next
				}
				continue
			}
			if r == quote {
				quote = 0
			}
			pos += size
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case '>':
			return pos
		}
		pos += size
	}
	return -1
}

func parseTag(raw string) (tag, bool, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return tag{}, false, false
	}
	if strings.HasSuffix(trimmed, "/") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "/"))
	}
	closeTag := strings.HasPrefix(trimmed, "/")
	if closeTag {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "/"))
	}
	parts, err := splitArgs(trimmed)
	if err != nil || len(parts) == 0 || parts[0] == "" {
		return tag{}, false, false
	}
	name := strings.ToLower(parts[0])
	return tag{name: name, args: parts[1:], literal: raw}, closeTag, true
}

func splitArgs(value string) ([]string, error) {
	var out []string
	var current strings.Builder
	var quote rune
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		if quote != 0 {
			if r == '\\' {
				i += size
				if i < len(value) {
					next, nextSize := utf8.DecodeRuneInString(value[i:])
					current.WriteRune(next)
					i += nextSize
				}
				continue
			}
			if r == quote {
				quote = 0
				i += size
				continue
			}
			current.WriteRune(r)
			i += size
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ':':
			out = append(out, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
		i += size
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted MiniMessage argument")
	}
	out = append(out, current.String())
	return out, nil
}

func tagBase(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i]
	}
	return name
}

func isSelfClosingTag(name string) bool {
	switch tagBase(name) {
	case "reset", "br", "newline", "lang", "tr", "translate", "lang_or", "tr_or", "translate_or", "key", "keybind", "selector", "score", "nbt", "sprite", "head":
		return true
	default:
		return false
	}
}

type transform struct {
	total int
	index int
	color func(i, total int) mccolor.Color
}

func renderNodes(nodes []node, style component.Style, tx *transform) ([]component.Component, component.Style) {
	var out []component.Component
	current := style
	for _, n := range nodes {
		if n.text != "" {
			out = append(out, renderText(n.text, current, tx)...)
			continue
		}
		if n.tag.name == "reset" {
			current = component.Style{}
			continue
		}
		rendered, next := renderTag(n.tag, n.children, current, tx)
		out = append(out, rendered...)
		if next != nil {
			current = *next
		}
	}
	return out, current
}

func renderTag(t tag, children []node, style component.Style, tx *transform) ([]component.Component, *component.Style) {
	name := tagBase(t.name)
	switch name {
	case "br", "newline":
		return renderText("\n", style, tx), nil
	case "lang", "tr", "translate", "lang_or", "tr_or", "translate_or":
		return []component.Component{renderTranslation(t, style)}, nil
	case "key", "keybind", "selector", "score", "nbt", "sprite", "head":
		return renderText("<"+t.literal+">", style, tx), nil
	case "gradient":
		colors := parseColors(t.args)
		if len(colors) < 2 {
			colors = []mccolor.Color{mccolor.White, mccolor.Aqua}
		}
		local := &transform{
			total: visibleRunes(children),
			color: func(i, total int) mccolor.Color {
				return gradientColor(colors, i, total)
			},
		}
		out, _ := renderNodes(children, style, local)
		return out, nil
	case "transition":
		colors := parseColors(t.args)
		if len(colors) == 0 {
			out, _ := renderNodes(children, style, tx)
			return out, nil
		}
		next := style
		next.Color = transitionColor(colors, t.args)
		out, _ := renderNodes(children, next, tx)
		return out, nil
	case "rainbow", "pride":
		reverse := false
		for _, arg := range t.args {
			if strings.Contains(arg, "!") {
				reverse = true
			}
		}
		local := &transform{
			total: visibleRunes(children),
			color: func(i, total int) mccolor.Color {
				if reverse {
					i = total - i - 1
				}
				return rainbowColor(i, total)
			},
		}
		out, _ := renderNodes(children, style, local)
		return out, nil
	}

	next, ok := applyStyleTag(t, style)
	if !ok {
		return renderText("<"+t.literal+">", style, tx), nil
	}
	out, _ := renderNodes(children, next, tx)
	return out, nil
}

func applyStyleTag(t tag, style component.Style) (component.Style, bool) {
	name := tagBase(t.name)
	next := style
	if strings.HasPrefix(name, "!") {
		decoration, ok := decoration(strings.TrimPrefix(name, "!"))
		if !ok {
			return style, false
		}
		next.SetDecoration(decoration, component.False)
		return next, true
	}
	if col, ok := parseColorTag(name, t.args); ok {
		next.Color = col
		return next, true
	}
	if decoration, ok := decoration(name); ok {
		state := component.True
		if len(t.args) > 0 && strings.EqualFold(t.args[0], "false") {
			state = component.False
		}
		next.SetDecoration(decoration, state)
		return next, true
	}
	switch name {
	case "font":
		if len(t.args) == 0 {
			return style, false
		}
		k, err := parseMiniMessageKey(t.args[0])
		if err != nil {
			return style, false
		}
		next.Font = k
	case "insert", "insertion":
		if len(t.args) == 0 {
			return style, false
		}
		value := t.args[0]
		next.Insertion = &value
	case "click":
		if len(t.args) < 2 {
			return style, false
		}
		if click := clickEvent(t.args[0], strings.Join(t.args[1:], ":")); click != nil {
			next.ClickEvent = click
		}
	case "hover":
		if len(t.args) < 2 {
			return style, false
		}
		switch strings.ToLower(t.args[0]) {
		case "show_text":
			next.HoverEvent = component.ShowText(ParseOrText(strings.Join(t.args[1:], ":")))
		case "show_item":
			if hover := showItemHover(strings.Join(t.args[1:], ":")); hover != nil {
				next.HoverEvent = hover
			}
		case "show_entity":
			if hover := showEntityHover(strings.Join(t.args[1:], ":")); hover != nil {
				next.HoverEvent = hover
			}
		}
	default:
		return style, false
	}
	return next, true
}

func renderTranslation(t tag, style component.Style) component.Component {
	name := tagBase(t.name)
	if len(t.args) == 0 {
		return textWithStyle("<"+t.literal+">", style)
	}
	keyName := t.args[0]
	withStart := 1
	if name == "lang_or" || name == "tr_or" || name == "translate_or" {
		withStart = 2
	}
	tr := &component.Translation{Key: keyName, S: style}
	for _, arg := range t.args[withStart:] {
		tr.With = append(tr.With, ParseOrText(arg))
	}
	return tr
}

func showItemHover(value string) component.HoverEvent {
	parts, err := splitArgs(value)
	if err != nil || len(parts) == 0 {
		return nil
	}
	itemKey, err := parseMiniMessageKey(parts[0])
	if err != nil {
		return nil
	}
	count := 1
	if len(parts) > 1 {
		if parsed, err := strconv.Atoi(parts[1]); err == nil {
			count = parsed
		}
	}
	return component.ShowItem(&component.ShowItemHoverType{
		Item:  itemKey,
		Count: count,
	})
}

func showEntityHover(value string) component.HoverEvent {
	parts, err := splitArgs(value)
	if err != nil || len(parts) < 2 {
		return nil
	}
	entityKey, err := parseMiniMessageKey(parts[0])
	if err != nil {
		return nil
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil
	}
	hover := &component.ShowEntityHoverType{Type: entityKey, Id: id}
	if len(parts) > 2 {
		hover.Name = ParseOrText(parts[2])
	}
	return component.ShowEntity(hover)
}

func parseMiniMessageKey(value string) (key.Key, error) {
	if !strings.Contains(value, ":") {
		value = key.MinecraftNamespace + ":" + value
	}
	return key.Parse(value)
}

func renderText(value string, style component.Style, tx *transform) []component.Component {
	if value == "" {
		return nil
	}
	if tx == nil || tx.total <= 1 {
		return []component.Component{textWithStyle(value, style)}
	}
	var out []component.Component
	for _, r := range value {
		next := style
		next.Color = tx.color(tx.index, tx.total)
		tx.index++
		out = append(out, textWithStyle(string(r), next))
	}
	return out
}

func textWithStyle(value string, style component.Style) component.Component {
	return &component.Text{Content: value, S: style}
}

func compact(parts []component.Component) component.Component {
	if len(parts) == 0 {
		return &component.Text{}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return &component.Text{Extra: parts}
}

func visibleRunes(nodes []node) int {
	total := 0
	for _, n := range nodes {
		if n.text != "" {
			total += utf8.RuneCountInString(n.text)
			continue
		}
		total += visibleRunes(n.children)
	}
	if total == 0 {
		return 1
	}
	return total
}

func parseColorTag(name string, args []string) (mccolor.Color, bool) {
	if name == "color" || name == "colour" || name == "c" {
		if len(args) == 0 {
			return nil, false
		}
		return parseColor(args[0])
	}
	return parseColor(name)
}

func parseColors(args []string) []mccolor.Color {
	var colors []mccolor.Color
	for _, arg := range args {
		if col, ok := parseColor(arg); ok {
			colors = append(colors, col)
		}
	}
	return colors
}

func parseColor(value string) (mccolor.Color, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "grey", "gray")
	if strings.HasPrefix(value, "#") {
		col, err := mccolor.Hex(value)
		return col, err == nil
	}
	col, ok := mccolor.Names[value]
	return col, ok
}

func decoration(name string) (component.Decoration, bool) {
	switch name {
	case "bold", "b":
		return component.Bold, true
	case "italic", "em", "i":
		return component.Italic, true
	case "underlined", "underline", "u":
		return component.Underlined, true
	case "strikethrough", "st":
		return component.Strikethrough, true
	case "obfuscated", "obf":
		return component.Obfuscated, true
	default:
		return "", false
	}
}

func clickEvent(action, value string) component.ClickEvent {
	switch strings.ToLower(action) {
	case "open_url":
		return component.OpenUrl(value)
	case "open_file":
		return component.OpenFile(value)
	case "run_command":
		return component.RunCommand(value)
	case "suggest_command":
		return component.SuggestCommand(value)
	case "change_page":
		return component.ChangePage(value)
	case "copy_to_clipboard":
		return component.CopyToClipboard(value)
	case "show_dialog":
		return component.ShowDialog(value)
	case "custom":
		parts, _ := splitArgs(value)
		if len(parts) == 0 {
			return nil
		}
		return component.CustomEvent(parts[0], parts[1:]...)
	default:
		return nil
	}
}

func gradientColor(colors []mccolor.Color, index, total int) mccolor.Color {
	if len(colors) == 0 {
		return mccolor.White
	}
	if len(colors) == 1 || total <= 1 {
		return colors[0]
	}
	pos := float64(index) / float64(total-1) * float64(len(colors)-1)
	left := int(math.Floor(pos))
	right := left + 1
	if right >= len(colors) {
		return colors[len(colors)-1]
	}
	return interpolate(colors[left], colors[right], pos-float64(left))
}

func transitionColor(colors []mccolor.Color, args []string) mccolor.Color {
	phase := 0.0
	for i := len(args) - 1; i >= 0; i-- {
		if v, err := strconv.ParseFloat(args[i], 64); err == nil {
			phase = math.Max(-1, math.Min(1, v))
			break
		}
	}
	index := int(math.Round(((phase + 1) / 2) * float64(len(colors)-1)))
	if index < 0 {
		index = 0
	}
	if index >= len(colors) {
		index = len(colors) - 1
	}
	return colors[index]
}

func rainbowColor(index, total int) mccolor.Color {
	if total <= 1 {
		return mccolor.Red
	}
	h := float64(index) / float64(total)
	r, g, b := hsvToRGB(h, 1, 1)
	return mccolor.HexInt((r << 16) | (g << 8) | b)
}

func interpolate(a, b mccolor.Color, t float64) mccolor.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	r := uint8(float64(ar>>8)*(1-t) + float64(br>>8)*t)
	g := uint8(float64(ag>>8)*(1-t) + float64(bg>>8)*t)
	blue := uint8(float64(ab>>8)*(1-t) + float64(bb>>8)*t)
	return mccolor.HexInt((int(r) << 16) | (int(g) << 8) | int(blue))
}

func hsvToRGB(h, s, v float64) (int, int, int) {
	i := int(h * 6)
	f := h*6 - float64(i)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return int(r * 255), int(g * 255), int(b * 255)
}
