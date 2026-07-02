// Package ui is the interactive shell: roaming the forest, inspecting towns,
// the groundskeeper's almanac (decay preview), and help. All overlays are
// drawn onto the same canvas as the world, so there is exactly one visual
// pipeline.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/model"
)

const (
	minW, minH = 80, 22
	stepDots   = 18.0
	strideDots = 64.0
	// The camera glides at moveFPS; a settled forest breathes at idleFPS to
	// keep idle CPU low. Wind runs on wall-clock time, so the switch is
	// invisible.
	moveFPS = 15
	idleFPS = 6
)

type mode int

const (
	roam mode = iota
	inspect
	almanac
	helpView
)

type tickMsg time.Time

// Model is the Bubble Tea model for the whole app.
type Model struct {
	world  *forest.World
	canv   *canvas.Canvas
	w, h   int
	ready  bool
	mode   mode
	start  time.Time
	now    time.Time
	cam    float64
	vel    float64
	target float64
	spring harmonica.Spring
	focus  *forest.Site
	labbed *forest.Site // town locked by the almanac
	hint   bool
}

// New builds the UI over a laid-out world.
func New(w *forest.World) Model {
	return Model{
		world:  w,
		start:  time.Now(),
		now:    time.Now(),
		spring: harmonica.NewSpring(harmonica.FPS(moveFPS), 5.5, 0.9),
		hint:   true,
	}
}

func (m Model) Init() tea.Cmd { return tick(moveFPS) }

