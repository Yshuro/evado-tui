package local

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func inMadrid(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestParseSaysWhatItAssumed(t *testing.T) {
	loc := inMadrid(t)
	at := func(day, hhmm string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		now, in         string
		text, assumed   string
		altText, altDay string // altDay is "2006-01-02 15:04"
	}{
		// At 9 PM the next 8 is tomorrow's, and the evening one is 8 PM tomorrow too.
		{"2026-09-30 21:00", "Wake up at 8", "at 8", "am", "at 8pm", "2026-10-01 20:00"},
		{"2026-09-30 10:00", "Meet at 3", "at 3", "pm", "at 3am", "2026-10-01 03:00"},
		{"2026-09-30 05:53", "Meet at half past 5", "at half past 5", "pm", "at 5:30am", "2026-10-01 05:30"},
		{"2026-09-30 05:53", "Meet at quarter to 6", "at quarter to 6", "pm", "at 5:45am", "2026-10-01 05:45"},
		{"2026-09-30 05:53", "Meet 5 o'clock", "5 o'clock", "pm", "at 5am", "2026-10-01 05:00"},
		// With a day of its own, the other half is on that day.
		{"2026-09-30 10:00", "Wake up tomorrow at 8", "at 8", "am", "at 8pm", "2026-10-01 20:00"},
		{"2026-09-30 10:00", "Meet friday at 5", "at 5", "pm", "at 5am", "2026-10-02 05:00"},
		{"2026-09-30 10:00", "Pay rent on the 15th at 3", "at 3", "pm", "at 3am", "2026-10-15 03:00"},
		// A weekly time starts on its first day; so does the other one.
		{"2026-09-30 10:00", "Standup every monday at 3", "at 3", "pm", "at 3am", "2026-10-05 03:00"},
	}
	for _, c := range cases {
		now, _ := time.ParseInLocation("2006-01-02 15:04", c.now, loc)
		got := Parse(c.in, loc, now)
		a := got.Assumed
		if a == nil {
			t.Errorf("Parse(%q) at %s assumed nothing", c.in, c.now)
			continue
		}
		if a.Text != c.text || a.Assumed != c.assumed || a.AlternativeText != c.altText || !a.AlternativeDoAt.Equal(at(c.altDay[:10], c.altDay[11:])) {
			t.Errorf("Parse(%q) at %s assumed %+v; want %q %s, then %q at %s", c.in, c.now, a, c.text, c.assumed, c.altText, c.altDay)
		}
	}
}

// Saying the other half outright reads as the alternative, and then nothing
// is guessed.
func TestSayingTheAlternativeOutrightAssumesNothing(t *testing.T) {
	loc := inMadrid(t)
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, loc)
	for _, in := range []string{"Wake up at 8", "Meet tomorrow at 5", "Meet at half past 5", "Meet at 7 o'clock", "Standup every monday at 3"} {
		first := Parse(in, loc, now)
		if first.Assumed == nil {
			t.Fatalf("Parse(%q) assumed nothing", in)
		}
		said := Parse(strings.Replace(in, first.Assumed.Text, first.Assumed.AlternativeText, 1), loc, now)
		if said.Assumed != nil || said.DoAt == nil || !said.DoAt.Equal(first.Assumed.AlternativeDoAt) || said.Title != first.Title {
			t.Errorf("Parse(%q) with the alternative said = %+v, want %v and no guess", in, said, first.Assumed.AlternativeDoAt)
		}
	}
}

func TestParseAssumesNothingWhenTheTimeSaysWhich(t *testing.T) {
	loc := inMadrid(t)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, loc)
	for _, in := range []string{
		"Buy milk", "Meet tomorrow", "Pay rent on the 15th", "Meet at 3pm", "Meet at 3 pm", "Meet at 15:00", "Meet at 03:00",
		"Meet at noon", "Meet at 12", "Meet at 12:30", "Meet at 3 in the afternoon", "Call at 8 tonight", "Meet at 3.30", "Stand up in an hour",
		"Pay 5,000 at 5,000 rate", "Meet half past 13 tomorrow",
	} {
		if got := Parse(in, loc, now); got.Assumed != nil {
			t.Errorf("Parse(%q) assumed %+v, want nothing", in, got.Assumed)
		}
	}
}

// With no account, the preview says what was guessed, in the service's shape.
func TestPreviewCarriesTheAssumedTime(t *testing.T) {
	b := open(t, "", "")
	loc := b.loc()
	b.now = func() time.Time { return time.Date(2031, 3, 3, 21, 0, 0, 0, loc) }

	req, err := http.NewRequest(http.MethodPost, address+"/parse", strings.NewReader(`{"title":"Wake up at 8"}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := b.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	var body struct {
		Parsed map[string]json.RawMessage `json:"parsed"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	var got map[string]any
	if err := json.Unmarshal(body.Parsed["assumedTime"], &got); err != nil {
		t.Fatalf("assumedTime in %s: %v", raw, err)
	}
	alt, err := time.Parse(time.RFC3339, got["alternativeDoAt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got["text"] != "at 8" || got["assumed"] != "am" || got["alternativeText"] != "at 8pm" ||
		!alt.Equal(time.Date(2031, 3, 4, 20, 0, 0, 0, loc)) {
		t.Errorf("assumedTime = %v, want the service's four fields for at 8 read as am", got)
	}

	// A time that says which half has the field too, as null.
	req, _ = http.NewRequest(http.MethodPost, address+"/parse", strings.NewReader(`{"title":"Wake up at 8am"}`))
	res, err = b.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	if !strings.Contains(string(raw), `"assumedTime":null`) {
		t.Errorf("preview of a time that says which half = %s, want assumedTime null", raw)
	}

	p, err := b.Client().Parse(context.Background(), "Wake up at 8")
	if err != nil || p.AssumedTime == nil || p.AssumedTime.Assumed != "am" || p.AssumedTime.Hint() != "type 8pm for evening" {
		t.Errorf("client preview = %+v %v", p, err)
	}
}

// Signed in, the service's own account of a guess is kept.
func TestCaptureKeepsTheServicesAssumedTime(t *testing.T) {
	svc, url := newParsingService(t)
	svc.assumed = map[string]any{
		"text": "at 8", "assumed": "am", "alternativeText": "at 8pm", "alternativeDoAt": serviceDay.Add(6 * time.Hour),
	}
	c := signedIn(t, svc.service, url).Client()

	p, err := c.Parse(context.Background(), "Wake up at 8")
	if err != nil || p.AssumedTime == nil || p.AssumedTime.Text != "at 8" || p.AssumedTime.AlternativeText != "at 8pm" ||
		!p.AssumedTime.AlternativeDoAt.Equal(serviceDay.Add(6*time.Hour)) {
		t.Errorf("preview = %+v %v, want the service's assumed time", p, err)
	}
	if got := fromService(p, "Wake up at 8"); got.Assumed == nil || got.Assumed.Assumed != "am" {
		t.Errorf("fromService dropped the assumed time: %+v", got)
	}
}
