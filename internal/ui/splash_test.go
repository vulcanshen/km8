package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Setup ──────────────────────────────────────────────────────────────────

func newTestSplash() SplashModel {
	return NewSplashModel()
}

// ── Initial state ──────────────────────────────────────────────────────────

func TestSplashModel_InitialState(t *testing.T) {
	m := newTestSplash()

	if m.IsActive() {
		t.Error("new splash model should be inactive")
	}
	if out := m.Render(80, 40); out != "" {
		t.Errorf("expected empty render when inactive, got non-empty string")
	}
}

// ── Show ───────────────────────────────────────────────────────────────────

func TestSplashModel_ShowReturnsCmd(t *testing.T) {
	m := newTestSplash()
	cmd := m.Show()
	if cmd == nil {
		// This is the bug fixed in c6ffcbe: Show() was changed to return
		// a tea.Cmd for the first tick, but the call site in app.go discarded
		// the return value, so the animation never started.
		t.Fatal("Show() must return a non-nil cmd to start the animation tick")
	}
}

func TestSplashModel_ShowActivates(t *testing.T) {
	m := newTestSplash()
	m.Show()

	if !m.IsActive() {
		t.Error("Show() must set active = true")
	}
}

func TestSplashModel_ShowPopulatesPixelOrder(t *testing.T) {
	m := newTestSplash()
	m.Show()

	if len(m.pixelOrder) == 0 {
		t.Error("Show() must populate pixelOrder with colored pixel indices")
	}
}

func TestSplashModel_ShowResetsRevealCount(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m, _ = m.Update(splashTickMsg{}) // advance a tick

	m.Show() // call again (re-trigger)

	if m.revealedCount != 0 {
		t.Errorf("Show() must reset revealedCount to 0, got %d", m.revealedCount)
	}
}

func TestSplashModel_ShowRendersNonEmpty(t *testing.T) {
	m := newTestSplash()
	m.Show()

	if out := m.Render(80, 40); out == "" {
		t.Error("active splash must render a non-empty string")
	}
}

// ── Animation ticks ────────────────────────────────────────────────────────

func TestSplashModel_TickRevealsPixels(t *testing.T) {
	m := newTestSplash()
	m.Show()
	before := m.revealedCount

	m, _ = m.Update(splashTickMsg{})

	if m.revealedCount <= before {
		t.Errorf("splashTickMsg must increase revealedCount; before=%d after=%d", before, m.revealedCount)
	}
}

func TestSplashModel_TickReturnsNextCmd(t *testing.T) {
	m := newTestSplash()
	m.Show()

	// Logo has many more than 15 colored pixels, so animation continues.
	_, cmd := m.Update(splashTickMsg{})
	if cmd == nil {
		t.Error("tick must return next cmd while animation is in progress")
	}
}

func TestSplashModel_TickDoesNotExceedTotal(t *testing.T) {
	m := newTestSplash()
	m.Show()

	// Run ticks until animation completes.
	for i := 0; i < 1000; i++ {
		if m.revealedCount >= len(m.pixelOrder) {
			break
		}
		m, _ = m.Update(splashTickMsg{})
	}

	if m.revealedCount != len(m.pixelOrder) {
		t.Errorf("revealedCount must not exceed pixelOrder length; got %d, want %d",
			m.revealedCount, len(m.pixelOrder))
	}
}

func TestSplashModel_TickStopsWhenComplete(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m.revealedCount = len(m.pixelOrder) // fast-forward to end
	m.identityVisible = true            // past identity hold
	m.taglineVisible = true             // past tagline hold
	m.versionVisible = true             // past version hold
	m.creditVisible = true              // past credit hold
	m.hintVisible = true                // past hint hold

	_, cmd := m.Update(splashTickMsg{})
	if cmd != nil {
		t.Error("no tick cmd should be returned when animation is already complete")
	}
}

