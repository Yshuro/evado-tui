# Security policy

## Reporting a vulnerability

Please don't open a public issue for a security problem. Report it privately
through [GitHub's private vulnerability reporting](https://github.com/nerdlabz/evado-tui/security/advisories/new)
instead. Only the maintainers can see the report.

A useful report says what is affected, how to reproduce it, and what someone
could do with it. Include the output of `evado -version` and your platform.

We aim to reply within a week, and will keep you updated while a fix is
prepared. Once a fixed release is out, the advisory is published, with credit
to you unless you would rather stay anonymous.

## What is in scope

This repository is the terminal client: the TUI, the CLI, the todos it keeps
on your computer, and how it stores tokens and sessions. Problems with the
Evado service itself can be reported the same way, and will be passed on.

## Supported versions

Fixes go into the latest release. Upgrade with `brew upgrade evado`, by
updating your Nix flake input, or by downloading the newest archive from
[Releases](https://github.com/nerdlabz/evado-tui/releases).
