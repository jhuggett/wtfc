package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jhuggett/wtfc/internal/auto"
	"github.com/jhuggett/wtfc/internal/change"
	"github.com/jhuggett/wtfc/internal/config"
	"github.com/jhuggett/wtfc/internal/hook"
	"github.com/jhuggett/wtfc/internal/release"
)

// flashTTL is how long a toast stays on screen before auto-dismissing.
const flashTTL = 3 * time.Second

// flashTickMsg drives the toast auto-dismiss timer. The model schedules
// one on Init and re-schedules from Update so we can poll the flash's
// age without burdening every flash-setting call site with cmd plumbing.
type flashTickMsg struct{}

// ansiCSI matches ANSI Control Sequence Introducer sequences (e.g. SGR
// color/style codes). Used to strip styling from the timeline backdrop
// before re-rendering it dimmed under a modal.
var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// Run launches the TUI, blocking until the user quits.
func Run() error {
	cfg, err := pickConfig()
	if err != nil {
		return err
	}
	m := newMainModel(cfg)
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// pickConfig finds wtfc/config.toml. First tries walk-up from cwd; if that fails,
// walks down from cwd to discover configs and lets the user pick interactively.
func pickConfig() (*config.Config, error) {
	if cfg, err := config.LoadFromCwd(); err == nil {
		return cfg, nil
	} else if !errors.Is(err, config.ErrNotFound) {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	paths, err := config.FindDown(cwd)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no wtfc/config.toml found at or below %s — run `wtfc init`", cwd)
	}
	if len(paths) == 1 {
		return config.Load(paths[0])
	}
	picker := newPickerModel(paths, cwd)
	res, err := tea.NewProgram(picker).Run()
	if err != nil {
		return nil, err
	}
	picked := res.(pickerModel).picked
	if picked == "" {
		return nil, fmt.Errorf("no config selected")
	}
	return config.Load(picked)
}

// ── palette + styles ─────────────────────────────────────────────────────

var (
	// Gruvbox Dark palette. Warm, earthy, easy on the eyes. Primary is
	// gruvbox yellow (#fabd2f); fg is cream (#ebdbb2); accents in burnt
	// orange and gruvbox green/red for semantic colours.
	cBrand     = lipgloss.Color("214") // gruvbox yellow — titles, brand
	cBrandSoft = lipgloss.Color("223") // cream — chip text on tinted bg
	cBrandDim  = lipgloss.Color("172") // muted gold — borders, accents
	cAccent    = lipgloss.Color("208") // burnt orange — secondary highlights
	cFg        = lipgloss.Color("223") // cream — body text
	cDim       = lipgloss.Color("246") // dim cream
	cMuted     = lipgloss.Color("239") // separators
	cSuccess   = lipgloss.Color("142") // gruvbox green
	cWarning   = lipgloss.Color("208") // burnt orange (yellow is brand)
	cDanger    = lipgloss.Color("167") // gruvbox red
	cPanelBg   = lipgloss.Color("235") // gruvbox bg0 — status bar
	cChipBg    = lipgloss.Color("237") // gruvbox bg1 — active chip bg
	cChipDimBg = lipgloss.Color("236") // between bg0/bg1 — muted chip bg
	cSelBg     = lipgloss.Color("237") // selected row bg (subtle lift)
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	h2Style     = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	bodyStyle   = lipgloss.NewStyle().Foreground(cFg)
	dimStyle    = lipgloss.NewStyle().Foreground(cDim)
	mutedStyle  = lipgloss.NewStyle().Foreground(cMuted)
	cursorStyle = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	okStyle     = lipgloss.NewStyle().Foreground(cSuccess).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(cWarning).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(cDanger).Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBrandDim).
			Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(cFg).
			Background(cPanelBg).
			Padding(0, 1)

	// Inline tag: colored text with no background. Used wherever chips
	// would cluster (field values, breakdown counts) — backgrounds at that
	// density turn the screen into a wall of grey blocks.
	tagStyle    = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	tagDimStyle = lipgloss.NewStyle().Foreground(cDim)

	// brandDimStyle: muted-gold foreground for line work (spine connectors,
	// unfocused card borders). Same family as the brand yellow so the
	// line color reads consistently across the timeline; focused elements
	// jump to the brighter tagStyle.
	brandDimStyle = lipgloss.NewStyle().Foreground(cBrandDim)

	// lineStyle: neutral grey for unfocused chrome (spine + card borders).
	// Going grey instead of muted-gold keeps the timeline structure quiet
	// so focused (brand yellow) elements pop hard. This is the difference
	// between "everything is brand-coloured" and "the cursor is obvious".
	lineStyle = lipgloss.NewStyle().Foreground(cMuted)

	// softTagStyle: muted-gold for inline tag values inside cards (feat,
	// public, internal, etc). Uses cBrandDim, no bold. Reserves the
	// brighter tagStyle for the diamond ◆ and focused borders so the
	// diamond reads as each card's visual anchor.
	softTagStyle = lipgloss.NewStyle().Foreground(cBrandDim)

	// Selected-row tint: subtle dark lift with gold fg so the cursor
	// reads at a glance without screaming.
	selRowStyle = lipgloss.NewStyle().
			Foreground(cBrand).
			Background(cSelBg).
			Bold(true)
)

