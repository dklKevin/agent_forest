package sprite

import "github.com/dklKevin/agentforest/internal/agentrun"

// WorkMark is a small, shape-only sign of locally evidenced work. The phase
// glyph stands above the campsite; the low post is the replay plaque.
type WorkMark struct {
	X       int
	GroundY int
	Phase   agentrun.Phase
	Active  bool
	Plaque  bool
	Lvl     uint8
}

// DrawWorkMark gives every phase a different silhouette. Text belongs behind
// inspect; the map stays quiet and readable without color.
func (p *P) DrawWorkMark(m WorkMark) {
	cx, gy := m.X/2, m.GroundY/4
	if m.Active {
		x, y := cx-1, gy-4
		p.C.ClearRect(x, y, 3, 2)
		switch m.Phase {
		case agentrun.Planning:
			p.C.Text(x, y, "╭─╮", m.Lvl, 0)
			p.C.Text(x, y+1, "╰─╯", m.Lvl, 0)
		case agentrun.Building:
			p.C.Text(x, y, "╱┼╲", m.Lvl, 0)
			p.C.Text(x, y+1, "╲┼╱", m.Lvl, 0)
		case agentrun.Testing:
			p.C.Text(x, y, "┌◇┐", m.Lvl, 0)
			p.C.Text(x, y+1, "└─┘", m.Lvl, 0)
		case agentrun.Reviewing:
			p.C.Text(x, y, " ◯ ", m.Lvl, 0)
			p.C.Text(x, y+1, "  ╲", m.Lvl, 0)
		case agentrun.Blocked:
			p.C.Text(x, y, " ╳ ", m.Lvl, 0)
			p.C.Text(x, y+1, "━┻━", m.Lvl, 0)
		case agentrun.HandedOff:
			p.C.Text(x, y, " ─╮", m.Lvl, 0)
			p.C.Text(x, y+1, " ─╯", m.Lvl, 0)
		case agentrun.Completed:
			p.C.Text(x, y, " ◆ ", m.Lvl, 0)
			p.C.Text(x, y+1, "━┻━", m.Lvl, 0)
		}
	}
	if m.Plaque {
		// A little board at the path edge. It carries no map text; opening it
		// is a deliberate inspect action.
		x := cx + 3
		p.C.ClearRect(x, gy-1, 3, 2)
		p.C.Text(x, gy-1, "┌─┐", m.Lvl-12, 0)
		p.C.Text(x, gy, " ┬ ", m.Lvl-24, 0)
		p.C.Dot((x+1)*2, m.GroundY+1, m.Lvl-34)
	}
}
