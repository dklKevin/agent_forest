// Package ui is the interactive shell: roaming the forest, inspecting towns,
// reading a town's almanac (its memoir), previewing the years of neglect,
// connecting roots, and help. All overlays are drawn onto the same canvas as
// the world, so there is exactly one visual pipeline.
package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"

	"github.com/dklKevin/agentforest/internal/almanac"
	"github.com/dklKevin/agentforest/internal/app"
	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/gitscan"
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
	// pollEvery is how often connected repos are checked for new commits
	// while the app is open. The check is stat-only (no processes spawned),
	// so it costs microseconds.
	pollEvery = 2500 * time.Millisecond
	// reviveDur is how long a town takes to shake off its decay when a new
	// commit lands.
	reviveDur = 1800 * time.Millisecond
	// The since-last-visit pulse: on launch, towns that stirred while the
	// forest was closed wake with the same motion a live commit plays. At
	// most maxPulses towns wake - a long absence stirs a handful of the most
	// notable towns, never the whole forest - and they wake one after
	// another, pulseStagger apart, so arriving reads as walking in on a
	// forest stirring rather than a synchronized blink.
	maxPulses    = 5
	pulseStagger = 350 * time.Millisecond
	// pulseFloor is the shallowest depth a pulse wakes from. A town that was
	// bright the whole time still gets a visible stir - the smoke swells, the
	// hearth window brightens - because the waking motion is the whole
	// message; it fades to the exact ordinary frame.
	pulseFloor = 0.12
	// finishDur is how long the laying-to-rest ceremony takes: slow enough
	// to read as a passage, short enough to stay one moment.
	finishDur = 2200 * time.Millisecond
)

type mode int

const (
	roam mode = iota
	inspect
	almanacView // the town's memoir, one deliberate keypress past inspect
	preview     // the neglect preview: scrub years of decay ahead
	helpView
	connectInput
	confirmExclude
	confirmFinish // the threshold panel: lay a town to rest, a word to carve
	ceremony      // the laying-to-rest passage is playing
)

type tickMsg time.Time

// scanKind says why a scan ran, which decides how loudly to report it.
type scanKind int

const (
	scanStartup scanKind = iota // catching up after launch: silent
	scanConnect                 // onboarding or the c key
	scanRefresh                 // the r key
	scanLive                    // fingerprint poll saw a commit
)

type scanDoneMsg struct {
	kind  scanKind
	rep   app.ScanReport
	err   error
	root  string   // connect only
	paths []string // live only: the repos that changed
}

// reviveAnim eases a town from its old decay back to truth after a commit.
// A live revive eases to zero (the commit just landed); a since-last-visit
// pulse eases to the town's real current depth, which may be deeper. A start
// in the future holds the town at from until its moment arrives, which is
// how the launch pulse staggers.
type reviveAnim struct {
	from  float64
	to    float64
	start time.Time
}

// finishAnim is the laying-to-rest ceremony in flight: the inverse of the
// revive beat. Over finishDur the town's decay stills to nothing, the carve
// spreads down the name board, the hearth sends one last plume, and the
// grove eases into its monument symmetry.
type finishAnim struct {
	path     string // repo path; demo towns have none and resolve by name
	name     string
	from     float64            // town decay when the ceremony began
	fromComp map[string]float64 // each building's decay by component path
	start    time.Time
}

// Config wires the UI to a laid-out world and its persistent state.
type Config struct {
	App     *app.App
	World   *forest.World
	Seed    uint64
	Demo    bool // world behind the UI is the demo forest
	Onboard bool // first run: open on the connect panel
	// LastOpened is when the forest was previously open: the since-last-visit
	// pulse wakes towns that stirred after this instant. Zero means first run
	// (or an upgrade from before the stamp existed): no pulse.
	LastOpened time.Time
	// Events is the log the demo world was folded from, so the almanac can
	// read demo towns too; a real forest reads the live app log instead.
	Events []events.Event
}