// ── picker (multi-config disambiguation) ──────────────────────────────────

type pickerModel struct {
	paths  []string
	root   string
	cursor int
	picked string
}

func newPickerModel(paths []string, root string) pickerModel {
	return pickerModel{paths: paths, root: root}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.paths)-1 {
				m.cursor++
			}
		case "enter":
			m.picked = m.paths[m.cursor]
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Multiple wtfc/config.toml found — pick one:") + "\n\n")
	for i, p := range m.paths {
		rel, err := filepath.Rel(m.root, p)
		if err != nil {
			rel = p
		}
		cursor := "  "
		line := rel
		if i == m.cursor {
			cursor = cursorStyle.Render("➤ ")
			line = cursorStyle.Render(line)
		}
		b.WriteString(cursor + line + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ select · enter pick · esc cancel"))
	return b.String()
}

// ── main model ────────────────────────────────────────────────────────────

type screen int

const (
	screenTimeline screen = iota
	screenForm
	screenHooks
	screenHookOutput
	screenConfirm
)

// formKind selects which submit action runs when the user hits enter on the
// trailing "submit" slot of a form.
type formKind int

const (
	formChange formKind = iota
	formRelease
)

type mainModel struct {
	cfg           *config.Config
	screen        screen
	flash         string
	flashOK       bool
	flashSetAt    time.Time
	width, height int

	// timeline screen — the home view. pending is newest-pending-first;
	// releases is newest-first (via cl.History()). lastForkCursor
	// remembers which fork node (0 = add-change, 1 = next-release) the
	// cursor last visited so ↑ from first pending returns to the same
	// side of the fork.
	pending         []*change.Change
	releases        []*release.Release
	timelineCursor  int
	timelineScroll  int
	lastForkCursor  int
	expandedRelease int // index of expanded release, or -1 for none

	// form state (used for both change and release flows). When
	// formChangeID is set during a formChange flow, we're editing that
	// existing change instead of creating a new one.
	formKind     formKind
	formTitle    string
	formSchema   []config.Field
	formFields   []formField
	formCursor   int
	formChangeID string

	// hooks screen
	hooks        []hook.Entry
	hookCursor   int
	hookOutput   string
	hookOutTitle string

	// confirmation modal — shown over the timeline before destructive
	// actions. confirmKind identifies which action to take on confirm
	// ("unrelease" today; more later); confirmPrompt is the question
	// shown to the user.
	confirmKind   string
	confirmPrompt string
}

// formField holds the in-progress value for one schema field. Only the
// subset relevant to the field's type is used.
type formField struct {
	text    string // string/int/bool (and any unknown type)
	picks   []bool // enum/list: which values are selected
	pillCur int    // enum/list: cursor among values
}

func newMainModel(cfg *config.Config) mainModel {
	m := mainModel{
		cfg:             cfg,
		screen:          screenTimeline,
		expandedRelease: -1,
	}
	m.refreshTimeline()
	return m
}

func (m mainModel) Init() tea.Cmd { return flashTick() }

// flashTick schedules the next poll to check whether the current flash
// has aged out. Polling at 500ms gives sub-second toast disappearance
// without spinning the runtime.
func flashTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return flashTickMsg{}
	})
}

// setFlash replaces the current toast with msg. ok=true styles success,
// ok=false styles error. Always updates the timestamp so the toast
// resets its 3s lifetime.
func (m *mainModel) setFlash(msg string, ok bool) {
	m.flash = msg
	m.flashOK = ok
	m.flashSetAt = time.Now()
}

// appendFlash adds extra text to the current flash, preserving the
// original ok-state. Used by hook runners that want to layer "but X
// also failed" onto the just-set message.
func (m *mainModel) appendFlash(extra string, ok bool) {
	if m.flash == "" {
		m.flash = extra
	} else {
		m.flash += extra
	}
	m.flashOK = ok
	m.flashSetAt = time.Now()
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case flashTickMsg:
		// Auto-dismiss the toast once it's older than flashTTL.
		if !m.flashSetAt.IsZero() && time.Since(m.flashSetAt) > flashTTL {
			m.flash = ""
			m.flashSetAt = time.Time{}
		}
		return m, flashTick()
	case tea.KeyMsg:
		// Capture the pre-dispatch flash so we can timestamp any change
		// without threading setFlash through every leaf handler.
		oldFlash := m.flash
		var (
			newM tea.Model
			cmd  tea.Cmd
		)
		switch m.screen {
		case screenTimeline:
			newM, cmd = m.updateTimeline(msg)
		case screenForm:
			newM, cmd = m.updateForm(msg)
		case screenHooks:
			newM, cmd = m.updateHooks(msg)
		case screenHookOutput:
			newM, cmd = m.updateHookOutput(msg)
		case screenConfirm:
			newM, cmd = m.updateConfirm(msg)
		default:
			return m, nil
		}
		if mm, ok := newM.(mainModel); ok && mm.flash != oldFlash {
			mm.flashSetAt = time.Now()
			newM = mm
		}
		return newM, cmd
	}
	return m, nil
}


