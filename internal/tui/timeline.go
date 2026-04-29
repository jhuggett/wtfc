package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jhuggett/wtfc/internal/change"
	"github.com/jhuggett/wtfc/internal/config"
	"github.com/jhuggett/wtfc/internal/release"
)

// ── timeline node model ───────────────────────────────────────────────────
//
// The timeline shows (top → bottom):
//
//   • an "add change" tick (always)
//   • a "next release" medallion (if pending > 0)
//   • one card per pending change (newest pending first)
//   • a "now" divider
//   • each release medallion (newest first), collapsible
//
// Cursor steps between *focusable* nodes only (skipping spine glyphs and
// expanded-release children).

type tlKind int

const (
	tlAddChange tlKind = iota
	tlNextRelease
	tlPendingChange
	tlRelease
)

type tlNode struct {
	kind tlKind
	idx  int // index into m.pending or m.releases (when applicable)
}

// timelineNodes builds the focusable-node list, top-to-bottom.
func (m mainModel) timelineNodes() []tlNode {
	nodes := []tlNode{{kind: tlAddChange}}
	if len(m.pending) > 0 {
		nodes = append(nodes, tlNode{kind: tlNextRelease})
		for i := range m.pending {
			nodes = append(nodes, tlNode{kind: tlPendingChange, idx: i})
		}
	}
	for i := range m.releases {
		nodes = append(nodes, tlNode{kind: tlRelease, idx: i})
	}
	return nodes
}

// ── geometry ──────────────────────────────────────────────────────────────

const (
	tlCardMaxW   = 56
	tlMedallionW = 28
)

func (m mainModel) tlCardWidth() int {
	avail := m.termWidth() - 4
	if avail > tlCardMaxW {
		return tlCardMaxW
	}
	if avail < 30 {
		return 30
	}
	return avail
}

func (m mainModel) tlMedallionWidth() int {
	avail := m.termWidth() - 4
	if avail > tlMedallionW {
		return tlMedallionW
	}
	if avail < 20 {
		return 20
	}
	return avail
}

// spineCol is the center column the timeline aligns to. All centered
// elements (cards, medallions, spine connectors) place their visual
// center on this column. Using a single anchor avoids off-by-one drift
// between blocks of different widths.
func (m mainModel) spineCol() int {
	return m.termWidth() / 2
}

// padToSpine returns left-padding spaces such that a block of width w
// centers its midpoint on the spine column.
func (m mainModel) padToSpine(w int) string {
	off := m.spineCol() - w/2
	if off < 0 {
		return ""
	}
	return strings.Repeat(" ", off)
}

// centerPad returns symmetric centering for arbitrary blocks not anchored
// to the spine (e.g. the "now" divider). For spine-aligned content use
// padToSpine instead.
func (m mainModel) centerPad(w int) string {
	off := (m.termWidth() - w) / 2
	if off < 0 {
		return ""
	}
	return strings.Repeat(" ", off)
}

// spineLine renders one line containing only a connector glyph at the
// spine column.
func (m mainModel) spineLine(glyph string, style lipgloss.Style) string {
	return m.padToSpine(1) + style.Render(glyph)
}

// ── card chrome ───────────────────────────────────────────────────────────
//
// Cards use ┴/┬ at the top/bottom center column so the spine line above
// and below connects cleanly through the card. Square corners (┌┐└┘) are
// used for "data" — pending changes, cut releases. Rounded corners
// (╭╮╰╯) are used for "affordances" — the + add tick and the next-release
// medallion. The shape difference signals what's actionable.

// cardTop builds a top border. connector=true uses ┴ at center (so a
// spine `│` above can pierce the border cleanly); connector=false uses
// a flat ─ instead, so the topmost element doesn't have an orphaned
// stub pointing up to nothing.
func cardTop(w int, style lipgloss.Style, rounded, connector bool) string {
	if w < 4 {
		w = 4
	}
	left, right := "┌", "┐"
	if rounded {
		left, right = "╭", "╮"
	}
	inner := w - 2
	center := inner / 2
	var sb strings.Builder
	sb.WriteString(left)
	for i := 0; i < inner; i++ {
		if i == center && connector {
			sb.WriteString("┴")
		} else {
			sb.WriteString("─")
		}
	}
	sb.WriteString(right)
	return style.Render(sb.String())
}

// cardBot builds a bottom border. connector=true uses ┬ at center for
// outgoing spine; connector=false uses a flat ─ so the bottommost
// element doesn't dangle a stub.
func cardBot(w int, style lipgloss.Style, rounded, connector bool) string {
	if w < 4 {
		w = 4
	}
	left, right := "└", "┘"
	if rounded {
		left, right = "╰", "╯"
	}
	inner := w - 2
	center := inner / 2
	var sb strings.Builder
	sb.WriteString(left)
	for i := 0; i < inner; i++ {
		if i == center && connector {
			sb.WriteString("┬")
		} else {
			sb.WriteString("─")
		}
	}
	sb.WriteString(right)
	return style.Render(sb.String())
}

