package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestParseDecodesTheAssumedTime(t *testing.T) {
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"parsed": {"title": "Wake up", "doAt": "2026-10-02T08:00:00.000Z", "hasTime": true,
			"recurrenceRule": null, "matchedText": "at 8",
			"assumedTime": {"text": "at 8", "assumed": "am", "alternativeText": "at 8pm", "alternativeDoAt": "2026-10-02T20:00:00.000Z"},
			"keptAsName": null}}`))
	}))
	p, err := c.Parse(context.Background(), "Wake up at 8")
	if err != nil {
		t.Fatal(err)
	}
	a := p.AssumedTime
	if a == nil || a.Text != "at 8" || a.Assumed != "am" || a.AlternativeText != "at 8pm" ||
		!a.AlternativeDoAt.Equal(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC)) {
		t.Errorf("assumed time = %+v, want the service's four fields", a)
	}
}

func TestParseWithNothingAssumed(t *testing.T) {
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"parsed": {"title": "Wake up", "doAt": "2026-10-02T08:00:00.000Z", "hasTime": true, "assumedTime": null}}`))
	}))
	p, err := c.Parse(context.Background(), "Wake up at 8am")
	if err != nil || p.AssumedTime != nil {
		t.Errorf("preview = %+v %v, want no assumed time", p, err)
	}
}

func TestAssumedTimeHintSaysHowToTypeTheOtherHalf(t *testing.T) {
	cases := map[string]string{
		"at 9pm":    "type 9pm for evening",
		"at 8pm":    "type 8pm for evening",
		"at 6pm":    "type 6pm for evening",
		"at 5pm":    "type 5pm for afternoon",
		"at 5:30pm": "type 5:30pm for afternoon",
		"at 12pm":   "type 12pm for afternoon",
		"at 3pm":    "type 3pm for afternoon",
		"at 10pm":   "type 10pm for evening",
		"at 8am":    "type 8am for morning",
		"at 5:45am": "type 5:45am for morning",
		"":          "",
	}
	for alt, want := range cases {
		if got := (AssumedTime{AlternativeText: alt}).Hint(); got != want {
			t.Errorf("Hint(%q) = %q, want %q", alt, got, want)
		}
	}
}