func TestSplashModel_CaptionRevealsAfterAnimation(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m.revealedCount = len(m.pixelOrder) // fast-forward past K/8 reveal

	// Post-completion tick schedules the identity reveal.
	_, cmd := m.Update(splashTickMsg{})
	if cmd == nil {
		t.Fatal("post-completion tick must return the identity-reveal cmd")
	}
	if m.identityVisible {
		t.Error("identity must not be visible until splashIdentityMsg fires")
	}

	// splashIdentityMsg fires KubeUI + version + tagline + credit together
	// AND schedules the hint reveal.
	m, cmd = m.Update(splashIdentityMsg{})
	if !m.identityVisible {
		t.Error("splashIdentityMsg must set identityVisible = true")
	}
	if !m.versionVisible {
		t.Error("splashIdentityMsg must set versionVisible = true (fires together)")
	}
	if !m.taglineVisible {
		t.Error("splashIdentityMsg must set taglineVisible = true (fires together)")
	}
	if !m.creditVisible {
		t.Error("splashIdentityMsg must set creditVisible = true (fires together)")
	}
	if cmd == nil {
		t.Fatal("splashIdentityMsg must return the hint-reveal cmd")
	}
	if m.hintVisible {
		t.Error("hint must not be visible until splashHintMsg fires")
	}

	// splashHintMsg makes the hint visible.
	m, _ = m.Update(splashHintMsg{})
	if !m.hintVisible {
		t.Error("splashHintMsg must set hintVisible = true")
	}
}

// The developer credit ("developed by" + email) is hidden until the identity
// beat fires, then rendered under the tagline — mirroring filu's splash.
func TestSplashModel_CreditRendersAfterIdentity(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m.revealedCount = len(m.pixelOrder)

	if strings.Contains(m.Render(120, 60), authorEmail) {
		t.Error("author email must not render before the identity beat")
	}

	m, _ = m.Update(splashIdentityMsg{})
	out := m.Render(120, 60)
	if !strings.Contains(out, "developed by") {
		t.Error(`rendered splash must contain "developed by" once credited`)
	}
	if !strings.Contains(out, authorEmail) {
		t.Errorf("rendered splash must contain %q once credited", authorEmail)
	}
}

func TestSplashModel_BoundaryBeatOnce(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m.revealedCount = m.bgCount // fast-forward to the D->U boundary

	// First tick at boundary: hold, don't advance.
	before := m.revealedCount
	m, cmd := m.Update(splashTickMsg{})
	if m.revealedCount != before {
		t.Errorf("boundary tick must not advance; before=%d after=%d", before, m.revealedCount)
	}
	if !m.pausedBg {
		t.Error("boundary tick must set pausedBg = true")
	}
	if cmd == nil {
		t.Error("boundary tick must return the resume cmd")
	}

	// Second tick at boundary: advance into stage 2, no further hold.
	m, _ = m.Update(splashTickMsg{})
	if m.revealedCount <= before {
		t.Errorf("second boundary tick must advance into stage 2; before=%d after=%d", before, m.revealedCount)
	}
	if m.versionVisible {
		t.Error("version must not be visible while pixels are still revealing")
	}
}

func TestSplashModel_SecondBoundaryBeat(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m.pausedBg = true        // first beat (D->U) already consumed
	m.revealedCount = m.uEnd // fast-forward to the U->K/B boundary

	// First tick at the boundary: hold, don't advance.
	before := m.revealedCount
	m, cmd := m.Update(splashTickMsg{})
	if m.revealedCount != before {
		t.Errorf("U->K/B boundary tick must not advance; before=%d after=%d", before, m.revealedCount)
	}
	if !m.pausedU {
		t.Error("boundary tick must set pausedU = true")
	}
	if cmd == nil {
		t.Error("boundary tick must return the resume cmd")
	}

	// Second tick: advance into stage 3 (letters).
	m, _ = m.Update(splashTickMsg{})
	if m.revealedCount <= before {
		t.Errorf("second tick must advance into stage 3; before=%d after=%d", before, m.revealedCount)
	}
}

func TestSplashModel_InactiveIgnoresTick(t *testing.T) {
	m := newTestSplash()
	// Do NOT call Show().

	m, cmd := m.Update(splashTickMsg{})
	if m.IsActive() || cmd != nil {
		t.Error("inactive splash must ignore tick messages")
	}
}

// ── Close ──────────────────────────────────────────────────────────────────

func TestSplashModel_CloseOnEsc(t *testing.T) {
	m := newTestSplash()
	m.Show()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.IsActive() {
		t.Error("Esc must close the splash overlay")
	}
}

func TestSplashModel_CloseResetsState(t *testing.T) {
	m := newTestSplash()
	m.Show()
	m, _ = m.Update(splashTickMsg{}) // reveal some pixels

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	if m.revealedCount != 0 {
		t.Errorf("closing splash must reset revealedCount to 0, got %d", m.revealedCount)
	}
	if m.pixelOrder != nil {
		t.Error("closing splash must set pixelOrder to nil")
	}
}

func TestSplashModel_InactiveIgnoresKeys(t *testing.T) {
	m := newTestSplash()
	// Do NOT call Show().

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.IsActive() || cmd != nil {
		t.Error("inactive splash must ignore key messages")
	}
}
