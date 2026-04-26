package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jhuggett/wtfc/internal/change"
	"github.com/jhuggett/wtfc/internal/config"
	"github.com/jhuggett/wtfc/internal/hook"
	"github.com/jhuggett/wtfc/internal/release"
)

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
	cBrand   = lipgloss.Color("213") // magenta — primary brand
	cAccent  = lipgloss.Color("75")  // cyan — secondary highlights
	cFg      = lipgloss.Color("252") // body text
	cDim     = lipgloss.Color("244") // muted body
	cMuted   = lipgloss.Color("239") // borders, separators
	cSuccess = lipgloss.Color("78")
	cWarning = lipgloss.Color("220")
	cDanger  = lipgloss.Color("203")
	cPanel   = lipgloss.Color("236") // status bar background
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	h2Style     = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	bodyStyle   = lipgloss.NewStyle().Foreground(cFg)
	dimStyle    = lipgloss.NewStyle().Foreground(cDim)
	mutedStyle  = lipgloss.NewStyle().Foreground(cMuted)
	cursorStyle = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	okStyle     = lipgloss.NewStyle().Foreground(cSuccess).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(cWarning).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(cDanger).Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cMuted).
			Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(cFg).
			Background(cPanel).
			Padding(0, 1)

	chipStyle = lipgloss.NewStyle().
			Foreground(cBrand).
			Background(cPanel).
			Padding(0, 1).
			Bold(true)

	menuSelStyle = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
)

const tagline = "changelogs, but make 'em fun"

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

// ── main menu ─────────────────────────────────────────────────────────────

type screen int

const (
	screenMenu screen = iota
	screenPending
	screenHistory
	screenForm
	screenHooks
	screenHookOutput
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
	cursor        int
	menu          []string
	flash         string
	flashOK       bool
	width, height int

	// live stats for the status bar / menu badges
	pendingCount int
	releaseCount int
	lastRelease  string

	// pending screen
	pending []*change.Change

	// history screen
	releases []*release.Release

	// form state (used for both change and release flows)
	formKind   formKind
	formTitle  string
	formSchema []config.Field
	formFields []formField
	formCursor int

	// hooks screen
	hooks        []hook.Entry
	hookCursor   int
	hookOutput   string
	hookOutTitle string
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
		cfg:    cfg,
		screen: screenMenu,
		menu: []string{
			"Create a change",
			"View pending changes",
			"Cut a release",
			"Unrelease",
			"Release history",
			"Hooks",
			"Quit",
		},
	}
	m.refreshStats()
	return m
}

func (m mainModel) Init() tea.Cmd { return nil }

// menuIcons mirrors `menu` 1:1.
var menuIcons = []string{"✎", "▤", "▲", "↺", "◷", "⚙", "⏻"}

// refreshStats reloads the cached pending / release counts.
func (m *mainModel) refreshStats() {
	if changes, _, err := change.List(m.cfg); err == nil {
		m.pendingCount = len(changes)
	}
	if cl, err := release.Load(m.cfg); err == nil {
		m.releaseCount = len(cl.Releases)
		if len(cl.Releases) > 0 {
			m.lastRelease = cl.Releases[len(cl.Releases)-1].Name
		} else {
			m.lastRelease = ""
		}
	}
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if w, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = w.Width
		m.height = w.Height
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.screen {
	case screenMenu:
		return m.updateMenu(k)
	case screenPending:
		return m.updatePending(k)
	case screenHistory:
		return m.updateHistory(k)
	case screenForm:
		return m.updateForm(k)
	case screenHooks:
		return m.updateHooks(k)
	case screenHookOutput:
		return m.updateHookOutput(k)
	}
	return m, nil
}

func (m mainModel) updateMenu(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.menu)-1 {
			m.cursor++
		}
	case "enter":
		m.flash = ""
		switch m.cursor {
		case 0:
			m.openForm(formChange, "Create a change", m.cfg.Changeset.Fields)
		case 1:
			changes, _, err := change.List(m.cfg)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			m.pending = changes
			m.screen = screenPending
		case 2:
			if changes, _, err := change.List(m.cfg); err != nil || len(changes) == 0 {
				m.flash = "no pending changes to release"
				m.flashOK = false
				return m, nil
			}
			cl, _ := release.Load(m.cfg)
			m.releases = cl.History()
			// Synthesize a "name" field as the first form slot so the same
			// form widget handles both flows.
			schema := make([]config.Field, 0, len(m.cfg.Release.Fields)+1)
			schema = append(schema, config.Field{Name: "name", Type: "string"})
			schema = append(schema, m.cfg.Release.Fields...)
			m.openForm(formRelease, "Cut a release", schema)
		case 3:
			rel, err := release.Unrelease(m.cfg)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			m.flash = fmt.Sprintf("✦ unreleased %s — %d change(s) restored", rel.Name, len(rel.Changes))
			m.flashOK = true
			m.runHook(hook.EventPostUnrelease, rel, rel.Name)
			m.refreshStats()
		case 4:
			cl, err := release.Load(m.cfg)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			m.releases = cl.History()
			m.screen = screenHistory
		case 5:
			if err := m.loadHooks(); err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			m.screen = screenHooks
		case 6:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m mainModel) updatePending(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q", "esc":
		m.screen = screenMenu
	}
	return m, nil
}