// openForm prepares form state for either the change or release flow and
// switches the screen. Always resets edit context — call openEditForm
// after this if you want to seed it for an existing change.
func (m *mainModel) openForm(kind formKind, title string, schema []config.Field) {
	m.formKind = kind
	m.formTitle = title
	m.formSchema = schema
	m.formFields = make([]formField, len(schema))
	for i, f := range schema {
		if f.Type == "enum" || f.Type == "list" {
			m.formFields[i].picks = make([]bool, len(f.Values))
		}
	}
	m.formCursor = 0
	m.formChangeID = ""
	m.screen = screenForm

	// Pre-fill from declared auto-sources so the user sees the value
	// (git.user, git.sha, etc.) before they submit and can edit it if
	// they want. Same resolver runs again at the write boundary as a
	// backstop, so this is purely a UX courtesy.
	m.applyAutoSources()
}

// applyAutoSources runs the auto resolver against the current form
// schema and seeds matching string-typed formFields with the resolved
// values. Enum/list fields are skipped — auto sources today only
// produce single string values.
func (m *mainModel) applyAutoSources() {
	resolved := map[string]any{}
	auto.Resolve(m.cfg.ProjectRoot(), m.formSchema, resolved)
	for i, f := range m.formSchema {
		v, ok := resolved[f.Name]
		if !ok {
			continue
		}
		if f.Type == "enum" || f.Type == "list" {
			continue
		}
		if s, ok := v.(string); ok && m.formFields[i].text == "" {
			m.formFields[i].text = s
		}
	}
}

// openEditForm opens the change form pre-populated with the existing
// values from c, and remembers c.ID so submit applies the edit instead
// of creating a new change.
func (m *mainModel) openEditForm(c *change.Change) {
	m.openForm(formChange, "Edit change", m.cfg.Changeset.Fields)
	m.formChangeID = c.ID
	for i, f := range m.formSchema {
		v := c.Fields[f.Name]
		if v == nil {
			continue
		}
		switch f.Type {
		case "enum":
			if s, ok := v.(string); ok {
				for j, val := range f.Values {
					if val == s {
						m.formFields[i].picks[j] = true
						break
					}
				}
			}
		case "list":
			for _, item := range extractStrings(v) {
				for j, val := range f.Values {
					if val == item {
						m.formFields[i].picks[j] = true
					}
				}
			}
		default:
			if s, ok := v.(string); ok {
				m.formFields[i].text = s
			} else {
				m.formFields[i].text = fmt.Sprintf("%v", v)
			}
		}
	}
}

// ── views ────────────────────────────────────────────────────────────────

func (m mainModel) View() string {
	switch m.screen {
	case screenTimeline:
		return m.viewTimeline()
	case screenForm:
		return m.viewForm()
	case screenHooks:
		return m.viewHooks()
	case screenHookOutput:
		return m.viewHookOutput()
	case screenConfirm:
		return m.viewConfirm()
	}
	return ""
}

// header renders the persistent top band. Brand on the left, project
// name (basename of the dir containing wtfc/) on the right. No tagline.
// Drops the project name only if the terminal is too narrow to fit
// both. Always one line tall.
func (m mainModel) header() string {
	w := m.termWidth()
	const hPad = 4

	brand := titleStyle.Render("wtfc")
	project := bodyStyle.Render(filepath.Base(m.cfg.ProjectRoot()))
	rule := mutedStyle.Render(strings.Repeat("─", w))

	left := strings.Repeat(" ", hPad) + brand
	row := left
	if lipgloss.Width(left)+lipgloss.Width(project)+hPad+2 <= w {
		gap := w - lipgloss.Width(left) - lipgloss.Width(project) - hPad
		row = left + strings.Repeat(" ", gap) + project
	}
	if pad := w - lipgloss.Width(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	return "\n" + row + "\n" + rule
}

// footer renders the persistent bottom band: a hairline rule above
// hint text right-aligned with the same 4-col side margins as the
// header. No background fill — matches the header's visual treatment
// so top and bottom feel like a pair.
func (m mainModel) footer(hints string) string {
	w := m.termWidth()
	const hPad = 4
	rule := mutedStyle.Render(strings.Repeat("─", w))

	avail := w - hPad*2
	if avail < 0 {
		avail = 0
	}
	right := dimStyle.Render(hints)
	if lipgloss.Width(right) > avail {
		right = dimStyle.Render(compactHints(hints))
	}
	if lipgloss.Width(right) > avail {
		right = ""
	}
	pad := w - lipgloss.Width(right) - hPad
	if pad < 0 {
		pad = 0
	}
	row := strings.Repeat(" ", pad) + right
	return rule + "\n" + row
}

// compactHints strips descriptions from a `key verb · key verb · …` hint
// string, leaving just the keys joined by " · ". Falls back to the input
// unchanged if it has no recognizable structure.
func compactHints(full string) string {
	if full == "" {
		return ""
	}
	parts := strings.Split(full, " · ")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		// Take the first whitespace-separated token as the "key".
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		keys = append(keys, fields[0])
	}
	return strings.Join(keys, " · ")
}

