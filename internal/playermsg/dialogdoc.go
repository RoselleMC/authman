package playermsg

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Dialog document model v2: a fully free-form description of an auth dialog.
// Admins may compose any combination of body elements, inputs, and buttons the
// vanilla 1.21.6+ dialog system supports; the functional auth contract is
// expressed through role bindings (an input bound as the password field, a
// button whose action submits the flow) instead of fixed slots.

const DialogDocVersion = 2

// Element visibility conditions, evaluated by the limbo portal at show time.
const (
	WhenError = "error"
)

// Body element kinds.
const (
	BodyText = "text"
	BodyItem = "item"
)

// Input kinds.
const (
	InputText    = "text"
	InputBoolean = "boolean"
	InputOption  = "option"
	InputRange   = "range"
)

// Input roles binding functional meaning to free-form components.
const (
	RolePassword      = "password"
	RoleConfirm       = "confirm"
	RoleProfileName   = "profile_name"   // text input carrying the new profile's protocol name
	RoleProfileChoice = "profile_choice" // option input listing the passport's profiles (runtime-populated)
)

// Button action kinds.
const (
	ActionSubmit          = "submit"
	ActionOpenURL         = "open_url"
	ActionCopyToClipboard = "copy_to_clipboard"
	ActionOpenScreen      = "open_screen" // switch to another auth dialog screen
)

// Dialog after_action behaviours. Close is intentionally unsupported: the auth
// flow must keep a way to respond or retry.
const (
	AfterWaitForResponse = "wait_for_response"
	AfterNone            = "none"
)

type DialogBody struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	When string `json:"when,omitempty"`
	// kind: text
	Text  string `json:"text,omitempty"`
	Width int    `json:"width,omitempty"`
	// kind: item
	Item            string `json:"item,omitempty"`
	Count           int    `json:"count,omitempty"`
	Description     string `json:"description,omitempty"`
	ShowTooltip     *bool  `json:"show_tooltip,omitempty"`
	ShowDecorations bool   `json:"show_decorations,omitempty"`
	Height          int    `json:"height,omitempty"`
}

type DialogOption struct {
	ID      string `json:"id"`
	Display string `json:"display"`
	Initial bool   `json:"initial,omitempty"`
}

type DialogInput struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Role         string `json:"role,omitempty"`
	Key          string `json:"key"`
	Label        string `json:"label"`
	LabelVisible *bool  `json:"label_visible,omitempty"`
	When         string `json:"when,omitempty"`
	Width        int    `json:"width,omitempty"`
	// kind: text
	Initial        string `json:"initial,omitempty"`
	MaxLength      int    `json:"max_length,omitempty"`
	Multiline      bool   `json:"multiline,omitempty"`
	MultilineLines int    `json:"multiline_lines,omitempty"`
	// kind: boolean
	InitialBool bool   `json:"initial_bool,omitempty"`
	OnTrue      string `json:"on_true,omitempty"`
	OnFalse     string `json:"on_false,omitempty"`
	// kind: option
	Options []DialogOption `json:"options,omitempty"`
	// kind: range
	Start       float64  `json:"start"`
	End         float64  `json:"end"`
	Step        *float64 `json:"step,omitempty"`
	InitialNum  *float64 `json:"initial_num,omitempty"`
	LabelFormat string   `json:"label_format,omitempty"`
}

type DialogAction struct {
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	Value  string `json:"value,omitempty"`
	Screen string `json:"screen,omitempty"`
}

type DialogButton struct {
	ID      string       `json:"id"`
	Label   string       `json:"label"`
	Tooltip string       `json:"tooltip,omitempty"`
	Width   int          `json:"width,omitempty"`
	When    string       `json:"when,omitempty"`
	Action  DialogAction `json:"action"`
}

type DialogDoc struct {
	Version            int            `json:"version"`
	Title              string         `json:"title"`
	ExternalTitle      string         `json:"external_title,omitempty"`
	CanCloseWithEscape bool           `json:"can_close_with_escape,omitempty"`
	Pause              bool           `json:"pause,omitempty"`
	AfterAction        string         `json:"after_action,omitempty"`
	Columns            int            `json:"columns,omitempty"`
	Body               []DialogBody   `json:"body"`
	Inputs             []DialogInput  `json:"inputs"`
	Buttons            []DialogButton `json:"buttons"`
}