// Model is the Bubble Tea model for the whole app.
type Model struct {
	world  *forest.World
	app    *app.App
	seed   uint64
	demo   bool
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
	labbed *forest.Site // town locked by the neglect preview
	hint   bool

	onboarding  bool   // connect panel is the first-run welcome
	freshBloom  bool   // a real forest just replaced the demo; recenter on esc
	input       string // path being typed in the connect panel
	inputMsg    string // result line under the input
	scanning    bool   // one scan at a time; also pauses polling
	startupScan bool   // Init reconciles once to catch up on closed-time commits

	fps         map[string]string // repo path -> cheap change fingerprint
	lastPoll    time.Time
	revives     map[string]*reviveAnim // repo path -> transition
	compRevives map[string]*reviveAnim // repo path \x00 component -> transition
	lastOpened  time.Time              // previous opening; feeds the launch pulse
	status      string                 // transient toast at the bottom
	statusAt    time.Time

	epitaph      string      // the line being carved in the threshold panel
	ceremonyAnim *finishAnim // the laying-to-rest in progress

	evs []events.Event // the demo world's log; the almanac's source when demo
}

// New builds the UI over a laid-out world.
func New(cfg Config) Model {
	m := Model{
		world:       cfg.World,
		app:         cfg.App,
		seed:        cfg.Seed,
		demo:        cfg.Demo,
		start:       time.Now(),
		now:         time.Now(),
		spring:      harmonica.NewSpring(harmonica.FPS(moveFPS), 5.5, 0.9),
		hint:        true,
		fps:         map[string]string{},
		revives:     map[string]*reviveAnim{},
		compRevives: map[string]*reviveAnim{},
		lastOpened:  cfg.LastOpened,
		evs:         cfg.Events,
	}
	if cfg.Onboard {
		m.mode = connectInput
		m.onboarding = true
	}
	if !cfg.Demo && cfg.App != nil && cfg.App.Connected() {
		m.startupScan = true
		m.scanning = true
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(moveFPS)}
	if m.startupScan {
		// Catch up on whatever happened while the app was closed.
		cmds = append(cmds, scanCmd(m.app, scanStartup, "", nil))
	}
	return tea.Batch(cmds...)
}

func tick(fps int) tea.Cmd {
	return tea.Tick(time.Second/time.Duration(fps), func(t time.Time) tea.Msg { return tickMsg(t) })
}

// scanCmd runs the git adapter off the UI thread. Only one runs at a time.
func scanCmd(a *app.App, kind scanKind, root string, paths []string) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		var rep app.ScanReport
		var err error
		switch kind {
		case scanConnect:
			rep, err = a.ConnectRoot(root, now)
		case scanLive:
			for _, p := range paths {
				r, e := a.RescanRepo(p, now)
				rep.Repos += r.Repos
				rep.Changed += r.Changed
				rep.NewEvents += r.NewEvents
				rep.Errors = append(rep.Errors, r.Errors...)
				if e != nil && err == nil {
					err = e
				}
			}
		default: // startup and manual refresh reconcile everything
			rep, err = a.Reconcile(now)
		}
		return scanDoneMsg{kind: kind, rep: rep, err: err, root: root, paths: paths}
	}
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

func (m *Model) siteByPath(path string) *forest.Site {
	for _, s := range m.world.Sites {
		if s.Town.Path == path {
			return s
		}
	}
	return nil
}

// rebuildWorld relays the forest out of the latest reduced state, keeping
// the camera anchored to whatever the eye was on. IdleOverride previews and
// the neglect preview's lock survive by repo path.
func (m *Model) rebuildWorld() {
	var focusPath, labbedPath string
	var oldFocusX float64
	if m.focus != nil {
		focusPath, oldFocusX = m.focus.Town.Path, float64(m.focus.SignX)
	}
	if m.labbed != nil {
		labbedPath = m.labbed.Town.Path
	}
	overrides := map[string]*time.Duration{}
	for _, s := range m.world.Sites {
		if s.Town.IdleOverride != nil && s.Town.Path != "" {
			overrides[s.Town.Path] = s.Town.IdleOverride
		}
	}

	m.world = forest.Build(m.seed, m.app.Towns())

	for path, ov := range overrides {
		if s := m.siteByPath(path); s != nil {
			s.Town.IdleOverride = ov
		}
	}
	if labbedPath != "" {
		m.labbed = m.siteByPath(labbedPath)
		if m.labbed == nil && m.mode == preview {
			m.mode = roam
		}
	}
	if focusPath != "" {
		if s := m.siteByPath(focusPath); s != nil {
			// Towns west of here may have grown; shift the camera with the
			// site so the view does not jump.
			delta := float64(s.SignX) - oldFocusX
			m.cam += delta
			m.target += delta
		}
	}
	m.clampTarget()
	if m.cam < 0 {
		m.cam = 0
	}
	for path := range m.fps {
		if m.siteByPath(path) == nil {
			delete(m.fps, path)
		}
	}
}

