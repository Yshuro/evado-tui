package ops

import (
	"context"
	"regexp"
	"strings"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// DefaultFeedPages is how many pages Changes reads unless told otherwise. A
// page holds up to 200 todos, so the default covers 10,000 changes, which is
// far more than a scheduled job sees between runs.
const DefaultFeedPages = 50

// ChangesInput asks for what changed after a cursor.
type ChangesInput struct {
	// Since is the cursor an earlier run returned, passed back unchanged.
	// Empty reads every live todo, which is how the first cursor is had: the
	// feed has no memory of who asked, so the caller says where it left off.
	Since string
	// MaxPages bounds the walk. Zero means DefaultFeedPages.
	MaxPages int
}

// ChangedTodo is a todo as the feed reports it, with its checklist inline.
// Deleted is true for a tombstone, whose other fields are what the todo was
// when it was deleted and are not to be shown as live.
type ChangedTodo struct {
	api.SyncTodo
	Deleted bool `json:"deleted"`
}

// Changes is what the change feed said, from Since to the end of the feed or
// to the page bound.
//
// The feed is for keeping a copy in step, not a history. A todo that changed
// several times since Since appears once or more, and the last appearance
// wins, so Todos may repeat an id. Lists and Invitations are snapshots, not
// changes: they are the state on the last page read, and an empty array
// means there are none, where null means the server did not send one.
type Changes struct {
	// Since is the cursor the run started from, empty for a first run.
	Since string        `json:"since"`
	Todos []ChangedTodo `json:"todos"`
	Lists []api.List    `json:"lists"`
	// Invitations is the snapshot on the last page read, like Lists.
	Invitations []api.Invitation `json:"invitations"`
	// Cursor is where the next run should start. It is the service's own
	// position in its change log, never a time, so it is kept and passed
	// back as it came: nothing about it can be worked out here.
	Cursor string `json:"cursor"`
	// HasMore is true exactly when Complete is false: the page bound was
	// reached with more changes waiting after Cursor.
	HasMore  bool `json:"hasMore"`
	Complete bool `json:"complete"`
	Pages    int  `json:"pages"`
}

// Changes reads the change feed from in.Since, following each page's
// cursor until the server says there is no more or the page bound is
// reached. The bound is reported in the result, not as an error: Complete is
// false and Cursor is where to resume. A failure on any page is an error and
// no result, so a partial walk is never mistaken for a whole one.
func (o *Ops) Changes(ctx context.Context, in ChangesInput) (Changes, error) {
	since, err := parseSince(in.Since)
	if err != nil {
		return Changes{}, err
	}
	maxPages := in.MaxPages
	if maxPages == 0 {
		maxPages = DefaultFeedPages
	}
	if maxPages < 0 {
		return Changes{}, usagef("the page bound must be at least 1, not %d", maxPages)
	}
	res := Changes{Since: since, Todos: []ChangedTodo{}}
	at := since
	for res.Pages < maxPages {
		page, err := o.pull(ctx, at)
		if err != nil {
			return Changes{}, err
		}
		if page.Cursor == "" {
			return Changes{}, stalled("the server sent no cursor, so there is nothing to continue from")
		}
		res.Pages++
		for _, st := range page.Todos {
			res.Todos = append(res.Todos, ChangedTodo{SyncTodo: st, Deleted: st.Tombstoned()})
		}
		res.Lists, res.Invitations = page.Lists, page.Invitations
		res.Cursor = page.Cursor
		if !page.HasMore {
			res.Complete = true
			return res, nil
		}
		// A page that says there is more and gives the same place again
		// would be fetched again forever.
		if page.Cursor == at {
			return Changes{}, stalled("the server says there are more changes but its cursor did not move")
		}
		at = page.Cursor
	}
	res.HasMore = true
	return res, nil
}

// pull reads one page: the feed from a cursor, or every live todo when
// there is none, which is the only request that hands out a first cursor.
func (o *Ops) pull(ctx context.Context, cursor string) (api.SyncResponse, error) {
	if cursor == "" {
		return o.C.Sync(ctx, api.SyncRequest{})
	}
	return o.C.ChangeFeed(ctx, cursor)
}

// Err is nil for a complete result. For one cut short by the page bound it
// is the error that gives the command its exit code and tells the caller
// where to resume.
func (c Changes) Err() error {
	if c.Complete {
		return nil
	}
	return &Error{
		Code: codeFeedIncomplete,
		Message: sprintf("stopped after %d pages with more changes waiting; resume with -since %s",
			c.Pages, c.Cursor),
		Exit: ExitIncomplete,
	}
}

// cursorShape is what the service accepts as a cursor. It is checked only to
// turn a timestamp left over from the days when -since was a time into an
// error that says what changed, instead of a bare refusal from the service.
var cursorShape = regexp.MustCompile(`^[0-9]{1,15}$`)

func parseSince(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || cursorShape.MatchString(s) {
		return s, nil
	}
	return "", usagef("-since is a cursor from an earlier run (the \"cursor\" in its output), not a time such as %q; leave it out to read everything and get a first cursor", s)
}

// stalled is a server that cannot be followed to the end of its feed. It is
// the server's fault, so it reads as one.
func stalled(why string) error {
	return &Error{Code: codeFeedStalled, Message: "the change feed cannot be followed: " + why, Exit: ExitServer}
}