// RoleKey returns the payload key of the input bound to the given role.
func (d DialogDoc) RoleKey(role string) string {
	for _, input := range d.Inputs {
		if input.Role == role {
			return strings.TrimSpace(input.Key)
		}
	}
	return ""
}

// DialogState captures the runtime branch of one dialog render.
type DialogState struct {
	AuthRequired bool
	Verified     bool
	HasError     bool
}

// VisibleWhen evaluates an element visibility condition against runtime state.
func VisibleWhen(when string, st DialogState) bool {
	switch when {
	case "", WhenAlways:
		return true
	case WhenAuthRequired:
		return st.AuthRequired
	case WhenPremiumPassthrough:
		return !st.AuthRequired
	case WhenPremiumUnverified:
		return st.AuthRequired && !st.Verified
	case WhenError:
		return st.HasError
	default:
		return false
	}
}

// DefaultDialog returns the built-in document for a dialog screen.
func DefaultDialog(screen string) DialogDoc {
	if screen == ScreenProfileCreate {
		return DialogDoc{
			Version: DialogDocVersion,
			Title:   "Create your profile",
			Body: []DialogBody{
				{ID: "intro", Kind: BodyText, Text: "Choose the name other players will see in game. ({count}/{max} profiles)", Width: 240},
				{ID: "error", Kind: BodyText, Text: "Error: {error}", Width: 240, When: WhenError},
			},
			Inputs: []DialogInput{
				{ID: "profile-name", Kind: InputText, Role: RoleProfileName, Key: "profile_name", Label: "Profile name", MaxLength: 16, Width: 240},
			},
			Buttons: []DialogButton{
				{ID: "submit", Label: "Create profile", Action: DialogAction{Kind: ActionSubmit}},
			},
		}
	}
	if screen == ScreenProfileSelect {
		return DialogDoc{
			Version: DialogDocVersion,
			Title:   "Choose your profile",
			Body: []DialogBody{
				{ID: "intro", Kind: BodyText, Text: "Pick the profile to join with. ({count}/{max} profiles)", Width: 240},
				{ID: "error", Kind: BodyText, Text: "Error: {error}", Width: 240, When: WhenError},
			},
			Inputs: []DialogInput{
				{ID: "profile-choice", Kind: InputOption, Role: RoleProfileChoice, Key: "profile_choice", Label: "Profile", Width: 240},
			},
			Buttons: []DialogButton{
				{ID: "submit", Label: "Join", Action: DialogAction{Kind: ActionSubmit}},
				{ID: "create-new", Label: "New profile", Action: DialogAction{Kind: ActionOpenScreen, Screen: ScreenProfileCreate}},
			},
			Columns: 2,
		}
	}
	if screen == ScreenRegister {
		return DialogDoc{
			Version: DialogDocVersion,
			Title:   "Register Authman",
			Body: []DialogBody{
				{ID: "intro", Kind: BodyText, Text: "Create an Authman offline passport for this name. Use at least 8 characters.", Width: 240},
				{ID: "error", Kind: BodyText, Text: "Error: {error}", Width: 240, When: WhenError},
			},
			Inputs: []DialogInput{
				{ID: "password", Kind: InputText, Role: RolePassword, Key: "password", Label: "Password", MaxLength: 128, Width: 240},
				{ID: "confirm", Kind: InputText, Role: RoleConfirm, Key: "confirm_password", Label: "Confirm password", MaxLength: 128, Width: 240},
			},
			Buttons: []DialogButton{
				{ID: "submit", Label: "Register", Action: DialogAction{Kind: ActionSubmit}},
			},
		}
	}
	return DialogDoc{
		Version: DialogDocVersion,
		Title:   "Authman",
		Body: []DialogBody{
			{ID: "intro", Kind: BodyText, Text: "Authenticate with Authman, then transfer to the downstream server.", Width: 240},
			{ID: "error", Kind: BodyText, Text: "Error: {error}", Width: 240, When: WhenError},
			{ID: "premium-unverified", Kind: BodyText, Text: "Your premium session is not verified. Authman is using offline password authentication for this login.", Width: 240, When: WhenPremiumUnverified},
			{ID: "premium-passthrough", Kind: BodyText, Text: "This premium passport can continue without a password.", Width: 240, When: WhenPremiumPassthrough},
		},
		Inputs: []DialogInput{
			{ID: "password", Kind: InputText, Role: RolePassword, Key: "password", Label: "Password", MaxLength: 128, Width: 240, When: WhenAuthRequired},
		},
		Buttons: []DialogButton{
			{ID: "submit", Label: "Login", Action: DialogAction{Kind: ActionSubmit}},
		},
	}
}