func (m mainModel) updateHistory(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q", "esc":
		m.screen = screenMenu
	}
	return m, nil
}

// openForm prepares form state for either the change or release flow and
// switches the screen.
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
	m.screen = screenForm
}

// ── views ────────────────────────────────────────────────────────────────

func (m mainModel) View() string {
	switch m.screen {
	case screenMenu:
		return m.viewMenu()
	case screenPending:
		return m.viewPending()
	case screenHistory:
		return m.viewHistory()
	case screenForm:
		return m.viewForm()
	case screenHooks:
		return m.viewHooks()
	case screenHookOutput:
		return m.viewHookOutput()
	}
	return ""
}

// header renders the persistent brand bar at the top of every screen.
func (m mainModel) header() string {
	brand := titleStyle.Render("✦ wtfc")
	tag := dimStyle.Render("  " + tagline)
	path := mutedStyle.Render(m.cfg.Path)
	w := m.contentWidth()
	rule := mutedStyle.Render(strings.Repeat("─", w))
	return brand + tag + "\n" + path + "\n" + rule + "\n"
}

// footer renders the persistent bottom status bar with live counts and
// the current screen's keybind hints.
func (m mainModel) footer(hints string) string {
	w := m.contentWidth()
	pending := fmt.Sprintf("%d pending", m.pendingCount)
	releases := fmt.Sprintf("%d releases", m.releaseCount)
	last := "no releases yet"
	if m.lastRelease != "" {
		last = "last: " + m.lastRelease
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top,
		chipStyle.Render(pending),
		" ",
		dimStyle.Render(releases+" · "+last),
	)
	right := dimStyle.Render(hints)
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	gap := w - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}
	return statusBarStyle.Width(w).Render(left + strings.Repeat(" ", gap) + right)
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

// contentWidth returns the rendering width, defaulting to 80 if the terminal
// hasn't reported a size yet.
func (m mainModel) contentWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	if w > 100 {
		w = 100
	}
	return w
}

// frame wraps a screen body with the standard header and footer.
func (m mainModel) frame(body, hints string) string {
	return m.header() + "\n" + body + "\n\n" + m.footer(hints)
}

// panel wraps body content in a rounded border. The title becomes a chip
// stamped on the top-left corner.
func (m mainModel) panel(title, body string) string {
	w := m.contentWidth() - 2
	if w < 20 {
		w = 20
	}
	header := h2Style.Render(title)
	inner := header + "\n\n" + body
	return panelStyle.Width(w).Render(inner)
}

func (m mainModel) viewMenu() string {
	var rows []string
	for i, item := range m.menu {
		icon := ""
		if i < len(menuIcons) {
			icon = menuIcons[i]
		}
		// inline badges for high-signal items
		badge := ""
		switch i {
		case 1: // pending
			if m.pendingCount > 0 {
				badge = "  " + chipStyle.Render(fmt.Sprintf("%d", m.pendingCount))
			}
		case 4: // history
			if m.releaseCount > 0 {
				badge = "  " + dimStyle.Render(fmt.Sprintf("(%d)", m.releaseCount))
			}
		}
		row := fmt.Sprintf("  %s  %s%s", icon, item, badge)
		if i == m.cursor {
			row = cursorStyle.Render("▶ ") + menuSelStyle.Render(icon) + "  " +
				menuSelStyle.Render(item) + badge
		}
		rows = append(rows, row)
	}
	body := strings.Join(rows, "\n") + m.flashLine()
	return m.frame(m.panel("Menu", body), "↑/↓  ·  enter  ·  q quit")
}

func (m mainModel) viewPending() string {
	var b strings.Builder
	if len(m.pending) == 0 {
		b.WriteString(dimStyle.Render("Nothing pending. ") +
			bodyStyle.Render("Create a change from the menu to start tracking work."))
	}
	for _, c := range m.pending {
		summary := bodyStyle.Render("(no summary)")
		if v, ok := c.Fields["summary"].(string); ok && v != "" {
			summary = bodyStyle.Render(v)
		}
		short := c.ID
		if len(short) > 8 {
			short = short[:8]
		}
		typeChip := ""
		if t, ok := c.Fields["type"].(string); ok && t != "" {
			typeChip = " " + chipStyle.Render(t)
		}
		b.WriteString(fmt.Sprintf("%s  %s  %s%s\n",
			dimStyle.Render(c.CreatedAt.Local().Format("01-02 15:04")),
			mutedStyle.Render(short), summary, typeChip))
	}
	title := fmt.Sprintf("Pending  %s", chipStyle.Render(fmt.Sprintf("%d", len(m.pending))))
	return m.frame(m.panel(title, b.String()), "esc back")
}