// cardRow builds one body row of a card. content may already carry ANSI
// styling; we use lipgloss.Width to measure visible width and pad to fit.
// If content is wider than the inner area, we truncate visible characters
// (best-effort — prefer keeping content concise upstream).
func cardRow(content string, w int, borderStyle lipgloss.Style) string {
	inner := w - 2
	pad := 2
	body := inner - pad*2
	vw := lipgloss.Width(content)
	if vw < body {
		content = content + strings.Repeat(" ", body-vw)
	}
	// We don't truncate in code (ANSI-safe truncate is non-trivial); callers
	// are expected to keep content within body width. If they don't, the
	// card will visually stretch, which is acceptable for a v1.
	return borderStyle.Render("│") +
		strings.Repeat(" ", pad) + content + strings.Repeat(" ", pad) +
		borderStyle.Render("│")
}

// centerStr pads s with spaces so it occupies exactly w cols, content
// centered. Truncation is best-effort.
func centerStr(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return s
	}
	left := (w - sw) / 2
	right := w - sw - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// ── card spec → rendered string ───────────────────────────────────────────

type cardSpec struct {
	headline string   // pre-styled
	sublines []string // pre-styled
}

// borderStyle returns the chrome color for unfocused/focused state.
// Unfocused = neutral grey (lineStyle); focused = brand yellow + bold
// (tagStyle). The grey/yellow contrast is the cursor-visibility cue.
func borderStyle(focused bool) lipgloss.Style {
	if focused {
		return tagStyle
	}
	return lineStyle
}

// renderCard renders a change card as 4 rows: top border, headline, meta,
// bottom border. Breathing comes from the spine `│` between cards, not
// from a blank inside row. width=0 uses the default card width; pass a
// smaller value for nested (released-children) cards. topConn/botConn
// control whether the borders show spine connectors.
func (m mainModel) renderCard(spec cardSpec, focused bool, width int, topConn, botConn bool) string {
	w := width
	if w <= 0 {
		w = m.tlCardWidth()
	}
	bs := borderStyle(focused)
	rows := []string{
		cardTop(w, bs, false, topConn),
		cardRow(spec.headline, w, bs),
	}
	for _, ln := range spec.sublines {
		rows = append(rows, cardRow(ln, w, bs))
	}
	rows = append(rows, cardBot(w, bs, false, botConn))
	pad := m.padToSpine(w)
	for i, r := range rows {
		rows[i] = pad + r
	}
	return strings.Join(rows, "\n")
}

// medallionRows returns the medallion as a slice of pre-styled rows
// (top + content + bot) WITHOUT any horizontal padding. Used both by
// renderMedallion (which adds spine pad to center) and renderForkRow
// (which positions two medallions side-by-side at custom columns).
func (m mainModel) medallionRows(lines []string, focused, rounded, topConn, botConn bool) []string {
	w := m.tlMedallionWidth()
	bs := borderStyle(focused)
	labelStyle := bodyStyle.Bold(true)
	if focused {
		labelStyle = tagStyle.Bold(true)
	}
	inner := w - 2
	pad := 2
	body := inner - pad*2

	// Tier the content rows by dimness: line 0 = label (bold/focused),
	// line 1 = primary detail (dim cream), line 2+ = secondary detail
	// (muted grey). Reads as "what · when · how many" descending.
	rowStyle := func(i int) lipgloss.Style {
		switch i {
		case 0:
			return labelStyle
		case 1:
			return dimStyle
		default:
			return mutedStyle
		}
	}

	rows := []string{cardTop(w, bs, rounded, topConn)}
	for i, ln := range lines {
		rows = append(rows,
			bs.Render("│")+
				strings.Repeat(" ", pad)+
				rowStyle(i).Render(centerStr(ln, body))+
				strings.Repeat(" ", pad)+
				bs.Render("│"))
	}
	rows = append(rows, cardBot(w, bs, rounded, botConn))
	return rows
}

// renderMedallion centers a medallion on the spine column. Rounded
// corners signal an affordance (action), square = data. topConn/botConn
// control whether the top/bottom border has a spine connector — set to
// false when nothing is rendered above/below so we don't leave an
// orphaned stub pointing into empty space.
func (m mainModel) renderMedallion(lines []string, focused, rounded, topConn, botConn bool) string {
	rows := m.medallionRows(lines, focused, rounded, topConn, botConn)
	pad := m.padToSpine(m.tlMedallionWidth())
	for i, r := range rows {
		rows[i] = pad + r
	}
	return strings.Join(rows, "\n")
}

// tlForkMinWidth is the terminal width below which we stack the
// add-change and next-release medallions vertically instead of forking
// them. Below this we don't have room for two medallions side-by-side.
const tlForkMinWidth = 64