// almanacEvents is the log the almanac folds: the live app log for a real
// forest, the config-supplied events for the demo.
func (m *Model) almanacEvents() []events.Event {
	if !m.demo && m.app != nil {
		return m.app.Events
	}
	return m.evs
}

// toast shows a quiet line at the bottom for a few seconds.
func (m *Model) toast(s string) {
	m.status = s
	m.statusAt = time.Now()
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
		m.stepRevives()
		m.stepCeremony()
		var cmd tea.Cmd
		if c := m.maybePoll(); c != nil {
			cmd = c
		}
		if moving || len(m.revives) > 0 || m.ceremonyAnim != nil {
			return m, tea.Batch(tick(moveFPS), cmd)
		}
		return m, tea.Batch(tick(idleFPS), cmd)

	case scanDoneMsg:
		return m.scanDone(msg)

	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

// maybePoll checks connected repos for new commits with stat calls only, and
// kicks a rescan when something changed. It stays quiet while another scan
// runs or while a preview mode holds the world still.
func (m *Model) maybePoll() tea.Cmd {
	if m.demo || m.app == nil || m.scanning ||
		m.mode == preview || m.mode == connectInput || m.mode == confirmExclude ||
		m.mode == confirmFinish || m.mode == ceremony {
		return nil
	}
	if time.Since(m.lastPoll) < pollEvery {
		return nil
	}
	m.lastPoll = time.Now()
	var changed []string
	for _, s := range m.world.Sites {
		path := s.Town.Path
		if path == "" {
			continue
		}
		fp := gitscan.Fingerprint(path)
		if fp == "" {
			continue // repo gone: it stands, and decays, on its history
		}
		if old, ok := m.fps[path]; ok && old != fp {
			changed = append(changed, path)
		}
		m.fps[path] = fp
	}
	if len(changed) == 0 {
		return nil
	}
	m.scanning = true
	return scanCmd(m.app, scanLive, "", changed)
}

// stepRevives eases reviving towns, and reviving buildings, from their old
// decay back to the truth.
func (m *Model) stepRevives() {
	for path, anim := range m.revives {
		s := m.siteByPath(path)
		if s == nil {
			delete(m.revives, path)
			continue
		}
		p := float64(time.Since(anim.start)) / float64(reviveDur)
		if p >= 1 {
			s.Town.IdleOverride = nil
			delete(m.revives, path)
			continue
		}
		if p < 0 {
			p = 0 // a staggered pulse holds its depth until its moment
		}
		ease := p * p * (3 - 2*p)
		d := anim.to + (anim.from-anim.to)*(1-ease)
		ov := model.IdleForDecay(d)
		s.Town.IdleOverride = &ov
	}
	for key, anim := range m.compRevives {
		path, comp, _ := strings.Cut(key, "\x00")
		s := m.siteByPath(path)
		if s == nil {
			delete(m.compRevives, key)
			continue
		}
		p := float64(time.Since(anim.start)) / float64(reviveDur)
		if p >= 1 {
			delete(s.Town.CompIdleOverride, comp)
			delete(m.compRevives, key)
			continue
		}
		if p < 0 {
			p = 0
		}
		ease := p * p * (3 - 2*p)
		if s.Town.CompIdleOverride == nil {
			s.Town.CompIdleOverride = map[string]time.Duration{}
		}
		s.Town.CompIdleOverride[comp] = model.IdleForDecay(anim.to + (anim.from-anim.to)*(1-ease))
	}
}

func (m Model) scanDone(msg scanDoneMsg) (tea.Model, tea.Cmd) {
	m.scanning = false
	if msg.kind == scanConnect {
		return m.connectDone(msg)
	}
	if msg.kind == scanStartup {
		// The catch-up scan: fold whatever landed while the forest was
		// closed, then play the since-last-visit pulse from the log. The log
		// already carries anything a CLI refresh appended while the app was
		// closed, so the pulse runs even when this scan found nothing new
		// itself - and even when the scan failed, the log on disk still
		// tells the story.
		if msg.err == nil && msg.rep.NewEvents > 0 {
			m.rebuildWorld()
		}
		m.beginPulse()
		return m, nil
	}
	if msg.err != nil {
		if msg.kind == scanRefresh {
			m.toast("refresh failed · " + msg.err.Error())
		}
		return m, nil
	}
	if msg.rep.NewEvents > 0 {
		// Remember how deep each changed town, and each of its buildings,
		// stood before the news.
		oldDecay := map[string]float64{}
		oldComp := map[string]float64{}
		for _, s := range m.world.Sites {
			if s.Town.Path == "" {
				continue
			}
			oldDecay[s.Town.Path] = s.Town.Decay(m.now)
			for _, b := range s.Buildings {
				oldComp[s.Town.Path+"\x00"+b.B.Path] = s.Town.BuildingDecay(b.B, m.now)
			}
		}
		m.rebuildWorld()
		revived := []string{}
		for path, was := range oldDecay {
			s := m.siteByPath(path)
			if s == nil || s.Town.IdleOverride != nil {
				continue
			}
			nowD := s.Town.Decay(m.now)
			if was-nowD > 0.02 {
				m.revives[path] = &reviveAnim{from: was, start: time.Now()}
				revived = append(revived, s.Town.Name)
			}
			// The precise stir: buildings whose components were touched
			// shake off their own decay, even when the town was awake.
			for _, b := range s.Buildings {
				key := s.Town.Path + "\x00" + b.B.Path
				wasB, had := oldComp[key]
				if !had {
					continue
				}
				if wasB-s.Town.BuildingDecay(b.B, m.now) > 0.02 {
					m.compRevives[key] = &reviveAnim{from: wasB, start: time.Now()}
				}
			}
		}
		switch {
		case msg.kind == scanLive && len(revived) == 1:
			m.toast(revived[0] + " stirs")
		case msg.kind == scanLive && len(revived) > 1:
			m.toast(fmt.Sprintf("%d towns stir", len(revived)))
		case msg.kind == scanRefresh:
			m.toast(fmt.Sprintf("refreshed · %d towns grew", msg.rep.Changed))
		}
	} else if msg.kind == scanRefresh {
		m.toast("refreshed · nothing new")
	}
	return m, nil
}

// beginPulse plays the since-last-visit pulse: towns that stirred while the
// forest was closed wake with the same motion a live commit plays - the
// grove eases back to bright, the smoke returns - staggered, capped at
// maxPulses, and fading on their own to the exact ordinary forest. Motion,
// never a number: no counts, no lists, at most one soft line for the most
// notable town that woke.
func (m *Model) beginPulse() {
	if m.demo || m.app == nil {
		return
	}
	stirs := app.SinceLastVisit(m.app.Events, m.lastOpened, m.now)
	started := 0
	toasted := false
	for _, st := range stirs {
		if started >= maxPulses {
			break
		}
		if st.NewCommits == 0 {
			continue
		}
		s := m.siteByPath(st.Repo)
		if s == nil || s.Town.Finished {
			continue // excluded towns and monuments stand as they are
		}
		to := trueDecay(s.Town, m.now)
		from := st.WakeDepth
		if from < pulseFloor {
			from = pulseFloor // a town bright the whole time still visibly stirs
		}
		if from <= to+0.02 {
			continue // nothing lighter to wake into
		}
		m.revives[st.Repo] = &reviveAnim{
			from:  from,
			to:    to,
			start: time.Now().Add(time.Duration(started) * pulseStagger),
		}
		started++
		if !toasted && st.Woke() {
			m.toast(st.Name + " stirred while you were away")
			toasted = true
		}
	}
}

// trueDecay is the depth a town really stands at now, overrides aside: the
// depth a pulse eases into.
func trueDecay(t *model.Town, now time.Time) float64 {
	if t.Finished || t.LastTS.IsZero() {
		return 0
	}
	return model.DecayAt(now.Sub(t.LastTS))
}

func (m Model) connectDone(msg scanDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.inputMsg = "hm · " + msg.err.Error()
		return m, nil
	}
	if msg.rep.Repos == 0 {
		m.inputMsg = "no repositories under " + msg.root
		return m, nil
	}
	wasDemo := m.demo
	if m.app.Connected() {
		m.demo = false
	}
	m.rebuildWorld()
	towns := len(m.world.Sites)
	if m.mode != connectInput {
		// The panel was left before the scan finished; bloom in place.
		if wasDemo && !m.demo {
			if s := m.world.SpotSite(time.Now()); s != nil {
				m.centerOn(s)
				m.cam = m.target
			}
		}
		m.toast(fmt.Sprintf("the forest blooms · %s", plural(towns, "town")))
		return m, nil
	}
	if wasDemo && !m.demo {
		m.freshBloom = true
	}
	m.inputMsg = fmt.Sprintf("%s · %s in the forest",
		plural(msg.rep.Repos, "repository"), plural(towns, "town"))
	if len(msg.rep.Errors) > 0 {
		m.inputMsg += fmt.Sprintf(" · %d unreadable", len(msg.rep.Errors))
	}
	m.input = ""
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if k != "?" {
		m.hint = false
	}

	// Modes that capture typing come first.
	if m.mode == connectInput {
		return m.connectKey(msg)
	}
	if m.mode == confirmFinish {
		return m.finishKey(msg)
	}
	if m.mode == ceremony {
		// The passage holds the floor; it is over in a couple of seconds.
		return m, nil
	}
	if m.mode == confirmExclude {
		return m.confirmKey(k)
	}
	if m.mode == preview && m.labbed != nil {
		if handled, mm := m.previewKey(k); handled {
			return mm, nil
		}
	}

	switch k {
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
			if m.focus.Town.Finished {
				m.unfinish(m.focus)
			} else {
				// The threshold: finishing is a ceremony, never a toggle.
				m.mode = confirmFinish
				m.labbed = nil
				m.epitaph = ""
			}
		}
	case "a":
		// The almanac: one deliberate keypress past inspect, never straight
		// off the map.
		if m.mode == inspect && m.focus != nil {
			m.mode = almanacView
		} else if m.mode == almanacView {
			m.mode = inspect
		}
	case "d":
		if m.mode == preview {
			m.mode = roam
			m.labbed = nil
		} else if m.focus != nil {
			m.mode = preview
			m.labbed = m.focus
			m.centerOn(m.focus)
		}
	case "c":
		m.mode = connectInput
		m.onboarding = false
		m.input = ""
		m.inputMsg = ""
	case "x":
		if !m.demo && m.focus != nil && m.focus.Town.Path != "" {
			m.mode = confirmExclude
		}
	case "r":
		if !m.demo && m.app != nil && !m.scanning && len(m.app.Settings.Roots) > 0 {
			m.scanning = true
			m.toast("walking the roots …")
			return m, scanCmd(m.app, scanRefresh, "", nil)
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

// connectKey is the connect panel's line editor: plain typing, backspace,
// enter to scan, esc to leave.
func (m Model) connectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = roam
		if m.freshBloom {
			// The demo just gave way to the real forest: land under the lantern.
			m.freshBloom = false
			if s := m.world.SpotSite(time.Now()); s != nil {
				m.centerOn(s)
				m.cam = m.target
			}
		}
		return m, nil
	case "enter":
		if m.scanning {
			return m, nil
		}
		path := strings.TrimSpace(m.input)
		if path == "" {
			m.inputMsg = "type a folder where repositories live"
			return m, nil
		}
		if strings.HasPrefix(path, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				path = home + strings.TrimPrefix(path, "~")
			}
		}
		m.scanning = true
		m.inputMsg = ""
		return m, scanCmd(m.app, scanConnect, path, nil)
	case "backspace":
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
		return m, nil
	case "ctrl+u":
		m.input = ""
		return m, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		m.input += string(msg.Runes)
	case tea.KeySpace:
		m.input += " "
	}
	return m, nil
}