func tick(fps int) tea.Cmd {
	return tea.Tick(time.Second/time.Duration(fps), func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) dotW() float64 { return float64(m.w * 2) }

func (m *Model) clampTarget() {
	max := float64(m.world.Width) - m.dotW()
	if max < 0 {
		// World narrower than the window: hold it centered.
		m.target = max / 2
		return
	}
	if m.target < 0 {
		m.target = 0
	}
	if m.target > max {
		m.target = max
	}
}

func (m *Model) centerOn(s *forest.Site) {
	if s == nil {
		return
	}
	m.target = float64(s.SignX) - m.dotW()/2
	m.clampTarget()
}

func (m *Model) siteIndex(s *forest.Site) int {
	for i, x := range m.world.Sites {
		if x == s {
			return i
		}
	}
	return -1
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.canv == nil {
			m.canv = canvas.New(m.w, m.h, canvas.DetectProfile())
		} else {
			m.canv.Resize(m.w, m.h)
		}
		if !m.ready {
			m.ready = true
			// First light: land on the most recently tended town, under the lantern.
			if s := m.world.SpotSite(time.Now()); s != nil {
				m.centerOn(s)
				m.cam = m.target
			}
		}
		m.clampTarget()
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		moving := absf(m.cam-m.target) > 0.4 || absf(m.vel) > 0.4
		if moving {
			m.cam, m.vel = m.spring.Update(m.cam, m.vel, m.target)
		} else {
			m.cam, m.vel = m.target, 0
		}
		m.focus = m.world.NearestSite(m.cam + m.dotW()/2)
		if moving {
			return m, tick(moveFPS)
		}
		return m, tick(idleFPS)

	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k != "?" {
		m.hint = false
	}

	// Overlay-local keys first.
	if m.mode == almanac && m.labbed != nil {
		if handled, mm := m.almanacKey(k); handled {
			return mm, nil
		}
	}

	switch k {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if m.mode != roam {
			m.mode = roam
			m.labbed = nil
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		m.mode = roam
		m.labbed = nil
	case "left", "h":
		m.target -= stepDots
		m.clampTarget()
	case "right", "l":
		m.target += stepDots
		m.clampTarget()
	case "shift+left", "H":
		m.target -= strideDots
		m.clampTarget()
	case "shift+right", "L":
		m.target += strideDots
		m.clampTarget()
	case "tab", "n":
		if i := m.siteIndex(m.focus); i >= 0 && i < len(m.world.Sites)-1 {
			m.centerOn(m.world.Sites[i+1])
		}
	case "shift+tab", "p":
		if i := m.siteIndex(m.focus); i > 0 {
			m.centerOn(m.world.Sites[i-1])
		}
	case "g":
		if len(m.world.Sites) > 0 {
			m.centerOn(m.world.Sites[0])
		}
	case "G":
		if n := len(m.world.Sites); n > 0 {
			m.centerOn(m.world.Sites[n-1])
		}
	case "enter", "i":
		if m.mode == inspect {
			m.mode = roam
		} else {
			m.mode = inspect
			m.labbed = nil
		}
	case "f":
		if m.focus != nil {
			m.focus.Town.Finished = !m.focus.Town.Finished
			if m.focus.Town.Finished {
				m.focus.Town.IdleOverride = nil
			}
		}
	case "d":
		if m.mode == almanac {
			m.mode = roam
			m.labbed = nil
		} else if m.focus != nil {
			m.mode = almanac
			m.labbed = m.focus
			m.centerOn(m.focus)
		}
	case "?":
		if m.mode == helpView {
			m.mode = roam
		} else {
			m.mode = helpView
			m.labbed = nil
		}
	}
	return m, nil
}

// almanacKey adjusts the locked town's preview idle time.
func (m Model) almanacKey(k string) (bool, Model) {
	t := m.labbed.Town
	cur := t.Idle(m.now)
	set := func(d time.Duration) Model {
		if d < 0 {
			d = 0
		}
		t.IdleOverride = &d
		return m
	}
	day := 24 * time.Hour
	switch k {
	case "+", "=":
		return true, set(cur + day)
	case "-", "_":
		return true, set(cur - day)
	case ">", ".":
		return true, set(cur + 30*day)
	case "<", ",":
		return true, set(cur - 30*day)
	case "]":
		return true, set(cur + 365*day)
	case "[":
		return true, set(cur - 365*day)
	case "0":
		t.IdleOverride = nil
		return true, m
	case "1", "2", "3", "4", "5", "6":
		depths := map[string]float64{"1": 0, "2": 0.15, "3": 0.37, "4": 0.62, "5": 0.85, "6": 0.965}
		return true, set(model.IdleForDecay(depths[k]))
	}
	return false, m
}

func (m Model) View() string {
	if !m.ready || m.canv == nil {
		return ""
	}
	if m.w < minW || m.h < minH {
		return m.tooSmall()
	}
	m.world.Render(m.canv, forest.Frame{
		Cam:   m.cam,
		T:     time.Since(m.start).Seconds(),
		Now:   m.now,
		Focus: m.focus,
		Spot:  m.world.SpotSite(m.now),
	})
	switch m.mode {
	case inspect:
		m.drawInspect()
	case almanac:
		m.drawAlmanac()
	case helpView:
		m.drawHelp()
	}
	if m.hint {
		hint := "← → wander · tab towns · enter inspect · ? help"
		m.canv.Text((m.w-len([]rune(hint)))/2, m.h-1, hint, 88, 0)
	}
	return m.canv.Render()
}

func (m Model) tooSmall() string {
	lines := []string{
		"agentforest needs a little more room",
		fmt.Sprintf("at least %dx%d · this window is %dx%d", minW, minH, m.w, m.h),
	}
	pad := (m.h - len(lines)) / 2
	var b strings.Builder
	for i := 0; i < m.h; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		li := i - pad
		if li >= 0 && li < len(lines) {
			s := lines[li]
			left := (m.w - len([]rune(s))) / 2
			if left < 0 {
				left = 0
			}
			b.WriteString(strings.Repeat(" ", left))
			b.WriteString(s)
		}
	}
	return b.String()
}

// ---- overlays -------------------------------------------------------------

type line struct {
	text string
	lvl  uint8
	acc  uint8
}

// panel clears a rectangle and draws framed lines onto the canvas.
func (m Model) panel(lines []line) {
	w := 0
	for _, l := range lines {
		if n := len([]rune(l.text)); n > w {
			w = n
		}
	}
	w += 4
	h := len(lines) + 2
	x0 := (m.w - w) / 2
	y0 := m.h - h - 2
	m.canv.ClearRect(x0-1, y0-1, w+2, h+2)
	m.canv.Rune(x0, y0, '╭', 100)
	m.canv.Rune(x0+w-1, y0, '╮', 100)
	m.canv.Rune(x0, y0+h-1, '╰', 100)
	m.canv.Rune(x0+w-1, y0+h-1, '╯', 100)
	for x := 1; x < w-1; x++ {
		m.canv.Rune(x0+x, y0, '─', 100)
		m.canv.Rune(x0+x, y0+h-1, '─', 100)
	}
	for y := 1; y < h-1; y++ {
		m.canv.Rune(x0, y0+y, '│', 100)
		m.canv.Rune(x0+w-1, y0+y, '│', 100)
	}
	for i, l := range lines {
		m.canv.Text(x0+2, y0+1+i, l.text, l.lvl, l.acc)
	}
}

func (m Model) drawInspect() {
	if m.focus == nil {
		return
	}
	t := m.focus.Town
	d := t.Decay(m.now)
	mix := topMix(t.Mix, 3)
	lines := []line{
		{t.Name, 230, 235},
		{fmt.Sprintf("planted %s · %s", t.FirstTS.Format("january 2006"), age(m.now.Sub(t.FirstTS))), 150, 0},
		{mix, 150, 0},
		{fmt.Sprintf("%s commits · %s", commas(t.TotalCommits), plural(len(t.Tags), "release")), 150, 0},
		{"last tended " + ago(t.Idle(m.now)), 150, 0},
		{model.StageLine(model.StageOf(d), t.Finished), 175, 60},
	}
	m.panel(lines)
}

func (m Model) drawAlmanac() {
	if m.labbed == nil {
		return
	}
	t := m.labbed.Town
	d := t.Decay(m.now)
	idleLine := "idle " + ago(t.Idle(m.now))
	if t.IdleOverride == nil {
		idleLine += " (real)"
	} else {
		idleLine += " (preview)"
	}
	lines := []line{
		{"groundskeeper's almanac · " + t.Name, 230, 235},
		{idleLine, 150, 0},
		{model.StageLine(model.StageOf(d), t.Finished), 175, 60},
		{"", 0, 0},
		{"+/- day   </> month   [/] year   1-6 stages", 115, 0},
		{"0 back to real time   esc done", 115, 0},
	}
	m.panel(lines)
}

func (m Model) drawHelp() {
	lines := []line{
		{"agentforest", 230, 235},
		{"", 0, 0},
		{"wander     ← → or h l · shift strides", 150, 0},
		{"towns      tab / shift+tab · g oldest · G newest", 150, 0},
		{"inspect    enter or i · numbers live here only", 150, 0},
		{"finished   f · freeze a town as a monument", 150, 0},
		{"almanac    d · preview the years of neglect", 150, 0},
		{"quit       q", 150, 0},
	}
	m.panel(lines)
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// ---- humanizing -----------------------------------------------------------

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func ago(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "just now"
	case d < 100*time.Minute:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 40*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 21*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	case d < 10*7*24*time.Hour:
		return plural(int(d.Hours()/(24*7)), "week") + " ago"
	case d < 20*30*24*time.Hour:
		return plural(int(d.Hours()/(24*30.4)), "month") + " ago"
	default:
		return plural(int(d.Hours()/(24*365.25)), "year") + " ago"
	}
}

func age(d time.Duration) string {
	switch {
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour") + " old"
	case d < 8*7*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " old"
	case d < 20*30*24*time.Hour:
		return plural(int(d.Hours()/(24*30.4)), "month") + " old"
	default:
		return plural(int(d.Hours()/(24*365.25)), "year") + " old"
	}
}

func topMix(mix map[string]float64, n int) string {
	type kv struct {
		k string
		v float64
	}
	var all []kv
	for k, v := range mix {
		all = append(all, kv{k, v})
	}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].v > all[i].v || (all[j].v == all[i].v && all[j].k < all[i].k) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if len(all) > n {
		all = all[:n]
	}
	parts := make([]string, 0, len(all))
	for _, e := range all {
		parts = append(parts, fmt.Sprintf("%s %d", e.k, int(e.v*100+0.5)))
	}
	return strings.Join(parts, " · ")
}
