// Package guiringer lets the intercom's display follow the ringer mute
// through a hook in its QML.
//
// The display's GUI follows a mute it sees on the bus but ignores an unmute
// (C100X firmware 1.5.8: the vendor library reports dimension 33 only when it
// says muted, and the GUI only ever sets its mute state). A QML item added to
// the GUI asks the companion for the ringer state, long polling
// GET /api/v3/gui/ringer, and sets the GUI's own state with
// global.audioState.enableState or disableState(AudioState.RingtoneMute).
// Each request says what the display shows, so the companion knows when the
// display has followed an unmute and the firmware reload is not needed. See
// docs/gui-ringer-hook.md.
package guiringer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"bticino-go-companion/internal/core"
)

const (
	// DefaultWait bounds one request, under the GUI's and any proxy's
	// timeouts; the display asks again at once.
	DefaultWait = 25 * time.Second
	// defaultPoll is how often a waiting request looks at the ringer state.
	defaultPoll = 250 * time.Millisecond
	// defaultRetry is how soon the same correction is sent again to a display
	// that did not apply it: sooner, a display that cannot follow would ask in
	// a tight loop.
	defaultRetry = 5 * time.Second
)

// Hook answers the display's requests and remembers what it last showed.
type Hook struct {
	muted func() bool
	now   func() time.Time
	// known says the ringer state has been read from the bus. Until then
	// muted's answer is a default, and telling the display to show it could
	// unmute a muted ringer.
	known atomic.Bool

	wait, poll, retry time.Duration

	mu sync.Mutex
	// shown is what the display last said it shows, at shownAt; seen says it
	// said anything at all.
	shown   bool
	shownAt time.Time
	seen    bool
	// corrected is the last answer that differed from what the display
	// showed, so the same one is not repeated within retry.
	corrected   bool
	correctedTo bool
	correctedAt time.Time
}

// New returns a hook for the ringer state muted reports.
func New(muted func() bool) *Hook {
	return &Hook{muted: muted, now: time.Now, wait: DefaultWait, poll: defaultPoll, retry: defaultRetry}
}

// Observe takes every event applied to the state: a ringer event from the bus
// makes the state known.
func (h *Hook) Observe(event core.Event) {
	switch event.(type) {
	case core.AudioMuted, core.AudioUnmuted:
		h.known.Store(true)
	}
}

// current is the state the display should show: what it shows while the
// ringer state is not known.
func (h *Hook) current(shown bool) bool {
	if !h.known.Load() {
		return shown
	}

	return h.muted()
}

// Next is one request of the display, which shows the ringer muted or not. It
// returns the state the display should show: at once if that differs, else
// when the ringer changes or the wait ends.
func (h *Hook) Next(ctx context.Context, shown bool) bool {
	h.report(shown)

	start := h.current(shown)
	if start != shown && h.mayCorrect(start) {
		return start
	}

	timer := time.NewTimer(h.wait)
	defer timer.Stop()

	ticker := time.NewTicker(h.poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return h.current(shown)
		case <-timer.C:
			return h.current(shown)
		case <-ticker.C:
		}

		if muted := h.current(shown); muted != start {
			return muted
		}
	}
}

func (h *Hook) report(shown bool) {
	h.mu.Lock()
	h.shown, h.shownAt, h.seen = shown, h.now(), true
	h.mu.Unlock()
}

// mayCorrect reports whether the display may be told to show muted now: the
// same correction is sent once per retry period.
func (h *Hook) mayCorrect(muted bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	if h.corrected && h.correctedTo == muted && now.Sub(h.correctedAt) < h.retry {
		return false
	}

	h.corrected, h.correctedTo, h.correctedAt = true, muted, now

	return true
}

// Shows reports whether the display has said, at since or later, that it
// shows the ringer muted or not.
func (h *Hook) Shows(muted bool, since time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.seen && h.shown == muted && !h.shownAt.Before(since)
}