func (m Model) confirmKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "y", "Y":
		if m.focus != nil && m.focus.Town.Path != "" {
			name, path := m.focus.Town.Name, m.focus.Town.Path
			m.app.Settings.SetExcluded(path, true)
			if err := m.app.SaveSettings(); err != nil {
				m.toast("could not save · " + err.Error())
			} else {
				m.rebuildWorld()
				m.focus = m.world.NearestSite(m.cam + m.dotW()/2)
				m.toast(name + " excluded · `agentforest include " + name + "` restores it")
			}
		}
		m.mode = roam
	case "n", "esc", "q":
		m.mode = roam
	}
	return m, nil
}

// finishKey is the threshold panel's line editor: one carved line, enter to
// lay the town to rest (an empty line leaves the monument unmarked), esc to
// keep tending. A threshold, not a wizard - enter always crosses it.
func (m Model) finishKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = roam
		return m, nil
	case "enter":
		if m.scanning {
			return m, nil // the panel says the woods are being walked
		}
		return m.beginCeremony()
	case "backspace":
		if r := []rune(m.epitaph); len(r) > 0 {
			m.epitaph = string(r[:len(r)-1])
		}
		return m, nil
	case "ctrl+u":
		m.epitaph = ""
		return m, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		m.epitaph = capRunes(m.epitaph+string(msg.Runes), app.EpitaphMaxRunes)
	case tea.KeySpace:
		m.epitaph = capRunes(m.epitaph+" ", app.EpitaphMaxRunes)
	}
	return m, nil
}