const (
	maxBodyElements = 12
	maxInputs       = 8
	maxButtons      = 6
	maxOptions      = 16
	maxColumns      = 4
	maxMultiline    = 20
	// Vanilla ItemBody's codec only accepts width/height in 1..256
	// (ExtraCodecs.intRange(1, 256)); larger values disconnect the client.
	maxItemDimension = 256
	// Vanilla text inputs default max_length to 32 when omitted.
	defaultTextMaxLength = 32
)

var (
	itemIDPattern = regexp.MustCompile(`^[a-z0-9_.-]+(:[a-z0-9_/.-]+)?$`)
	keyPattern    = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
)

func validVisibility(screen, when string) bool {
	switch when {
	case "", WhenAlways, WhenError:
		return true
	case WhenAuthRequired, WhenPremiumPassthrough, WhenPremiumUnverified:
		return screen == ScreenLogin
	default:
		return false
	}
}

// requiredRoles maps each screen to the roles it must bind exactly once, and
// allowedRoles to the set of roles that may appear on it at all.
func requiredRoles(screen string) []string {
	switch screen {
	case ScreenRegister:
		return []string{RolePassword, RoleConfirm}
	case ScreenProfileCreate:
		return []string{RoleProfileName}
	case ScreenProfileSelect:
		return []string{RoleProfileChoice}
	default:
		return []string{RolePassword}
	}
}

func roleAllowed(screen, role string) bool {
	for _, required := range requiredRoles(screen) {
		if required == role {
			return true
		}
	}
	return false
}

// openScreenTargets lists which screens a button may switch to from a screen.
func openScreenTargets(screen string) []string {
	switch screen {
	case ScreenProfileSelect:
		return []string{ScreenProfileCreate}
	case ScreenProfileCreate:
		return []string{ScreenProfileSelect}
	default:
		return nil
	}
}

// functionalVisibility reports whether a condition keeps a password-role
// input reachable in every branch that asks for a password.
func functionalVisibility(when string) bool {
	switch when {
	case "", WhenAlways, WhenAuthRequired:
		return true
	default:
		return false
	}
}

// alwaysVisible reports whether a condition keeps an element visible in every
// branch. Submit buttons must satisfy this: premium-passthrough players have
// AuthRequired=false, so an auth_required submit button would vanish and leave
// the dialog without any way to continue.
func alwaysVisible(when string) bool {
	switch when {
	case "", WhenAlways:
		return true
	default:
		return false
	}
}