// flashLine renders the success/error message that follows mutating actions.
func (m mainModel) flashLine() string {
	if m.flash == "" {
		return ""
	}
	style := errStyle
	if m.flashOK {
		style = okStyle
	}
	return "\n" + style.Render(m.flash) + "\n"
}

func (m mainModel) termWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m mainModel) termHeight() int {
	if m.height > 0 {
		return m.height
	}
	return 24
}

// formatFieldValue renders any value type appropriately. Enum/list values
// become chips; nil renders as a muted placeholder. Fields whose source
// is "git.sha" display only the first 7 chars (the on-disk record keeps
// the full SHA).
func formatFieldValue(f config.Field, v any) string {
	if v == nil {
		return mutedStyle.Render("—")
	}
	switch f.Type {
	case "enum":
		if s, ok := v.(string); ok && s != "" {
			return softTagStyle.Render(s)
		}
	case "list":
		vals := extractStrings(v)
		if len(vals) == 0 {
			return mutedStyle.Render("—")
		}
		var parts []string
		for _, s := range vals {
			parts = append(parts, softTagStyle.Render(s))
		}
		return strings.Join(parts, dimStyle.Render(", "))
	case "bool":
		if b, ok := v.(bool); ok {
			if b {
				return okStyle.Render("yes")
			}
			return dimStyle.Render("no")
		}
	}
	// Fallback for string / int / unknown: pretty-print whatever we got.
	switch x := v.(type) {
	case string:
		if x == "" {
			return mutedStyle.Render("—")
		}
		if f.Source == "git.sha" && len(x) > 7 {
			x = x[:7]
		}
		return bodyStyle.Render(x)
	case []any:
		var parts []string
		for _, it := range x {
			parts = append(parts, fmt.Sprintf("%v", it))
		}
		return bodyStyle.Render(strings.Join(parts, ", "))
	default:
		return bodyStyle.Render(fmt.Sprintf("%v", v))
	}
}