// capRunes truncates s to at most n runes: an epitaph is carved, not written.
func capRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// beginCeremony carves the epitaph into the log and starts the laying-to-rest
// passage. The words are kept the moment the threshold is crossed, so even an
// interrupted ceremony loses nothing; the animation then plays out and lands
// exactly on the monument the world would build.
func (m Model) beginCeremony() (tea.Model, tea.Cmd) {
	s := m.focus
	if s == nil {
		m.mode = roam
		return m, nil
	}
	t := s.Town
	epitaph := strings.TrimSpace(m.epitaph)
	now := time.Now()
	if !m.demo && m.app != nil && t.Path != "" {
		if err := m.app.Finish(t.Path, epitaph, now); err != nil {
			m.toast("could not save · " + err.Error())
			return m, nil
		}
	} else if m.demo {
		m.evs = append(m.evs, events.Event{Kind: events.KindFinish, Repo: t.Name, TS: now, Path: t.Path, Epitaph: epitaph})
	}
	m.epitaph = ""
	if epitaph != "" {
		t.RepoState.Epitaph = epitaph
	}
	t.RepoState.FinishTS = now

	// The ceremony eases the town from its real depth, not a preview's.
	t.IdleOverride = nil
	delete(m.revives, t.Path)
	anim := &finishAnim{
		path: t.Path, name: t.Name,
		from:     t.Decay(m.now),
		fromComp: map[string]float64{},
		start:    time.Now(),
	}
	for _, b := range s.Buildings {
		anim.fromComp[b.B.Path] = t.BuildingDecay(b.B, m.now)
		delete(m.compRevives, t.Path+"\x00"+b.B.Path)
	}
	m.ceremonyAnim = anim
	m.mode = ceremony
	m.centerOn(s)
	return m, nil
}