// renderForkRow places two medallions equidistant from the spine column
// — left = + add change, right = next release — and joins their bottoms
// into the spine continuation below via a Y-shaped merge. Visualizes
// the "fork in the road" decision at the top of the timeline.
//
// Returns the multi-line string (medallion rows + drop row + merge row).
func (m mainModel) renderForkRow(leftLines, rightLines []string, leftFocused, rightFocused bool) string {
	medW := m.tlMedallionWidth()
	spine := m.spineCol()

	// Symmetric placement around the spine. gap = 4 cols of empty space
	// between the inner edges, so the spine sits at the gap's midpoint.
	const gap = 4
	offset := medW/2 + gap/2
	leftCenterCol := spine - offset
	rightCenterCol := spine + offset
	leftStart := leftCenterCol - medW/2
	rightStart := rightCenterCol - medW/2

	// Fork medallions sit at the very top of the timeline — nothing
	// above, drop line below. So topConn=false (no orphan stub), botConn=true.
	leftRows := m.medallionRows(leftLines, leftFocused, true, false, true)
	rightRows := m.medallionRows(rightLines, rightFocused, true, false, true)

	n := len(leftRows)
	if len(rightRows) > n {
		n = len(rightRows)
	}

	var combined []string
	gapBetween := strings.Repeat(" ", rightStart-(leftStart+medW))
	leftPad := strings.Repeat(" ", leftStart)
	for i := 0; i < n; i++ {
		lp := strings.Repeat(" ", medW)
		if i < len(leftRows) {
			lp = leftRows[i]
		}
		rp := strings.Repeat(" ", medW)
		if i < len(rightRows) {
			rp = rightRows[i]
		}
		combined = append(combined, leftPad+lp+gapBetween+rp)
	}

	// Drop row: vertical line below each medallion's center.
	{
		var sb strings.Builder
		sb.WriteString(strings.Repeat(" ", leftCenterCol))
		sb.WriteString(lineStyle.Render("│"))
		sb.WriteString(strings.Repeat(" ", rightCenterCol-leftCenterCol-1))
		sb.WriteString(lineStyle.Render("│"))
		combined = append(combined, sb.String())
	}

	// Merge row: └─…─┬─…─┘ joining both verticals into the spine.
	{
		var content strings.Builder
		content.WriteString("└")
		for c := leftCenterCol + 1; c < rightCenterCol; c++ {
			if c == spine {
				content.WriteString("┬")
			} else {
				content.WriteString("─")
			}
		}
		content.WriteString("┘")
		combined = append(combined,
			strings.Repeat(" ", leftCenterCol)+lineStyle.Render(content.String()))
	}

	return strings.Join(combined, "\n")
}

// ── relative time ────────────────────────────────────────────────────────

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 0:
		return t.Format("2006-01-02")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// ── per-card content builders ────────────────────────────────────────────

// changeMetaParts returns the styled meta-line segments for a change
// card (schema-driven enum/list/string values plus relative time). The
// renderer joins them with " · " separators and wraps onto multiple
// lines when the result doesn't fit the card body. Schema-generic —
// adding `author` or `commit` to config.toml makes them appear here.
func (m mainModel) changeMetaParts(c *change.Change) []string {
	var parts []string
	for _, f := range m.cfg.Changeset.Fields {
		if f.Name == "summary" {
			continue
		}
		v := c.Fields[f.Name]
		if v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) == "" {
				continue
			}
		case []any:
			if len(x) == 0 {
				continue
			}
		case []string:
			if len(x) == 0 {
				continue
			}
		}
		parts = append(parts, formatFieldValue(f, v))
	}
	parts = append(parts, mutedStyle.Render(relativeTime(c.CreatedAt)))
	return parts
}