func (m mainModel) viewHistory() string {
	var b strings.Builder
	if len(m.releases) == 0 {
		b.WriteString(dimStyle.Render("No releases yet. ") +
			bodyStyle.Render("Cut your first one from the menu."))
	}
	for i, r := range m.releases {
		marker := mutedStyle.Render("◷")
		if i == 0 {
			marker = okStyle.Render("●") // freshest release glows
		}
		b.WriteString(fmt.Sprintf("%s  %s  %s  %s\n",
			marker,
			dimStyle.Render(r.ReleasedAt.Local().Format("2006-01-02")),
			menuSelStyle.Render(r.Name),
			dimStyle.Render(fmt.Sprintf("· %d changes", len(r.Changes)))))
	}
	title := fmt.Sprintf("History  %s", chipStyle.Render(fmt.Sprintf("%d", len(m.releases))))
	return m.frame(m.panel(title, b.String()), "esc back")
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
		m.screen = screenMenu
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
		m.screen = screenMenu
		m.refreshStats()
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
		m.flash = fmt.Sprintf("✦ released %s with %d change(s)", rel.Name, len(rel.Changes))
		m.flashOK = true
		m.screen = screenMenu
		m.runHook(hook.EventPostRelease, rel, rel.Name)
		m.refreshStats()
		return m, nil
	}
	return m, nil
}

// runHook executes the named hook and folds the result into the flash line.
// On hook failure the flash flips to error mode but the prior action (release
// or unrelease) stays committed — the changelog is authoritative.
func (m *mainModel) runHook(event string, payload any, releaseName string) {
	res, err := hook.Run(m.cfg, event, payload, map[string]string{
		"WTFC_RELEASE_NAME": releaseName,
	})
	if err != nil {
		m.flash = fmt.Sprintf("%s — but %s hook failed: %v", m.flash, event, err)
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
		m.flash += fmt.Sprintf(" · %s hook ran", event)
	}
}

func (m mainModel) viewForm() string {
	var b strings.Builder

	// For the release form, surface previous release names as context.
	if m.formKind == formRelease && len(m.releases) > 0 {
		b.WriteString(dimStyle.Render("Previous releases") + "\n")
		for i, r := range m.releases {
			if i >= 5 {
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
			label = menuSelStyle.Render(f.Name)
		}
		b.WriteString(bullet + label + dimStyle.Render("  "+typeHint(f)) + "\n")
		b.WriteString("      " + m.renderFieldValue(i, f, focused) + "\n")
	}
	submitFocused := m.formCursor == len(m.formSchema)
	submitLabel := dimStyle.Render("[ submit ]")
	if submitFocused {
		submitLabel = okStyle.Render("[ ✦ submit ]")
	}
	b.WriteString("\n  " + submitLabel + "\n")
	b.WriteString(m.flashLine())
	return m.frame(m.panel(m.formTitle, b.String()), m.formHints())
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
		m.screen = screenMenu
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
	res, runErr := hook.Run(m.cfg, entry.Event, last, map[string]string{
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

func (m mainModel) viewHooks() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render(hook.HooksDir(m.cfg)) + "\n\n")
	if len(m.hooks) == 0 {
		b.WriteString(dimStyle.Render("No files in hooks/. ") +
			bodyStyle.Render("Drop an executable named for an event to wire one up."))
	}
	for i, h := range m.hooks {
		bullet := "  "
		name := bodyStyle.Render(h.Name)
		if i == m.hookCursor {
			bullet = cursorStyle.Render("▶ ")
			name = menuSelStyle.Render(h.Name)
		}
		b.WriteString(bullet + hookStatusGlyph(h.Status) + "  " + name + "\n")
		b.WriteString("      " + dimStyle.Render(hookStatusDescription(h)) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("Known events:  ") +
		bodyStyle.Render(strings.Join(hook.KnownEvents, "  ·  ")))
	b.WriteString(m.flashLine())
	return m.frame(m.panel("Hooks", b.String()),
		"↑/↓  ·  e enable  ·  t test  ·  r refresh  ·  esc back")
}

func (m mainModel) viewHookOutput() string {
	body := m.hookOutput
	if body == "" {
		body = dimStyle.Render("(no output)")
	}
	return m.frame(m.panel(m.hookOutTitle, body), "esc back")
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