// stepCeremony advances the laying-to-rest: the decay stills, the carve
// deepens ridge-down, and the grove eases into its monument symmetry. The
// final frame is exactly the monument the world builds for a finished town,
// so completing needs no rebuild.
func (m *Model) stepCeremony() {
	a := m.ceremonyAnim
	if a == nil {
		return
	}
	s := m.ceremonySite(a)
	if s == nil || s.Town.Finished {
		// The world was rebuilt mid-passage; the derived state already
		// stands finished, so the moment simply completes.
		m.ceremonyAnim = nil
		if m.mode == ceremony {
			m.mode = roam
		}
		return
	}
	t := s.Town
	p := float64(time.Since(a.start)) / float64(finishDur)
	if p >= 1 {
		t.Finished = true
		t.CarveOverride = nil
		t.IdleOverride = nil
		for path := range a.fromComp {
			delete(t.CompIdleOverride, path)
		}
		s.CarveGrove(1)
		m.ceremonyAnim = nil
		m.mode = roam
		m.toast(t.Name + " stands as a monument")
		return
	}
	ease := p * p * (3 - 2*p)
	carve := ease
	t.CarveOverride = &carve
	ov := model.IdleForDecay(a.from * (1 - ease))
	t.IdleOverride = &ov
	if len(a.fromComp) > 0 && t.CompIdleOverride == nil {
		t.CompIdleOverride = map[string]time.Duration{}
	}
	for path, from := range a.fromComp {
		t.CompIdleOverride[path] = model.IdleForDecay(from * (1 - ease))
	}
	s.CarveGrove(ease)
}

// ceremonySite resolves the town under ceremony against the current world:
// by repo path, or by name for demo towns that have none.
func (m *Model) ceremonySite(a *finishAnim) *forest.Site {
	if a.path != "" {
		return m.siteByPath(a.path)
	}
	for _, s := range m.world.Sites {
		if s.Town.Name == a.name {
			return s
		}
	}
	return nil
}

// unfinish lights the hearth again: the quiet reverse of the ceremony. No
// passage, no panel - the town simply returns to life. Every word ever
// carved stays in the log; only the standing changes.
func (m *Model) unfinish(s *forest.Site) {
	if m.scanning {
		m.toast("the woods are being walked · a moment")
		return
	}
	t := s.Town
	now := time.Now()
	if !m.demo && m.app != nil && t.Path != "" {
		if err := m.app.Unfinish(t.Path, now); err != nil {
			m.toast("could not save · " + err.Error())
			return
		}
	} else if m.demo {
		m.evs = append(m.evs, events.Event{Kind: events.KindUnfinish, Repo: t.Name, TS: now, Path: t.Path})
	}
	t.Finished = false
	t.CarveOverride = nil
	s.CarveGrove(0)
	m.toast(t.Name + " · the hearth is lit again")
}