// extractStrings normalises an arbitrary JSON-decoded value into a slice of
// string items. Single strings become one-element slices; arrays of strings
// pass through; everything else returns empty.
func extractStrings(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if s, ok := it.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// ── form (shared by change + release flows) ──────────────────────────────

var (
	// "selected" = the value will be saved. Bright green so it stands out
	// regardless of focus.
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	// "unselected" = present but not picked. Dim grey.
	unselectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

func (m mainModel) updateForm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := m.formSchema
	submitIdx := len(fields)
	cur := m.formCursor

	// the focused field, if any (nil when cursor is on submit)
	var ff *formField
	var fdef *config.Field
	if cur < submitIdx {
		ff = &m.formFields[cur]
		fdef = &fields[cur]
	}

	switch k.String() {
	case "ctrl+c", "esc":
		m.formChangeID = ""
		m.screen = screenTimeline
		return m, nil

	case "up", "shift+tab":
		if cur > 0 {
			m.formCursor--
		}
		return m, nil

	case "down", "tab":
		if cur < submitIdx {
			m.formCursor++
		}
		return m, nil

	case "left":
		if ff != nil && (fdef.Type == "enum" || fdef.Type == "list") && len(fdef.Values) > 0 {
			if ff.pillCur > 0 {
				ff.pillCur--
			}
		}
		return m, nil

	case "right":
		if ff != nil && (fdef.Type == "enum" || fdef.Type == "list") && len(fdef.Values) > 0 {
			if ff.pillCur < len(fdef.Values)-1 {
				ff.pillCur++
			}
		}
		return m, nil

	case "enter":
		if cur < submitIdx {
			m.formCursor++
			return m, nil
		}
		return m.submitForm()
	}

	// from here on: type-specific handling
	if ff == nil {
		return m, nil
	}

	switch fdef.Type {
	case "enum":
		// space selects the focused value (replaces any previous selection)
		if k.String() == " " || k.String() == "space" {
			for i := range ff.picks {
				ff.picks[i] = false
			}
			if ff.pillCur < len(ff.picks) {
				ff.picks[ff.pillCur] = true
			}
		}
	case "list":
		// space toggles the focused value
		if k.String() == " " || k.String() == "space" {
			if ff.pillCur < len(ff.picks) {
				ff.picks[ff.pillCur] = !ff.picks[ff.pillCur]
			}
		}
	default:
		// freeform text input (string, int, bool, unknown)
		switch k.String() {
		case "backspace":
			if len(ff.text) > 0 {
				ff.text = ff.text[:len(ff.text)-1]
			}
		case "space":
			ff.text += " "
		default:
			s := k.String()
			if len(s) == 1 {
				ff.text += s
			}
		}
	}
	return m, nil
}

// submitForm gathers form values, dispatches by formKind, and returns to menu.
func (m mainModel) submitForm() (tea.Model, tea.Cmd) {
	values := map[string]string{}
	for i, f := range m.formSchema {
		s := m.formFields[i]
		switch f.Type {
		case "enum":
			for j, v := range f.Values {
				if j < len(s.picks) && s.picks[j] {
					values[f.Name] = v
					break
				}
			}
		case "list":
			var picked []string
			for j, v := range f.Values {
				if j < len(s.picks) && s.picks[j] {
					picked = append(picked, v)
				}
			}
			if len(picked) > 0 {
				values[f.Name] = strings.Join(picked, ",")
			}
		default:
			v := strings.TrimSpace(s.text)
			if v != "" {
				values[f.Name] = v
			}
		}
	}

	switch m.formKind {
	case formChange:
		// Edit path: we have an existing change to update in place.
		if m.formChangeID != "" {
			existing, _, err := change.FindByID(m.cfg, m.formChangeID)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			typed, err := change.CoerceFields(m.cfg.Changeset.Fields, values)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			existing.Apply(typed)
			if err := existing.Write(m.cfg); err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			short := existing.ID
			if len(short) > 8 {
				short = short[:8]
			}
			m.flash = fmt.Sprintf("✎ edited change %s", short)
			m.flashOK = true
			m.formChangeID = ""
			m.screen = screenTimeline
			m.refreshTimeline()
			return m, nil
		}

		// Create path.
		c, err := change.New(m.cfg, values)
		if err != nil {
			m.flash = err.Error()
			m.flashOK = false
			return m, nil
		}
		if err := c.Write(m.cfg); err != nil {
			m.flash = err.Error()
			m.flashOK = false
			return m, nil
		}
		short := c.ID
		if len(short) > 8 {
			short = short[:8]
		}
		m.flash = fmt.Sprintf("✎ created change %s", short)
		m.flashOK = true
		m.screen = screenTimeline
		m.refreshTimeline()
		return m, nil

	case formRelease:
		name := values["name"]
		if name == "" {
			m.flash = "name is required"
			m.flashOK = false
			m.formCursor = 0 // jump back to the name field
			return m, nil
		}
		// Strip "name" from values; everything else is release metadata.
		delete(values, "name")
		typed, err := release.CoerceFields(m.cfg.Release.Fields, values)
		if err != nil {
			m.flash = err.Error()
			m.flashOK = false
			return m, nil
		}
		rel, err := release.Cut(m.cfg, name, typed)
		if err != nil {
			m.flash = err.Error()
			m.flashOK = false
			return m, nil
		}
		m.flash = fmt.Sprintf("◆ released %s with %d change(s)", rel.Name, len(rel.Changes))
		m.flashOK = true
		m.screen = screenTimeline
		m.runHook(hook.OpRelease, rel.Name)
		m.refreshTimeline()
		return m, nil
	}
	return m, nil
}

// runHook fires on-release-changed with the full Changelog as payload, after
// any release mutation. WTFC_OP distinguishes release from unrelease so a
// single hook script can branch if needed. Failures fold into the flash
// line; the prior mutation stays committed (changelog is authoritative).
func (m *mainModel) runHook(op, releaseName string) {
	cl, err := release.Load(m.cfg)
	if err != nil {
		m.flash += fmt.Sprintf(" · could not load changelog for %s hook: %v", hook.EventOnReleaseChanged, err)
		m.flashOK = false
		return
	}
	res, err := hook.Run(m.cfg, hook.EventOnReleaseChanged, cl, map[string]string{
		"WTFC_OP":           op,
		"WTFC_RELEASE_NAME": releaseName,
	})
	if err != nil {
		m.flash = fmt.Sprintf("%s — but %s hook failed: %v", m.flash, hook.EventOnReleaseChanged, err)
		m.flashOK = false
		// Append last bit of stderr so the user has a clue what broke.
		if res != nil && len(res.Stderr) > 0 {
			tail := strings.TrimRight(string(res.Stderr), "\n")
			if i := strings.LastIndexByte(tail, '\n'); i >= 0 {
				tail = tail[i+1:]
			}
			m.flash += "\n  " + tail
		}
		return
	}
	if res != nil && res.Ran {
		m.flash += fmt.Sprintf(" · %s hook ran", hook.EventOnReleaseChanged)
	}
}

// viewForm renders the create-change / cut-release form as a modal that
// floats over the timeline. The timeline stays visible above and below
// the modal so the user retains context while filling it in. Footer
// hints switch to form-specific keys; pressing esc closes the modal.
func (m mainModel) viewForm() string {
	w := m.termWidth()
	h := m.termHeight()
	header := m.header()
	footer := m.footer(m.formHints())
	bodyH := h - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 4 {
		bodyH = 4
	}

	// Background: the timeline body, scrolled to current position.
	bgRaw, _ := m.renderTimelineBody()
	bgLines := strings.Split(bgRaw, "\n")
	scroll := m.timelineScroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > len(bgLines) {
		scroll = len(bgLines)
	}
	end := scroll + bodyH
	if end > len(bgLines) {
		end = len(bgLines)
	}
	visible := bgLines[scroll:end]
	for len(visible) < bodyH {
		visible = append(visible, "")
	}

	// Grey out the backdrop so the modal pops. Strip the timeline's
	// ANSI styling and re-render each line in a uniform muted grey —
	// no individual element competes with the modal for attention.
	for i, line := range visible {
		plain := ansiCSI.ReplaceAllString(line, "")
		if strings.TrimSpace(plain) != "" {
			visible[i] = mutedStyle.Render(plain)
		}
	}

	// The modal box itself.
	modal := m.renderFormModal()

	// Overlay the modal centered over the body. Modal rows replace the
	// background rows they occupy; rows above/below show the timeline.
	body := overlayLines(visible, modal, w)

	bodyArea := lipgloss.NewStyle().Width(w).Height(bodyH).Render(strings.Join(body, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, header, bodyArea, footer)
}

// renderFormModal builds the modal box: title, optional context (recent
// releases for the release flow), schema fields, submit button, flash
// line. Returns a bordered, padded multi-line string.
func (m mainModel) renderFormModal() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(m.formTitle) + "\n\n")

	if m.formKind == formRelease && len(m.releases) > 0 {
		b.WriteString(dimStyle.Render("Previous releases") + "\n")
		for i, r := range m.releases {
			if i >= 3 {
				break
			}
			b.WriteString("  " + mutedStyle.Render("◷ ") +
				dimStyle.Render(r.ReleasedAt.Local().Format("2006-01-02")) + "  " +
				bodyStyle.Render(r.Name) + "\n")
		}
		b.WriteString("\n")
	}

	if len(m.formSchema) == 0 {
		b.WriteString(dimStyle.Render("(no schema fields defined)") + "\n")
	}
	for i, f := range m.formSchema {
		focused := i == m.formCursor
		bullet := "  "
		label := bodyStyle.Render(f.Name)
		if focused {
			bullet = cursorStyle.Render("▶ ")
			label = cursorStyle.Render(f.Name)
		}
		// Required fields get a dim "*" suffix so users see upfront
		// which slots they must fill in. Same rule enforced on submit
		// via config.Validate.
		req := ""
		if f.Required {
			req = errStyle.Render("*")
		}
		b.WriteString(bullet + label + req + dimStyle.Render("  "+typeHint(f)) + "\n")
		b.WriteString("      " + m.renderFieldValue(i, f, focused) + "\n")
	}
	submitFocused := m.formCursor == len(m.formSchema)
	submitLabel := dimStyle.Render("[ submit ]")
	if submitFocused {
		submitLabel = okStyle.Render("[ ◆ submit ]")
	}
	b.WriteString("\n  " + submitLabel + "\n")
	b.WriteString(m.flashLine())

	// Size the modal generously — roughly two-thirds of the terminal,
	// clamped between 50 and 80 cols of inner content. Padding(2,4)
	// gives the form room to breathe inside the box.
	modalW := m.termWidth() * 2 / 3
	if modalW < 50 {
		modalW = 50
	}
	if modalW > 80 {
		modalW = 80
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBrand).
		Padding(2, 4).
		Width(modalW)
	return box.Render(b.String())
}

