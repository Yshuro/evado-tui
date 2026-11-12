package ops

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// Exit codes. They are stable, so a script or an agent can branch on them
// without parsing text.
const (
	ExitOK          = 0
	ExitFailure     = 1 // something unexpected
	ExitUsage       = 2 // bad arguments, or a reference that matches several todos
	ExitAuth        = 3 // the token is missing or rejected
	ExitNotFound    = 4 // no such todo or list
	ExitRateLimited = 5 // retry after a pause
	ExitRejected    = 6 // the server refused the change: invalid, conflict, read-only list
	ExitForbidden   = 7 // the token is not allowed to do this
	ExitServer      = 8 // the server failed; retrying may help
	ExitNetwork     = 9 // could not reach the server
	// ExitIncomplete is not a failure: the command did what it was asked as
	// far as its bound allowed, printed that much, and stopped with more
	// left to do. It is its own code so a cron job can tell "resume from the
	// cursor" from "something broke".
	ExitIncomplete     = 10
	codeUsage          = "usage"
	codeAmbiguous      = "ambiguous"
	codeIncomplete     = "incomplete_lookup"
	codeNotFound       = "not_found"
	codeConfirmation   = "confirmation_required"
	codeFeedIncomplete = "feed_incomplete"
	codeFeedStalled    = "feed_stalled"
	codeListStalled    = "list_stalled"
)

// Error is a failure that is the caller's to fix, as opposed to one the API
// reported. It names what went wrong and, for an ambiguous reference, the
// candidates.
type Error struct {
	Code       string
	Message    string
	Candidates []string
	// Exit, when set, is the exit code. Without it a caller's error is a
	// usage error, or not found for a missing todo or list.
	Exit int
	// Cause, when set, is an API or network failure this error adds context
	// to. The exit code is the cause's, so a script still sees "server
	// error, retry" and not a usage error.
	Cause error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func usagef(format string, args ...any) error {
	return &Error{Code: codeUsage, Message: sprintf(format, args...)}
}

func notFoundf(format string, args ...any) error {
	return &Error{Code: codeNotFound, Message: sprintf(format, args...)}
}

// Info is an error in the shape scripts and agents read.
type Info struct {
	Code       string           `json:"code"`
	Message    string           `json:"message"`
	ExitCode   int              `json:"exitCode"`
	Retryable  bool             `json:"retryable,omitempty"`
	RequestID  string           `json:"requestId,omitempty"`
	RetryAfter int              `json:"retryAfterSeconds,omitempty"`
	Fields     []api.FieldError `json:"fields,omitempty"`
	Candidates []string         `json:"candidates,omitempty"`
	// Todo and Assignee are set for a claim or release that lost: the todo
	// as it is now, and who has it. A conflict with no Assignee means
	// nobody the list knows has it.
	Todo     *api.Todo   `json:"todo,omitempty"`
	Assignee *api.Member `json:"assignee,omitempty"`
}

// Describe turns any error into its Info.
func Describe(err error) Info {
	if err == nil {
		return Info{}
	}
	var oe *Error
	if errors.As(err, &oe) {
		if oe.Cause != nil {
			info := Describe(oe.Cause)
			info.Message = oe.Message + ": " + info.Message
			return info
		}
		code := ExitUsage
		switch {
		case oe.Exit != 0:
			code = oe.Exit
		case oe.Code == codeNotFound:
			code = ExitNotFound
		}
		return Info{Code: oe.Code, Message: oe.Message, ExitCode: code, Candidates: oe.Candidates}
	}
	var ae *api.Error
	if errors.As(err, &ae) {
		info := Info{
			Code: ae.Code, Message: ae.Message, ExitCode: exitForAPI(ae),
			Retryable: ae.Retryable(), RequestID: ae.RequestID,
			RetryAfter: int(ae.RetryAfter.Seconds()), Fields: ae.Fields,
		}
		if info.Message == "" {
			info.Message = ae.Error()
		}
		if info.Code == "" {
			info.Code = "error"
		}
		var conflict *api.AssignmentConflict
		if errors.As(err, &conflict) {
			info.Todo, info.Assignee = conflict.Todo, conflict.Assignee
			info.Message = conflictMessage(conflict, info.Message)
		}
		return info
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) || strings.Contains(err.Error(), "connection refused") {
		return Info{Code: "network", Message: err.Error(), ExitCode: ExitNetwork, Retryable: true}
	}
	return Info{Code: "error", Message: err.Error(), ExitCode: ExitFailure}
}

// ExitCode is the process exit code for err.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	return Describe(err).ExitCode
}

// conflictMessage adds who has the todo to the service's own words, which
// only say that someone does.
func conflictMessage(c *api.AssignmentConflict, said string) string {
	switch {
	case c.Err.Code == api.CodeTodoCompleted:
		return said + "; reopen it first"
	case c.Holder() != "":
		return said + "; " + c.Holder() + " has it"
	case c.Err.Code == api.CodeNotAssignedToYou:
		return said + "; it is assigned to someone who has left the list"
	}
	return said
}

func exitForAPI(e *api.Error) int {
	switch {
	case e.StatusCode == http.StatusUnauthorized || e.Code == api.CodeUnauthorized:
		return ExitAuth
	case e.StatusCode == http.StatusTooManyRequests || e.Code == api.CodeRateLimited:
		return ExitRateLimited
	case e.StatusCode == http.StatusNotFound || e.Code == api.CodeNotFound:
		return ExitNotFound
	case e.StatusCode == http.StatusForbidden:
		if e.Code == api.CodeListReadOnly {
			return ExitRejected
		}
		return ExitForbidden
	case e.StatusCode >= 500:
		return ExitServer
	case e.StatusCode >= 400:
		return ExitRejected
	}
	return ExitFailure
}