// ValidateDialog checks a free-form dialog document and returns errors keyed
// by a field path such as "title", "body[2].text", or "buttons".
func ValidateDialog(screen string, doc DialogDoc) map[string]string {
	errs := map[string]string{}

	if strings.TrimSpace(doc.Title) == "" {
		errs["title"] = "title is required"
	} else if msg := validateText(doc.Title, maxTitleLength); msg != "" {
		errs["title"] = msg
	}
	if strings.TrimSpace(doc.ExternalTitle) != "" {
		if msg := validateText(doc.ExternalTitle, maxTitleLength); msg != "" {
			errs["external_title"] = msg
		}
	}
	switch doc.AfterAction {
	case "", AfterWaitForResponse, AfterNone:
	default:
		errs["after_action"] = "after action must be wait_for_response or none"
	}
	if doc.Pause && doc.AfterAction == AfterNone {
		// Vanilla's CommonDialogData codec rejects pause=true with an
		// after_action that does not unpause; the client would disconnect.
		errs["pause"] = "dialogs that pause the game cannot keep the dialog open after a click; disable pause or use wait_for_response"
	}
	if doc.Columns < 0 || doc.Columns > maxColumns {
		errs["columns"] = fmt.Sprintf("columns must be between 0 and %d", maxColumns)
	}

	seenIDs := map[string]bool{}
	checkID := func(path, id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			errs[path+".id"] = "element id is required"
			return
		}
		if seenIDs[id] {
			errs[path+".id"] = "element id must be unique"
		}
		seenIDs[id] = true
	}

	if len(doc.Body) > maxBodyElements {
		errs["body"] = fmt.Sprintf("at most %d body elements are allowed", maxBodyElements)
	}
	for i, body := range doc.Body {
		path := fmt.Sprintf("body[%d]", i)
		checkID(path, body.ID)
		if !validVisibility(screen, body.When) {
			errs[path+".when"] = "unsupported visibility condition"
		}
		if body.Width < 0 || body.Width > maxElemWidth {
			errs[path+".width"] = "width must be between 0 and 1024"
		}
		switch body.Kind {
		case BodyText:
			if strings.TrimSpace(body.Text) == "" {
				errs[path+".text"] = "text is required"
			} else if msg := validateText(body.Text, maxTextLength); msg != "" {
				errs[path+".text"] = msg
			}
		case BodyItem:
			if !itemIDPattern.MatchString(strings.TrimSpace(body.Item)) {
				errs[path+".item"] = "item id must be a namespaced identifier such as minecraft:diamond"
			}
			if body.Count < 0 || body.Count > 99 {
				errs[path+".count"] = "count must be between 0 and 99"
			}
			if strings.TrimSpace(body.Description) != "" {
				if msg := validateText(body.Description, maxTextLength); msg != "" {
					errs[path+".description"] = msg
				}
			}
			if body.Width > maxItemDimension {
				errs[path+".width"] = "item width must be between 0 and 256"
			}
			if body.Height < 0 || body.Height > maxItemDimension {
				errs[path+".height"] = "item height must be between 0 and 256"
			}
		default:
			errs[path+".kind"] = "unsupported body kind"
		}
	}

	if len(doc.Inputs) > maxInputs {
		errs["inputs"] = fmt.Sprintf("at most %d inputs are allowed", maxInputs)
	}
	seenKeys := map[string]bool{}
	roleCount := map[string]int{}
	for i, input := range doc.Inputs {
		path := fmt.Sprintf("inputs[%d]", i)
		checkID(path, input.ID)
		if !validVisibility(screen, input.When) {
			errs[path+".when"] = "unsupported visibility condition"
		}
		key := strings.TrimSpace(input.Key)
		if !keyPattern.MatchString(key) {
			errs[path+".key"] = "key must match [a-z0-9_] and be at most 32 characters"
		} else if seenKeys[key] {
			errs[path+".key"] = "key must be unique"
		}
		seenKeys[key] = true
		if strings.TrimSpace(input.Label) == "" {
			errs[path+".label"] = "label is required"
		} else if msg := validateText(input.Label, maxTitleLength); msg != "" {
			errs[path+".label"] = msg
		}
		if input.Width < 0 || input.Width > maxElemWidth {
			errs[path+".width"] = "width must be between 0 and 1024"
		}
		switch input.Role {
		case "":
		case RolePassword, RoleConfirm, RoleProfileName:
			roleCount[input.Role]++
			if !roleAllowed(screen, input.Role) {
				errs[path+".role"] = "this role is not available on this screen"
			}
			if input.Kind != InputText {
				errs[path+".role"] = "this role requires a text input"
			}
			if !functionalVisibility(input.When) {
				errs[path+".when"] = "role-bound inputs must stay visible whenever they are required"
			}
		case RoleProfileChoice:
			roleCount[input.Role]++
			if !roleAllowed(screen, input.Role) {
				errs[path+".role"] = "this role is not available on this screen"
			}
			if input.Kind != InputOption {
				errs[path+".role"] = "the profile choice role requires an option input"
			}
			if !functionalVisibility(input.When) {
				errs[path+".when"] = "role-bound inputs must stay visible whenever they are required"
			}
		default:
			errs[path+".role"] = "unsupported input role"
		}
		switch input.Kind {
		case InputText:
			if input.MaxLength < 0 || input.MaxLength > 1024 {
				errs[path+".max_length"] = "max length must be between 0 and 1024"
			}
			effectiveMax := input.MaxLength
			if effectiveMax <= 0 {
				effectiveMax = defaultTextMaxLength
			}
			if input.Initial != "" && len([]rune(input.Initial)) > effectiveMax {
				// Vanilla rejects initial values longer than max_length.
				errs[path+".initial"] = fmt.Sprintf("initial value exceeds the maximum length of %d", effectiveMax)
			}
			if input.MultilineLines < 0 || input.MultilineLines > maxMultiline {
				errs[path+".multiline_lines"] = fmt.Sprintf("multiline lines must be between 0 and %d", maxMultiline)
			}
			if input.Role != "" && input.Multiline {
				errs[path+".multiline"] = "password inputs cannot be multiline"
			}
		case InputBoolean:
			if len(input.OnTrue) > maxTitleLength || len(input.OnFalse) > maxTitleLength {
				errs[path+".on_true"] = "boolean labels are too long"
			}
		case InputOption:
			if len(input.Options) == 0 && input.Role != RoleProfileChoice {
				errs[path+".options"] = "at least one option is required"
			}
			if len(input.Options) > maxOptions {
				errs[path+".options"] = fmt.Sprintf("at most %d options are allowed", maxOptions)
			}
			seenOptions := map[string]bool{}
			initialCount := 0
			for j, option := range input.Options {
				if option.Initial {
					initialCount++
				}
				optPath := fmt.Sprintf("%s.options[%d]", path, j)
				id := strings.TrimSpace(option.ID)
				if id == "" {
					errs[optPath+".id"] = "option id is required"
				} else if seenOptions[id] {
					errs[optPath+".id"] = "option id must be unique"
				}
				seenOptions[id] = true
				if strings.TrimSpace(option.Display) == "" {
					errs[optPath+".display"] = "option display is required"
				} else if msg := validateText(option.Display, maxTitleLength); msg != "" {
					errs[optPath+".display"] = msg
				}
			}
			if initialCount > 1 {
				// Vanilla's single_option codec rejects multiple initial options.
				errs[path+".options"] = "only one option may be initially selected"
			}
		case InputRange:
			if input.End <= input.Start {
				errs[path+".end"] = "end must be greater than start"
			}
			if input.Step != nil && *input.Step <= 0 {
				errs[path+".step"] = "step must be greater than zero"
			}
			if input.InitialNum != nil && (*input.InitialNum < input.Start || *input.InitialNum > input.End) {
				errs[path+".initial_num"] = "initial value must be inside the range"
			}
		default:
			errs[path+".kind"] = "unsupported input kind"
		}
	}

	if len(doc.Buttons) == 0 {
		errs["buttons"] = "at least one button is required"
	}
	if len(doc.Buttons) > maxButtons {
		errs["buttons"] = fmt.Sprintf("at most %d buttons are allowed", maxButtons)
	}
	submitCount := 0
	clientActionCount := 0
	for i, button := range doc.Buttons {
		path := fmt.Sprintf("buttons[%d]", i)
		checkID(path, button.ID)
		if !validVisibility(screen, button.When) {
			errs[path+".when"] = "unsupported visibility condition"
		}
		if strings.TrimSpace(button.Label) == "" {
			errs[path+".label"] = "button label is required"
		} else if msg := validateText(button.Label, maxTitleLength); msg != "" {
			errs[path+".label"] = msg
		}
		if strings.TrimSpace(button.Tooltip) != "" {
			if msg := validateText(button.Tooltip, maxTitleLength); msg != "" {
				errs[path+".tooltip"] = msg
			}
		}
		if button.Width < 0 || button.Width > maxElemWidth {
			errs[path+".width"] = "width must be between 0 and 1024"
		}
		switch button.Action.Kind {
		case ActionSubmit:
			submitCount++
			if !alwaysVisible(button.When) {
				errs[path+".when"] = "the submit button must stay visible in every auth branch"
			}
		case ActionOpenURL:
			clientActionCount++
			parsed, err := url.Parse(strings.TrimSpace(button.Action.URL))
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				errs[path+".action.url"] = "a valid http(s) URL is required"
			}
		case ActionCopyToClipboard:
			clientActionCount++
			if strings.TrimSpace(button.Action.Value) == "" {
				errs[path+".action.value"] = "a clipboard value is required"
			}
		case ActionOpenScreen:
			allowed := false
			for _, target := range openScreenTargets(screen) {
				if button.Action.Screen == target {
					allowed = true
				}
			}
			if !allowed {
				errs[path+".action.screen"] = "this screen switch is not available here"
			}
		default:
			errs[path+".action.kind"] = "unsupported button action"
		}
	}
	if submitCount == 0 && len(doc.Buttons) > 0 {
		errs["buttons"] = "one button must use the submit action so players can continue"
	}
	if clientActionCount > 0 && (doc.AfterAction == "" || doc.AfterAction == AfterWaitForResponse) {
		errs["after_action"] = "buttons with client actions (open URL, copy) require after_action=none, otherwise players get stuck on the waiting screen"
	}

	for _, role := range requiredRoles(screen) {
		if roleCount[role] != 1 {
			errs["inputs.role."+role] = "exactly one input must be bound to the " + role + " role"
		}
	}
	for role, count := range roleCount {
		if count > 1 {
			errs["inputs.role."+role] = "only one input may be bound to the " + role + " role"
		}
	}
	return errs
}