// overlayLines centers a multi-line modal over a slice of background
// lines and returns the composited slice. Modal rows replace the
// corresponding background rows entirely; left padding centers the
// modal horizontally inside `width`. Used by viewForm so the form
// floats over the timeline backdrop without obscuring everything.
func overlayLines(bg []string, modal string, width int) []string {
	mLines := strings.Split(modal, "\n")
	mH := len(mLines)
	mW := 0
	for _, l := range mLines {
		if w := lipgloss.Width(l); w > mW {
			mW = w
		}
	}
	startRow := (len(bg) - mH) / 2
	if startRow < 0 {
		startRow = 0
	}
	leftPad := (width - mW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	pad := strings.Repeat(" ", leftPad)
	out := make([]string, len(bg))
	copy(out, bg)
	for i, ml := range mLines {
		row := startRow + i
		if row < 0 || row >= len(out) {
			continue
		}
		out[row] = pad + ml
	}
	return out
}

func typeHint(f config.Field) string {
	if f.Type == "" {
		return "string"
	}
	return f.Type
}

func (m mainModel) renderFieldValue(idx int, f config.Field, focused bool) string {
	s := m.formFields[idx]
	switch f.Type {
	case "enum", "list":
		if len(f.Values) == 0 {
			return dimStyle.Render("(no values defined)")
		}
		var parts []string
		for j, v := range f.Values {
			selected := j < len(s.picks) && s.picks[j]
			pillFocused := focused && j == s.pillCur

			// Selection symbol: () for enum (radio), [] for list (check).
			var marker string
			if f.Type == "list" {
				if selected {
					marker = "[x]"
				} else {
					marker = "[ ]"
				}
			} else {
				if selected {
					marker = "(•)"
				} else {
					marker = "( )"
				}
			}

			// Compose styles (not rendered strings) so ANSI codes don't nest.
			style := unselectedStyle
			if selected {
				style = selectedStyle
			}
			if pillFocused {
				style = style.Underline(true)
			}
			pill := style.Render(marker + " " + v)
			if pillFocused {
				pill = cursorStyle.Render("➤") + pill
			} else {
				pill = " " + pill
			}
			parts = append(parts, pill)
		}
		return strings.Join(parts, "  ")
	default:
		val := s.text
		if focused {
			return cursorStyle.Render(val + "▌")
		}
		if val == "" {
			return dimStyle.Render("∅")
		}
		return val
	}
}

func (m mainModel) formHints() string {
	cur := m.formCursor
	if cur >= len(m.formSchema) {
		return "↑/↓ field · enter submit · esc cancel"
	}
	switch m.formSchema[cur].Type {
	case "enum":
		return "↑/↓ field · ←/→ option · space select · enter next · esc cancel"
	case "list":
		return "↑/↓ field · ←/→ option · space toggle · enter next · esc cancel"
	default:
		return "↑/↓ field · type to edit · enter next · esc cancel"
	}
}

// ── hooks screen ──────────────────────────────────────────────────────────

func (m *mainModel) loadHooks() error {
	hooks, err := hook.List(m.cfg)
	if err != nil {
		return err
	}
	m.hooks = hooks
	if m.hookCursor >= len(hooks) {
		m.hookCursor = 0
	}
	return nil
}

func (m mainModel) updateHooks(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q", "esc":
		m.screen = screenTimeline
		return m, nil
	case "up", "k":
		if m.hookCursor > 0 {
			m.hookCursor--
		}
	case "down", "j":
		if m.hookCursor < len(m.hooks)-1 {
			m.hookCursor++
		}
	case "r":
		if err := (&m).loadHooks(); err != nil {
			m.flash = err.Error()
			m.flashOK = false
		}
	case "e":
		if len(m.hooks) == 0 {
			return m, nil
		}
		entry := m.hooks[m.hookCursor]
		if !entry.CanEnable() {
			m.flash = fmt.Sprintf("nothing to enable for %s (status: %s)", entry.Name, entry.Status)
			m.flashOK = false
			return m, nil
		}
		newPath, err := hook.Enable(entry)
		if err != nil {
			m.flash = err.Error()
			m.flashOK = false
			return m, nil
		}
		m.flash = "enabled " + filepath.Base(newPath)
		m.flashOK = true
		_ = (&m).loadHooks()
	case "t":
		if len(m.hooks) == 0 {
			return m, nil
		}
		entry := m.hooks[m.hookCursor]
		if entry.Status != hook.StatusEnabled {
			m.flash = "hook is not enabled — press 'e' first"
			m.flashOK = false
			return m, nil
		}
		return m.runHookTest(entry)
	}
	return m, nil
}