// previewKey adjusts the locked town's preview idle time.
func (m Model) previewKey(k string) (bool, Model) {
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
	case almanacView:
		m.drawAlmanac()
	case preview:
		m.drawPreview()
	case helpView:
		m.drawHelp()
	case connectInput:
		m.drawConnect()
	case confirmExclude:
		m.drawConfirm()
	case confirmFinish:
		m.drawFinishConfirm()
	}
	// The hint and a toast share the bottom line; a live toast takes it whole
	// so the two never interleave (the launch pulse can toast before any key
	// has dismissed the hint).
	showStatus := m.status != "" && time.Since(m.statusAt) < 4*time.Second && m.mode == roam
	if m.hint && m.mode == roam && !showStatus {
		hint := "← → wander · tab towns · enter inspect · ? help"
		m.canv.Text((m.w-len([]rune(hint)))/2, m.h-1, hint, 88, 0)
	}
	if showStatus {
		m.canv.Text((m.w-len([]rune(m.status)))/2, m.h-1, m.status, 100, 0)
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

// panel clears a rectangle and draws framed lines onto the canvas. A line
// longer than the window is trimmed with an ellipsis so the frame never
// spills past the edges.
func (m Model) panel(lines []line) {
	w := 0
	for _, l := range lines {
		if n := len([]rune(l.text)); n > w {
			w = n
		}
	}
	w += 4
	if w > m.w-2 {
		w = m.w - 2
	}
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
		txt := l.text
		if max := w - 4; len([]rune(txt)) > max {
			txt = string([]rune(txt)[:max-1]) + "…"
		}
		m.canv.Text(x0+2, y0+1+i, txt, l.lvl, l.acc)
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
		{fmt.Sprintf("planted %s · %s", almanac.MonthYear(t.FirstTS), age(m.now.Sub(t.FirstTS))), 150, 0},
		{mix, 150, 0},
		{fmt.Sprintf("%s commits · %s", commas(t.TotalCommits), plural(len(t.Tags), "release")), 150, 0},
		{"last tended " + ago(t.Idle(m.now)), 150, 0},
		{model.StageLine(model.StageOf(d), t.Finished), 175, 60},
	}
	// The carved epitaph: the user's own words, read only here. The map
	// stays silent; the monument just stands.
	if t.Finished && t.Epitaph != "" {
		lines = append(lines, line{"\"" + t.Epitaph + "\"", 200, 90})
	}
	if t.Path != "" {
		// A deep path identifies by its tail; trim from the front.
		p := collapseHome(t.Path)
		if max := m.w - 8; max > 10 && len([]rune(p)) > max {
			r := []rune(p)
			p = "…" + string(r[len(r)-max:])
		}
		lines = append(lines, line{p, 110, 0})
	}
	// The settlement: what stands here, and how each building fares.
	if bs := t.Buildings(); len(bs) > 0 {
		lines = append(lines, line{"", 0, 0})
		show := bs
		if len(show) > 6 {
			show = show[:6]
		}
		for _, b := range show {
			stage := model.StageOf(t.BuildingDecay(b, m.now)).String()
			if t.Finished {
				stage = "kept"
			}
			name := b.Name
			if len(name) > 15 {
				name = name[:14] + "…"
			}
			lines = append(lines, line{fmt.Sprintf("%-12s %-16s %s", b.Form, name, stage), 135, 0})
		}
		if len(bs) > 6 {
			lines = append(lines, line{fmt.Sprintf("and %d more", len(bs)-6), 110, 0})
		}
	}
	lines = append(lines, line{"", 0, 0}, line{"a · the almanac", 115, 0})
	m.panel(lines)
}

// drawAlmanac is the town's memoir: its life folded from the same event log
// the forest grows from, told in a handful of lines. For a finished town the
// carved words lead. It lives one deliberate keypress past inspect and never
// touches the map.
func (m Model) drawAlmanac() {
	if m.focus == nil {
		return
	}
	t := m.focus.Town
	key := t.Path
	if m.demo || key == "" {
		// The demo log keys towns by name; real logs key by canonical path.
		key = t.Name
	}
	mem := almanac.Fold(m.almanacEvents(), key, m.now)
	lines := []line{{"almanac · " + t.Name, 230, 235}, {"", 0, 0}}
	if mem == nil {
		lines = append(lines, line{"the log holds no story yet", 150, 0})
	} else {
		if mem.Epitaph != "" {
			lines = append(lines, line{"\"" + mem.Epitaph + "\"", 200, 90})
		}
		if mem.Brief != "" {
			lines = append(lines, line{mem.Brief, 150, 0})
		}
		for _, c := range mem.Chapters {
			lines = append(lines, line{c, 150, 0})
		}
	}
	lines = append(lines, line{"", 0, 0}, line{"a back to inspect · esc the forest", 115, 0})
	m.panel(lines)
}

