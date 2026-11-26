package guiringer

import (
	"context"
	"sync"
	"testing"
	"time"

	"bticino-go-companion/internal/core"
)

// ringer is the companion's ringer state, changed by the test.
type ringer struct {
	mu    sync.Mutex
	muted bool
}

func (r *ringer) get() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.muted
}

func (r *ringer) set(muted bool) {
	r.mu.Lock()
	r.muted = muted
	r.mu.Unlock()
}

func newTestHook(r *ringer) *Hook {
	h := New(r.get)
	h.wait = 200 * time.Millisecond
	h.poll = time.Millisecond
	h.Observe(core.AudioUnmuted{})

	return h
}

// A display that shows another state is told at once.
func TestHookCorrectsTheDisplayAtOnce(t *testing.T) {
	r := &ringer{muted: false}
	h := newTestHook(r)

	start := time.Now()
	if got := h.Next(context.Background(), true); got {
		t.Fatal("Next = muted, want unmuted")
	}

	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("answered after %v, want at once", time.Since(start))
	}
}

// A display that agrees waits until the ringer changes: an unmute while the
// display shows muted.
func TestHookAnswersWhenTheRingerChanges(t *testing.T) {
	r := &ringer{muted: true}
	h := newTestHook(r)

	go func() {
		time.Sleep(20 * time.Millisecond)
		r.set(false)
	}()

	if got := h.Next(context.Background(), true); got {
		t.Fatal("Next = muted, want the unmute")
	}
}

func TestHookAnswersTheSameStateWhenTheWaitEnds(t *testing.T) {
	r := &ringer{muted: true}
	h := newTestHook(r)

	start := time.Now()
	if got := h.Next(context.Background(), true); !got {
		t.Fatal("Next = unmuted, want muted")
	}

	if elapsed := time.Since(start); elapsed < h.wait {
		t.Fatalf("answered after %v, want the whole wait", elapsed)
	}
}

// A display that does not apply a correction is not told again at once, or it
// would ask in a tight loop.
func TestHookDoesNotRepeatACorrectionAtOnce(t *testing.T) {
	r := &ringer{muted: false}
	h := newTestHook(r)

	_ = h.Next(context.Background(), true)

	start := time.Now()
	_ = h.Next(context.Background(), true)

	if elapsed := time.Since(start); elapsed < h.wait {
		t.Fatalf("second answer after %v, want it to wait", elapsed)
	}
}

func TestHookRemembersWhatTheDisplayShows(t *testing.T) {
	r := &ringer{muted: false}
	h := newTestHook(r)

	before := time.Now()

	if h.Shows(false, before) {
		t.Fatal("Shows before any request")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = h.Next(ctx, false)

	if !h.Shows(false, before) {
		t.Fatal("Shows(unmuted) = false after the display said so")
	}

	if h.Shows(true, before) {
		t.Fatal("Shows(muted) = true, the display said unmuted")
	}

	if h.Shows(false, time.Now().Add(time.Second)) {
		t.Fatal("Shows counts a report from before since")
	}
}

// Before the companion has read the ringer from the bus its state is a
// default: the display is left as it is, then told once the state is known.
func TestHookLeavesTheDisplayAloneUntilTheStateIsKnown(t *testing.T) {
	r := &ringer{muted: false}
	h := New(r.get)
	h.wait = 200 * time.Millisecond
	h.poll = time.Millisecond

	start := time.Now()
	if got := h.Next(context.Background(), true); !got {
		t.Fatal("Next = unmuted before the state was known, want the display's muted")
	}

	if elapsed := time.Since(start); elapsed < h.wait {
		t.Fatalf("answered after %v, want the whole wait", elapsed)
	}

	h.Observe(core.VoicemailEnabled{})

	go func() {
		time.Sleep(20 * time.Millisecond)
		h.Observe(core.AudioUnmuted{})
	}()

	if got := h.Next(context.Background(), true); got {
		t.Fatal("Next = muted once the state was known, want unmuted")
	}
}