// runHookTest executes the selected hook against the latest release in the
// changelog, capturing stdout/stderr for display.
func (m mainModel) runHookTest(entry hook.Entry) (tea.Model, tea.Cmd) {
	cl, err := release.Load(m.cfg)
	if err != nil {
		m.flash = err.Error()
		m.flashOK = false
		return m, nil
	}
	if len(cl.Releases) == 0 {
		m.flash = "no releases yet — cut one before testing a hook"
		m.flashOK = false
		return m, nil
	}
	last := cl.Releases[len(cl.Releases)-1]
	// Test fire mirrors a real on-release-changed: full Changelog as payload,
	// WTFC_OP=release (the most common "test against latest" scenario).
	res, runErr := hook.Run(m.cfg, entry.Event, cl, map[string]string{
		"WTFC_OP":           hook.OpRelease,
		"WTFC_RELEASE_NAME": last.Name,
	})
	var b strings.Builder
	if res != nil && res.Ran {
		if len(res.Stdout) > 0 {
			b.WriteString("─── stdout ───\n")
			b.WriteString(string(res.Stdout))
			if !strings.HasSuffix(string(res.Stdout), "\n") {
				b.WriteString("\n")
			}
		}
		if len(res.Stderr) > 0 {
			b.WriteString("─── stderr ───\n")
			b.WriteString(string(res.Stderr))
			if !strings.HasSuffix(string(res.Stderr), "\n") {
				b.WriteString("\n")
			}
		}
	}
	if runErr != nil {
		b.WriteString("─── error ───\n")
		b.WriteString(runErr.Error() + "\n")
	} else {
		b.WriteString("\n" + okStyle.Render("hook exited 0"))
	}
	m.hookOutTitle = fmt.Sprintf("Test: %s (against release %s)", entry.Name, last.Name)
	m.hookOutput = b.String()
	m.screen = screenHookOutput
	return m, nil
}

func (m mainModel) updateHookOutput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q", "esc":
		_ = (&m).loadHooks()
		m.screen = screenHooks
	}
	return m, nil
}

// viewHooks shares the timeline's centered-screen chrome: header,
// centered title + path, focusable hook list (status glyph, name, dim
// description), and the same footer hint style.
func (m mainModel) viewHooks() string {
	var b strings.Builder

	title := titleStyle.Render("hooks")
	b.WriteString(m.centerPad(lipgloss.Width(title)) + title + "\n")
	pathLabel := mutedStyle.Render(hook.HooksDir(m.cfg))
	b.WriteString(m.centerPad(lipgloss.Width(pathLabel)) + pathLabel + "\n\n")

	if len(m.hooks) == 0 {
		empty := dimStyle.Render("no files in hooks/ — drop an executable named for an event to wire one up")
		b.WriteString(m.centerPad(lipgloss.Width(empty)) + empty + "\n")
	}

	// Each hook entry: line 1 = focus marker + status glyph + name;
	// line 2 = dim description, indented to align under the name.
	for i, h := range m.hooks {
		focused := i == m.hookCursor
		var line1 strings.Builder
		if focused {
			line1.WriteString(tagStyle.Bold(true).Render("▸ "))
		} else {
			line1.WriteString("  ")
		}
		line1.WriteString(hookStatusGlyph(h.Status) + "  ")
		if focused {
			line1.WriteString(tagStyle.Bold(true).Render(h.Name))
		} else {
			line1.WriteString(bodyStyle.Render(h.Name))
		}
		l1 := line1.String()
		l2 := "    " + mutedStyle.Render(hookStatusDescription(h))

		// Center the wider of the two lines on the spine; share leftPad
		// so the rows align.
		blockW := lipgloss.Width(l1)
		if w := lipgloss.Width(l2); w > blockW {
			blockW = w
		}
		left := m.spineCol() - blockW/2
		if left < 0 {
			left = 0
		}
		pad := strings.Repeat(" ", left)
		b.WriteString(pad + l1 + "\n")
		b.WriteString(pad + l2 + "\n\n")
	}

	if len(hook.KnownEvents) > 0 {
		events := dimStyle.Render("known events: ") + softTagStyle.Render(strings.Join(hook.KnownEvents, " · "))
		b.WriteString("\n" + m.centerPad(lipgloss.Width(events)) + events)
	}

	return m.renderScreenCentered(b.String(),
		"↑/↓ select · e enable · t test · r refresh · esc back")
}