func (m Model) drawPreview() {
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
		{"the years of neglect · " + t.Name, 230, 235},
		{idleLine, 150, 0},
		{model.StageLine(model.StageOf(d), t.Finished), 175, 60},
		{"", 0, 0},
		{"+/- day   </> month   [/] year   1-6 stages", 115, 0},
		{"0 back to real time   esc done", 115, 0},
	}
	m.panel(lines)
}

func (m Model) drawConnect() {
	shown := m.input
	if max := m.w - 24; max > 10 && len([]rune(shown)) > max {
		r := []rune(shown)
		shown = "…" + string(r[len(r)-max:])
	}
	prompt := "> " + shown + "▌"
	var lines []line
	if m.onboarding {
		lines = []line{
			{"welcome to agentforest", 230, 235},
			{"", 0, 0},
			{"behind this panel is a demo forest; yours grows from your repositories", 150, 0},
			{"type a folder where they live (roots are scanned recursively)", 150, 0},
			{"", 0, 0},
			{prompt, 210, 0},
		}
	} else {
		lines = []line{
			{"connect a root", 230, 235},
			{"", 0, 0},
			{prompt, 210, 0},
		}
	}
	msg := m.inputMsg
	if m.scanning {
		msg = "walking the woods" + strings.Repeat(".", 1+int(time.Since(m.start).Seconds()*2)%3)
	}
	if msg != "" {
		lines = append(lines, line{msg, 150, 60})
	}
	lines = append(lines, line{"", 0, 0},
		line{"enter scan · esc " + map[bool]string{true: "wander for now", false: "back"}[m.onboarding], 115, 0})
	m.panel(lines)
}

func (m Model) drawConfirm() {
	if m.focus == nil {
		return
	}
	m.panel([]line{
		{"exclude " + m.focus.Town.Name + "?", 230, 235},
		{"its history is kept; `agentforest include " + m.focus.Town.Name + "` replants it", 150, 0},
		{"", 0, 0},
		{"y exclude · esc keep it", 115, 0},
	})
}

// drawFinishConfirm is the threshold: the one place the product asks for
// words. One short line, carved rather than written; enter with nothing
// leaves the monument unmarked, and the ceremony is the same either way.
func (m Model) drawFinishConfirm() {
	if m.focus == nil {
		return
	}
	lines := []line{
		{"lay " + m.focus.Town.Name + " to rest as a monument?", 230, 235},
		{"its hearth goes cold and its grove stands still · f lights it again", 150, 0},
		{"", 0, 0},
		{"a word to carve? (enter to leave it unmarked)", 150, 0},
		{"> " + m.epitaph + "▌", 210, 0},
	}
	if m.scanning {
		lines = append(lines, line{"the woods are being walked …", 150, 60})
	}
	lines = append(lines, line{"", 0, 0},
		line{"enter lay it to rest · esc keep tending", 115, 0})
	m.panel(lines)
}

func (m Model) drawHelp() {
	lines := []line{
		{"agentforest", 230, 235},
		{"", 0, 0},
		{"wander     ← → or h l · shift strides", 150, 0},
		{"towns      tab / shift+tab · g oldest · G newest", 150, 0},
		{"inspect    enter or i · numbers live here only", 150, 0},
		{"almanac    a while inspecting · the town's memoir", 150, 0},
		{"finished   f · lay a town to rest as a monument", 150, 0},
		{"foresee    d · preview the years of neglect", 150, 0},
		{"connect    c · add a root full of repositories", 150, 0},
		{"exclude    x · hide the focused town", 150, 0},
		{"refresh    r · rescan every root now", 150, 0},
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

func collapseHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// ---- humanizing -----------------------------------------------------------

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	// Consonant + y pluralizes to ies (repository); vowel + y just takes s (day).
	if r := []rune(word); len(r) > 1 && r[len(r)-1] == 'y' && !strings.ContainsRune("aeiou", r[len(r)-2]) {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
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
