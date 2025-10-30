package window

import "time"

// Moves forward and sets the lower limit of the acceptable age
type Watermark struct {
	grace   time.Duration
	current time.Time
	initted bool
}

func newWatermark(grace time.Duration) *Watermark {
	return &Watermark{grace: grace}
}

func (w *Watermark) Advance(now time.Time) {
	now = now.UTC()
	if !w.initted {
		w.current = now.Add(-w.grace)
		w.initted = true
		return
	}

	if now.Add(-w.grace).After(w.current) {
		w.current = now.Add(-w.grace)
	}
}

func (w *Watermark) IsLate(t time.Time) bool {
	if !w.initted {
		return false // before first tick -> apply between grace and current now
	}

	return t.UTC().Before(w.current)
}