// wrapMetaParts joins parts with " · " separators, breaking onto a new
// line whenever the next part would overflow the max width. Each part
// stays whole — wrapping happens at separators only, never mid-value.
func wrapMetaParts(parts []string, max int) []string {
	if len(parts) == 0 {
		return nil
	}
	sep := mutedStyle.Render(" · ")
	sepW := lipgloss.Width(sep)
	var lines []string
	var cur strings.Builder
	curW := 0
	for _, p := range parts {
		pW := lipgloss.Width(p)
		switch {
		case curW == 0:
			cur.WriteString(p)
			curW = pW
		case curW+sepW+pW <= max:
			cur.WriteString(sep)
			cur.WriteString(p)
			curW += sepW + pW
		default:
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(p)
			curW = pW
		}
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// renderChangeCard renders a pending change. Hollow diamond (◇) marks
// it as in-flux — distinct from the filled ◆ used on released changes,
// which signals "sealed". Pending cards always have spine connectors on
// both sides (they're never the topmost or bottommost element).
func (m mainModel) renderChangeCard(c *change.Change, focused bool) string {
	return m.renderChangeCardInner(c, focused, "◇", true, true)
}

// renderChangeCardInner sizes a change card to fit its content. Card
// width is capped at tlCardMaxW so cards don't sprawl across wide
// terminals; long summaries and meta lines both wrap onto continuation
// rows so cards grow vertically rather than horizontally.
func (m mainModel) renderChangeCardInner(c *change.Change, focused bool, marker string, topConn, botConn bool) string {
	summary := ""
	if s, ok := c.Fields["summary"].(string); ok {
		summary = strings.TrimSpace(s)
	}
	if summary == "" {
		summary = "(no summary)"
	}
	parts := m.changeMetaParts(c)

	// Card width: pick the widest of headline-with-summary or longest
	// single meta part, clamped between a min and the cap. We use the
	// longest *part*, not the joined meta — wrapping handles the rest.
	headlineW := 3 + lipgloss.Width(summary)
	contentW := headlineW
	for _, p := range parts {
		if pw := lipgloss.Width(p); pw > contentW {
			contentW = pw
		}
	}
	w := contentW + 6
	if w < 40 {
		w = 40
	}
	if w > tlCardMaxW {
		w = tlCardMaxW
	}
	maxW := m.termWidth() - 4
	if w > maxW {
		w = maxW
	}

	body := w - 6
	const prefixW = 3
	textW := body - prefixW
	if textW < 8 {
		textW = 8
	}
	summaryLines := wrapText(summary, textW)
	metaLines := wrapMetaParts(parts, body)

	headline := tagStyle.Render(marker) + "  " + bodyStyle.Bold(true).Render(summaryLines[0])
	var sublines []string
	for _, cont := range summaryLines[1:] {
		sublines = append(sublines, "   "+bodyStyle.Bold(true).Render(cont))
	}
	sublines = append(sublines, metaLines...)

	return m.renderCard(cardSpec{
		headline: headline,
		sublines: sublines,
	}, focused, w, topConn, botConn)
}

// wrapText word-wraps s into lines of at most max visible columns. A
// single word longer than max gets its own line (and overflows) — fine
// for typical changelog summaries.
func wrapText(s string, max int) []string {
	if max <= 0 || lipgloss.Width(s) <= max {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	curW := 0
	for _, w := range words {
		ww := lipgloss.Width(w)
		switch {
		case curW == 0:
			cur.WriteString(w)
			curW = ww
		case curW+1+ww <= max:
			cur.WriteByte(' ')
			cur.WriteString(w)
			curW += 1 + ww
		default:
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(w)
			curW = ww
		}
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}


// ── full body rendering ───────────────────────────────────────────────────

// renderExpandedRelease draws the entire expanded release as a single
// rounded box: header section with name + date centered, an internal
// divider, then each child as a left-aligned headline + meta. The
// release "grows" to contain its changes — the medallion shape goes
// away in favor of one unified container.
func (m mainModel) renderExpandedRelease(r *release.Release, focused, topConn, botConn bool) string {
	name := r.Name
	date := r.ReleasedAt.Local().Format("2006-01-02")

	type childRow struct {
		summary string
		parts   []string // styled meta segments; wrapped at render time
	}
	children := make([]childRow, 0, len(r.Changes))
	for _, c := range r.Changes {
		summary := ""
		if s, ok := c.Fields["summary"].(string); ok {
			summary = strings.TrimSpace(s)
		}
		if summary == "" {
			summary = "(no summary)"
		}
		children = append(children, childRow{summary, m.changeMetaParts(c)})
	}

	// Box width: target tlCardMaxW so long summaries wrap on a comfortable
	// column width instead of stretching the box (or cramping it when meta
	// parts are short). We deliberately do NOT include child summary widths
	// here — those wrap at render time. Meta parts still factor in so the
	// box is wide enough for the longest single part if it exceeds the cap.
	contentW := tlCardMaxW
	if w := lipgloss.Width(name); w > contentW {
		contentW = w
	}
	if w := lipgloss.Width(date); w > contentW {
		contentW = w
	}
	for _, ch := range children {
		for _, p := range ch.parts {
			if w := lipgloss.Width(p); w > contentW {
				contentW = w
			}
		}
	}
	maxContent := m.termWidth() - 4 - 2 - 6 // outer margin, borders, inner pad
	if maxContent > 0 && contentW > maxContent {
		contentW = maxContent
	}
	if contentW < 32 {
		contentW = 32
	}

	const innerPad = 3
	boxInnerW := contentW + innerPad*2
	boxOuterW := boxInnerW + 2
	bs := borderStyle(focused)

	boxLeft := m.spineCol() - boxOuterW/2
	if boxLeft < 0 {
		boxLeft = 0
	}
	leftPad := strings.Repeat(" ", boxLeft)

	nameStyle := bodyStyle.Bold(true)
	if focused {
		nameStyle = tagStyle.Bold(true)
	}

	centeredRow := func(content string) string {
		cw := lipgloss.Width(content)
		left := boxInnerW/2 - cw/2
		if left < 0 {
			left = 0
		}
		right := boxInnerW - left - cw
		if right < 0 {
			right = 0
		}
		return leftPad + bs.Render("│") +
			strings.Repeat(" ", left) + content + strings.Repeat(" ", right) +
			bs.Render("│")
	}

	leftRow := func(content string) string {
		cw := lipgloss.Width(content)
		right := boxInnerW - innerPad - cw
		if right < 0 {
			right = 0
		}
		return leftPad + bs.Render("│") +
			strings.Repeat(" ", innerPad) + content + strings.Repeat(" ", right) +
			bs.Render("│")
	}

	divider := leftPad + bs.Render("├"+strings.Repeat("─", boxInnerW)+"┤")

	out := []string{leftPad + cardTop(boxOuterW, bs, true, topConn)}
	out = append(out, centeredRow(nameStyle.Render(name)))
	out = append(out, centeredRow(dimStyle.Render(date)))
	out = append(out, divider)
	// Inner row width available for wrapped content = boxInnerW - 2*innerPad.
	metaW := boxInnerW - innerPad*2
	if metaW < 8 {
		metaW = 8
	}
	const markerPrefix = 3 // "◆  "
	summaryW := metaW - markerPrefix
	if summaryW < 8 {
		summaryW = 8
	}
	for i, ch := range children {
		if i > 0 {
			out = append(out, leftRow(""))
		}
		summaryLines := wrapText(ch.summary, summaryW)
		out = append(out, leftRow(
			tagStyle.Render("◆")+"  "+bodyStyle.Bold(true).Render(summaryLines[0])))
		for _, cont := range summaryLines[1:] {
			out = append(out, leftRow("   "+bodyStyle.Bold(true).Render(cont)))
		}
		for _, ml := range wrapMetaParts(ch.parts, metaW) {
			out = append(out, leftRow(ml))
		}
	}
	out = append(out, leftPad+cardBot(boxOuterW, bs, true, botConn))
	return strings.Join(out, "\n")
}

// wrapInBox surrounds inner timeline lines with a rounded outer
// border. topConn=true puts ┴ at the top-center so an incoming spine
// from above connects through; botConn=true puts ┬ at the bottom-center
// for outgoing spine. Centers itself on the spine column and inner
// lines are recentered so all spine connectors land on the same column.
func (m mainModel) wrapInBox(inner []string, topConn, botConn bool) []string {
	maxW := 0
	for _, l := range inner {
		stripped := strings.TrimLeft(l, " ")
		if w := lipgloss.Width(stripped); w > maxW {
			maxW = w
		}
	}
	if maxW == 0 {
		return inner
	}
	const innerPad = 2
	boxInnerW := maxW + innerPad*2
	boxOuterW := boxInnerW + 2

	// Center the box on the spine column. boxLeft = spineCol - boxOuterW/2
	// places the box's center column exactly on the spine.
	boxLeft := m.spineCol() - boxOuterW/2
	if boxLeft < 0 {
		boxLeft = 0
	}
	bs := lineStyle
	leftPad := strings.Repeat(" ", boxLeft)
	top := leftPad + cardTop(boxOuterW, bs, true, topConn)
	bot := leftPad + cardBot(boxOuterW, bs, true, botConn)

	out := make([]string, 0, len(inner)+2)
	out = append(out, top)
	for _, l := range inner {
		stripped := strings.TrimLeft(l, " ")
		contentW := lipgloss.Width(stripped)
		// Use the same anchor formula as padToSpine: place the content's
		// midpoint on the box's center column. Works for any contentW.
		leftInside := boxInnerW/2 - contentW/2
		if leftInside < 0 {
			leftInside = 0
		}
		rightInside := boxInnerW - leftInside - contentW
		if rightInside < 0 {
			rightInside = 0
		}
		out = append(out, leftPad+
			bs.Render("│")+
			strings.Repeat(" ", leftInside)+stripped+strings.Repeat(" ", rightInside)+
			bs.Render("│"))
	}
	out = append(out, bot)
	return out
}

// renderTimelineBody returns the multi-line timeline string and a map of
// node-index → starting-line in that string. The line map drives scroll
// adjustment so the cursor stays visible.
func (m mainModel) renderTimelineBody() (string, map[int]int) {
	var b strings.Builder
	nodeLine := map[int]int{}
	line := 0
	emit := func(s string) {
		b.WriteString(s)
		b.WriteString("\n")
		line += strings.Count(s, "\n") + 1
	}

	nodes := m.timelineNodes()
	cur := m.timelineCursor
	isCur := func(i int) bool { return i == cur }
	nodeAt := func(i int) tlNode {
		if i >= 0 && i < len(nodes) {
			return nodes[i]
		}
		return tlNode{}
	}

	// Top breathing room so the first node doesn't kiss the header rule.
	emit("")

	nodeIdx := 0
	// Action-verb headline + explicit key hint on the second line so
	// each medallion reads as a callable button rather than a passive
	// label. The pending count still lives on the footer status bar.
	addLines := []string{"track change", "press t"}
	nrLines := []string{"cut release", "press r"}

	// Fork at top: + add change (left) and next release (right) sit on
	// either side of a Y-merge into the spine. Visualizes the user's
	// choice point — extend the queue or commit it. Falls back to
	// stacked when the terminal is too narrow for both medallions.
	if m.useForkLayout() {
		nodeLine[0] = line
		nodeLine[1] = line
		emit(m.renderForkRow(addLines, nrLines, isCur(0), isCur(1)))
		nodeIdx = 2
	} else {
		// Stacked: add change is topmost (no spine above), so topConn
		// = false. Spine continues below to the next medallion or to
		// pending cards.
		nodeLine[0] = line
		emit(m.renderMedallion(addLines, isCur(0), true, false, true))
		emit(m.spineLine("│", lineStyle))
		nodeIdx = 1
		if len(m.pending) > 0 {
			nodeLine[nodeIdx] = line
			emit(m.renderMedallion(nrLines, isCur(nodeIdx), true, true, true))
			nodeIdx++
			emit(m.spineLine("│", lineStyle))
		}
	}

	// Pending change cards: a spine line rides between every pair so the
	// timeline keeps its rhythm: card → │ → card.
	if len(m.pending) > 0 {
		for i, c := range m.pending {
			nodeLine[nodeIdx] = line
			emit(m.renderChangeCard(c, isCur(nodeIdx)))
			nodeIdx++
			if i < len(m.pending)-1 {
				emit(m.spineLine("│", lineStyle))
			}
		}
		emit(m.spineLine("│", lineStyle))
	}

	// Section divider with directional labels: "↑ pending ↑" above the
	// horizontal rule, "↓ released ↓" below. Each label only appears
	// when its section has content — no point telling the user about
	// pending when there's nothing pending.
	{
		dash := lineStyle.Render(strings.Repeat("─", 28))
		if len(m.pending) > 0 {
			pendingLabel := dimStyle.Render("↑ pending ↑")
			emit(m.centerPad(lipgloss.Width(pendingLabel)) + pendingLabel)
		}
		emit(m.centerPad(lipgloss.Width(dash)) + dash)
		if len(m.releases) > 0 {
			releasedLabel := dimStyle.Render("↓ released ↓")
			emit(m.centerPad(lipgloss.Width(releasedLabel)) + releasedLabel)
		}
	}
	if len(m.releases) > 0 {
		emit(m.spineLine("│", lineStyle))
	}

	// release medallions, newest first. Square corners — these are data.
	// Single-expand: at most one release shows children at a time. The
	// expanded one wraps medallion + children in an outer rounded box,
	// visually binding the changes to their release.
	for i, r := range m.releases {
		nodeLine[nodeIdx] = line
		curNode := nodeAt(nodeIdx)
		expanded := m.expandedRelease == curNode.idx
		isLast := i == len(m.releases)-1

		if expanded {
			// Expanded release is one big box: header (name + date) at
			// the top, internal divider, then children as left-aligned
			// rows. The box itself reads as the release; the children
			// are visually contained within it.
			emit(m.renderExpandedRelease(r, isCur(nodeIdx), true, !isLast))
		} else {
			label := "1 change"
			if len(r.Changes) != 1 {
				label = fmt.Sprintf("%d changes", len(r.Changes))
			}
			medallion := []string{r.Name, r.ReleasedAt.Local().Format("2006-01-02"), label}
			medBotConn := !isLast
			emit(m.renderMedallion(medallion, isCur(nodeIdx), false, true, medBotConn))
		}
		nodeIdx++

		if !isLast {
			emit(m.spineLine("│", lineStyle))
		}
	}

	return strings.TrimRight(b.String(), "\n"), nodeLine
}

// ── screen view ──────────────────────────────────────────────────────────

func (m mainModel) viewTimeline() string {
	body, nodeLine := m.renderTimelineBody()
	lines := strings.Split(body, "\n")

	// Compute visible window using the precomputed scroll offset.
	slot := m.bodyHeight()
	scroll := m.timelineScroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > len(lines) {
		scroll = len(lines)
	}
	end := scroll + slot
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[scroll:end]
	_ = nodeLine

	// Pad to full body height so the toast can position relative to the
	// actual bottom edge instead of the timeline's last rendered line.
	for len(visible) < slot {
		visible = append(visible, "")
	}

	if m.flash != "" {
		visible = overlayBottomRight(visible, m.renderToast(), m.termWidth())
	}

	return m.renderScreenCentered(strings.Join(visible, "\n"), m.timelineHints())
}

// renderToast builds the floating toast notification: a small rounded
// box in the bottom-right that displays the latest flash message in
// success/error color. The box is sized by content; a separate auto-
// dismiss timer (flashTickMsg) clears it after flashTTL.
func (m mainModel) renderToast() string {
	if m.flash == "" {
		return ""
	}
	msgStyle := errStyle
	border := cDanger
	if m.flashOK {
		msgStyle = okStyle
		border = cSuccess
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 2)
	return box.Render(msgStyle.Render(m.flash))
}

// overlayBottomRight pins a multi-line overlay to the bottom-right of
// the background slice. Replaces the rows it occupies; rows above show
// the unmodified background. Right margin is 2 cols, bottom margin is 1
// row — keeps the toast off the absolute edge.
func overlayBottomRight(bg []string, overlay string, width int) []string {
	if overlay == "" {
		return bg
	}
	oLines := strings.Split(overlay, "\n")
	oH := len(oLines)
	oW := 0
	for _, l := range oLines {
		if lw := lipgloss.Width(l); lw > oW {
			oW = lw
		}
	}
	startRow := len(bg) - oH - 1
	if startRow < 0 {
		startRow = 0
	}
	leftPad := width - oW - 2
	if leftPad < 0 {
		leftPad = 0
	}
	pad := strings.Repeat(" ", leftPad)
	out := make([]string, len(bg))
	copy(out, bg)
	for i, ol := range oLines {
		row := startRow + i
		if row < 0 || row >= len(out) {
			continue
		}
		out[row] = pad + ol
	}
	return out
}

// bodyHeight is the height of the body area between header and footer.
func (m mainModel) bodyHeight() int {
	h := m.termHeight() - lipgloss.Height(m.header()) - lipgloss.Height(m.footer(""))
	if h < 4 {
		h = 4
	}
	return h
}

// renderScreenCentered composes the same chrome as renderScreen but skips
// the body left-indent — the timeline centers its own content.
func (m mainModel) renderScreenCentered(body, hints string) string {
	w := m.termWidth()
	header := m.header()
	footer := m.footer(hints)
	slot := m.bodyHeight()
	bodyArea := lipgloss.NewStyle().Width(w).Height(slot).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, bodyArea, footer)
}

func (m mainModel) timelineHints() string {
	cur := m.timelineCursor
	nodes := m.timelineNodes()
	if cur >= len(nodes) {
		return "j/k move · t track · h hooks · q quit"
	}
	switch nodes[cur].kind {
	case tlAddChange:
		if m.useForkLayout() {
			return "j/k move · → cut release · enter/t track · h hooks · q quit"
		}
		return "j/k move · enter/t track change · h hooks · q quit"
	case tlNextRelease:
		if m.useForkLayout() {
			return "j/k move · ← track change · enter/r release · h hooks · q quit"
		}
		return "j/k move · enter/r release · h hooks · q quit"
	case tlPendingChange:
		return "j/k move · enter/e edit · d delete · t track · h hooks · q quit"
	case tlRelease:
		hint := "j/k move · enter expand · t track"
		if newestReleaseNodeIndex(nodes) == cur {
			hint += " · u unrelease"
		}
		return hint + " · h hooks · q quit"
	}
	return "j/k move · q quit"
}

// newestReleaseNodeIndex returns the cursor index of the newest release
// node, or -1 if there are no releases.
func newestReleaseNodeIndex(nodes []tlNode) int {
	for i, n := range nodes {
		if n.kind == tlRelease {
			return i // releases listed newest-first, so first match is newest
		}
	}
	return -1
}

// ── update ────────────────────────────────────────────────────────────────

// useForkLayout returns true when the top-of-timeline fork (add-change +
// next-release side-by-side) is active. Same condition is used by the
// renderer; navigation respects it so vertical keys skip past the fork.
func (m mainModel) useForkLayout() bool {
	return len(m.pending) > 0 && m.termWidth() >= tlForkMinWidth
}

// onFork reports whether the cursor is currently on one of the fork
// nodes (add-change or next-release).
func (m mainModel) onFork() bool {
	return m.useForkLayout() && (m.timelineCursor == 0 || m.timelineCursor == 1)
}

// setCursor moves the cursor and updates fork-side memory + scroll.
func (m *mainModel) setCursor(idx int) {
	m.timelineCursor = idx
	if m.useForkLayout() && (idx == 0 || idx == 1) {
		m.lastForkCursor = idx
	}
	m.adjustTimelineScroll()
}

func (m mainModel) updateTimeline(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	nodes := m.timelineNodes()
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		// Coming from the first node below the fork (cursor 2): return
		// to whichever fork side was last visited, not whichever happens
		// to be sequentially-prior. Otherwise step backward by one.
		if m.useForkLayout() && m.timelineCursor == 2 {
			m.setCursor(m.lastForkCursor)
		} else if !m.onFork() && m.timelineCursor > 0 {
			m.setCursor(m.timelineCursor - 1)
		}
	case "down", "j":
		// On fork: skip past both medallions (they're the same row,
		// not stacked). Otherwise step forward by one.
		if m.onFork() {
			if 2 < len(nodes) {
				m.setCursor(2)
			}
		} else if m.timelineCursor < len(nodes)-1 {
			m.setCursor(m.timelineCursor + 1)
		}
	case "left":
		// Only meaningful when on the right side of the fork.
		if m.useForkLayout() && m.timelineCursor == 1 {
			m.setCursor(0)
		}
	case "right":
		// Only meaningful when on the left side of the fork.
		if m.useForkLayout() && m.timelineCursor == 0 {
			m.setCursor(1)
		}
	case "g":
		m.setCursor(0)
	case "G":
		m.setCursor(len(nodes) - 1)
	case "t":
		m.openForm(formChange, "Track a change", m.cfg.Changeset.Fields)
	case "r":
		m.flash = ""
		if len(m.pending) == 0 {
			m.flash = "no pending changes to release"
			m.flashOK = false
			return m, nil
		}
		schema := make([]config.Field, 0, len(m.cfg.Release.Fields)+1)
		schema = append(schema, config.Field{Name: "name", Type: "string"})
		schema = append(schema, m.cfg.Release.Fields...)
		m.openForm(formRelease, "Cut a release", schema)
	case "u":
		m.flash = ""
		if len(m.releases) == 0 {
			m.flash = "no releases to unrelease"
			m.flashOK = false
			return m, nil
		}
		// Only allow unrelease when cursor is on the newest release.
		if m.timelineCursor != newestReleaseNodeIndex(nodes) {
			m.flash = "u unrelease only works on the newest release"
			m.flashOK = false
			return m, nil
		}
		// Destructive — pop a confirmation modal instead of running it
		// straight away. The modal handler (updateConfirm) actually
		// executes the unrelease on `y/enter`.
		newest := m.releases[0]
		m.confirmKind = "unrelease"
		m.confirmPrompt = fmt.Sprintf(
			"Unrelease %s? This will restore its %d change(s) to pending.",
			newest.Name, len(newest.Changes))
		m.screen = screenConfirm
	case "h":
		if err := m.loadHooks(); err != nil {
			m.flash = err.Error()
			m.flashOK = false
			return m, nil
		}
		m.screen = screenHooks
	case "e":
		// Edit only valid on a pending change — released changes are
		// frozen and the medallions don't take field edits.
		if m.timelineCursor < len(nodes) && nodes[m.timelineCursor].kind == tlPendingChange {
			c := m.pending[nodes[m.timelineCursor].idx]
			m.openEditForm(c)
		}
	case "d":
		if m.timelineCursor < len(nodes) && nodes[m.timelineCursor].kind == tlPendingChange {
			c := m.pending[nodes[m.timelineCursor].idx]
			_, path, err := change.FindByID(m.cfg, c.ID)
			if err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			if err := os.Remove(path); err != nil {
				m.flash = err.Error()
				m.flashOK = false
				return m, nil
			}
			short := c.ID
			if len(short) > 8 {
				short = short[:8]
			}
			m.flash = fmt.Sprintf("✗ deleted change %s", short)
			m.flashOK = true
			m.refreshTimeline()
		}
	case "enter", " ":
		if m.timelineCursor >= len(nodes) {
			return m, nil
		}
		switch nodes[m.timelineCursor].kind {
		case tlAddChange:
			m.openForm(formChange, "Track a change", m.cfg.Changeset.Fields)
		case tlNextRelease:
			schema := make([]config.Field, 0, len(m.cfg.Release.Fields)+1)
			schema = append(schema, config.Field{Name: "name", Type: "string"})
			schema = append(schema, m.cfg.Release.Fields...)
			m.openForm(formRelease, "Cut a release", schema)
		case tlPendingChange:
			// Enter on a pending change opens the edit form pre-filled
			// with its current values.
			c := m.pending[nodes[m.timelineCursor].idx]
			m.openEditForm(c)
		case tlRelease:
			// Single-expand: pressing enter on the focused release
			// toggles it; opening one collapses any other.
			idx := nodes[m.timelineCursor].idx
			if m.expandedRelease == idx {
				m.expandedRelease = -1
			} else {
				m.expandedRelease = idx
			}
			m.adjustTimelineScroll()
		}
	}
	return m, nil
}

// refreshTimeline reloads pending changes and releases (e.g., after a
// release/unrelease) and clamps the cursor to the new node count.
// Release indices are positional (newest = 0), so any prior expansion
// state is invalidated when releases shift — clear it.
func (m *mainModel) refreshTimeline() {
	if changes, _, err := change.List(m.cfg); err == nil {
		m.pending = changes
	}
	if cl, err := release.Load(m.cfg); err == nil {
		m.releases = cl.History()
	}
	m.expandedRelease = -1
	nodes := m.timelineNodes()
	if m.timelineCursor >= len(nodes) {
		m.timelineCursor = len(nodes) - 1
	}
	if m.timelineCursor < 0 {
		m.timelineCursor = 0
	}
	m.adjustTimelineScroll()
}

// adjustTimelineScroll keeps the focused node fully visible inside the
// body window. The cursor's region runs from `top` (its starting line)
// down to the line just before the next node at a different line —
// fork-aware, since nodes 0 and 1 share a line in fork layout.
func (m *mainModel) adjustTimelineScroll() {
	body, nodeLine := m.renderTimelineBody()
	cur := m.timelineCursor
	top, ok := nodeLine[cur]
	if !ok {
		return
	}
	totalLines := strings.Count(body, "\n") + 1

	// Scan forward for the next node whose line differs from `top`.
	// Co-located nodes (the fork) share a region, so they all share the
	// same `bottom`.
	nextLine := -1
	for k := cur + 1; ; k++ {
		ln, ok := nodeLine[k]
		if !ok {
			break
		}
		if ln != top {
			nextLine = ln
			break
		}
	}
	bottom := top
	if nextLine > top {
		bottom = nextLine - 1
	} else {
		bottom = totalLines - 1
	}

	slot := m.bodyHeight()
	margin := 1
	if top < m.timelineScroll+margin {
		m.timelineScroll = top - margin
	} else if bottom >= m.timelineScroll+slot-margin {
		m.timelineScroll = bottom - slot + margin + 1
	}
	if m.timelineScroll < 0 {
		m.timelineScroll = 0
	}
	maxScroll := totalLines - slot
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.timelineScroll > maxScroll {
		m.timelineScroll = maxScroll
	}
}