// coerceSlices replaces nil element slices with empty ones so JSON marshals
// always emit arrays — frontend consumers iterate these fields directly.
func coerceSlices(doc DialogDoc) DialogDoc {
	if doc.Body == nil {
		doc.Body = []DialogBody{}
	}
	if doc.Inputs == nil {
		doc.Inputs = []DialogInput{}
	}
	if doc.Buttons == nil {
		doc.Buttons = []DialogButton{}
	}
	return doc
}

// NormalizeDialog upgrades stored documents to the current version. Legacy v1
// documents (fixed password/submit slots) are converted into the free-form
// model so existing overrides keep working.
func NormalizeDialog(screen string, raw json.RawMessage) (DialogDoc, error) {
	var probe struct {
		Version int             `json:"version"`
		Inputs  json.RawMessage `json:"inputs"`
		Submit  json.RawMessage `json:"submit"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return DialogDoc{}, err
	}
	if probe.Version >= DialogDocVersion {
		var doc DialogDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			return DialogDoc{}, err
		}
		return coerceSlices(doc), nil
	}
	// v1 detection: inputs is an object and/or a submit field exists.
	looksV1 := len(probe.Submit) > 0 && string(probe.Submit) != "null"
	if !looksV1 && len(probe.Inputs) > 0 {
		trimmed := strings.TrimSpace(string(probe.Inputs))
		looksV1 = strings.HasPrefix(trimmed, "{")
	}
	if !looksV1 {
		var doc DialogDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			return DialogDoc{}, err
		}
		doc.Version = DialogDocVersion
		return coerceSlices(doc), nil
	}
	var legacy struct {
		Title string `json:"title"`
		Body  []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Text  string `json:"text"`
			Width int    `json:"width"`
			When  string `json:"when"`
		} `json:"body"`
		Inputs struct {
			PasswordLabel string `json:"password_label"`
			ConfirmLabel  string `json:"confirm_label"`
			Width         int    `json:"width"`
		} `json:"inputs"`
		Submit struct {
			Label   string `json:"label"`
			Tooltip string `json:"tooltip"`
			Width   int    `json:"width"`
		} `json:"submit"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return DialogDoc{}, err
	}
	doc := DefaultDialog(screen)
	if strings.TrimSpace(legacy.Title) != "" {
		doc.Title = legacy.Title
	}
	if len(legacy.Body) > 0 {
		doc.Body = nil
		for _, block := range legacy.Body {
			when := block.When
			if block.Type == "error" {
				when = WhenError
			}
			doc.Body = append(doc.Body, DialogBody{ID: block.ID, Kind: BodyText, Text: block.Text, Width: block.Width, When: when})
		}
	}
	for i := range doc.Inputs {
		if doc.Inputs[i].Role == RolePassword && strings.TrimSpace(legacy.Inputs.PasswordLabel) != "" {
			doc.Inputs[i].Label = legacy.Inputs.PasswordLabel
		}
		if doc.Inputs[i].Role == RoleConfirm && strings.TrimSpace(legacy.Inputs.ConfirmLabel) != "" {
			doc.Inputs[i].Label = legacy.Inputs.ConfirmLabel
		}
		if legacy.Inputs.Width > 0 {
			doc.Inputs[i].Width = legacy.Inputs.Width
		}
	}
	if len(doc.Buttons) > 0 {
		if strings.TrimSpace(legacy.Submit.Label) != "" {
			doc.Buttons[0].Label = legacy.Submit.Label
		}
		doc.Buttons[0].Tooltip = legacy.Submit.Tooltip
		doc.Buttons[0].Width = legacy.Submit.Width
	}
	return doc, nil
}
