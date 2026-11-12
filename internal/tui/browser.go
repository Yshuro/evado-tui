package tui

import (
	"context"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func webAddress(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(raw, "\x1b\x07\x7f") {
		return ""
	}
	return u.String()
}

// openWebAddress is separate so tests never start a real browser.
var openWebAddress = func(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{address}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		command, args = "xdg-open", []string{address}
	}
	return exec.CommandContext(ctx, command, args...).Run()
}

func webLink(address string) string {
	return webLinkText(address, address)
}

func webLinkText(address, label string) string {
	return "\x1b]8;;" + address + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}