// viewConfirm renders a small confirmation dialog floating over a
// dimmed timeline backdrop. Used for destructive actions (unrelease
// today) so the user has to consciously commit instead of fat-fingering
// a single keystroke.
func (m mainModel) viewConfirm() string {
	w := m.termWidth()
	h := m.termHeight()
	header := m.header()
	footer := m.footer("y/enter confirm · n/esc cancel")
	bodyH := h - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 4 {
		bodyH = 4
	}

	// Background: dimmed timeline.
	bgRaw, _ := m.renderTimelineBody()
	bgLines := strings.Split(bgRaw, "\n")
	scroll := m.timelineScroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > len(bgLines) {
		scroll = len(bgLines)
	}
	end := scroll + bodyH
	if end > len(bgLines) {
		end = len(bgLines)
	}
	visible := bgLines[scroll:end]
	for len(visible) < bodyH {
		visible = append(visible, "")
	}
	for i, line := range visible {
		plain := ansiCSI.ReplaceAllString(line, "")
		if strings.TrimSpace(plain) != "" {
			visible[i] = mutedStyle.Render(plain)
		}
	}

	// Modal: small bordered box, danger-toned (destructive action).
	title := errStyle.Render("Are you sure?")
	prompt := bodyStyle.Render(m.confirmPrompt)
	hint := dimStyle.Render("y/enter — confirm    n/esc — cancel")
	content := title + "\n\n" + prompt + "\n\n" + hint

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cDanger).
		Padding(1, 3)
	body := overlayLines(visible, box.Render(content), w)

	bodyArea := lipgloss.NewStyle().Width(w).Height(bodyH).Render(strings.Join(body, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, header, bodyArea, footer)
}

// updateConfirm handles y/n decisions for the confirmation modal. The
// confirmKind tag determines what action runs on confirm.
func (m mainModel) updateConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y", "enter", " ":
		switch m.confirmKind {
		case "unrelease":
			rel, err := release.Unrelease(m.cfg)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				m.confirmKind = ""
				m.confirmPrompt = ""
				m.screen = screenTimeline
				return m, nil
			}
			m.flash = fmt.Sprintf("◆ unreleased %s — %d change(s) restored", rel.Name, len(rel.Changes))
			m.flashOK = true
			m.runHook(hook.OpUnrelease, rel.Name)
			m.refreshTimeline()
		}
		m.confirmKind = ""
		m.confirmPrompt = ""
		m.screen = screenTimeline
		return m, nil
	case "n", "N", "ctrl+c", "esc":
		m.confirmKind = ""
		m.confirmPrompt = ""
		m.screen = screenTimeline
		return m, nil
	}
	return m, nil
}

// viewHookOutput renders the captured stdout/stderr from a hook test
// inside the centered screen chrome, with the title centered above the
// output block.
func (m mainModel) viewHookOutput() string {
	var b strings.Builder
	title := titleStyle.Render(m.hookOutTitle)
	b.WriteString(m.centerPad(lipgloss.Width(title)) + title + "\n\n")

	body := m.hookOutput
	if body == "" {
		body = dimStyle.Render("(no output)")
	}
	b.WriteString(body)

	return m.renderScreenCentered(b.String(), "esc back")
}

func hookStatusGlyph(s hook.Status) string {
	switch s {
	case hook.StatusEnabled:
		return okStyle.Render("✓")
	case hook.StatusNotExecutable:
		return errStyle.Render("⚠")
	case hook.StatusSample:
		return dimStyle.Render("·")
	case hook.StatusUnknown:
		return errStyle.Render("?")
	}
	return " "
}

func hookStatusDescription(e hook.Entry) string {
	switch e.Status {
	case hook.StatusEnabled:
		return "enabled — fires on " + e.Event
	case hook.StatusNotExecutable:
		return "matches " + e.Event + " event but not executable — press 'e' to fix"
	case hook.StatusSample:
		return "sample — press 'e' to enable as " + e.Event
	case hook.StatusUnknown:
		return "filename doesn't match a known event — won't fire"
	}
	return ""
}
