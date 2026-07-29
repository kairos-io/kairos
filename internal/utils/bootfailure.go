package utils

import (
	"fmt"
	"strings"
)

// Operator-facing boot-failure screens.
//
// Any part of immucore that hits a condition the END USER has to fix (bad
// cmdline, missing partitions, unsupported disk layout, ...) should render a
// screen with RenderFailureScreen and hand it to HaltWithBanner. The result
// is a consistent full-screen "KAIROS BOOT FAILED" banner with a subtitle
// naming the failing feature and structured sections (what went wrong / how
// to fix) instead of a log line lost in the systemd scroll.

// ANSI escapes used by the failure screens. Safe on both video consoles
// (linux tty) and serial (any vt100-ish terminal).
const (
	ansiClear   = "\033[2J\033[H"
	ansiReset   = "\033[0m"
	ansiBanner  = "\033[1;97;41m" // bold white on red
	ansiHeading = "\033[1;97m"    // bold white
)

// Conventional section titles used across the failure screens.
const (
	SectionWhatWentWrong = "What went wrong"
	SectionHowToFix      = "How to fix"
)

// FailureSection is one titled block on a failure screen, e.g.
// {Title: SectionWhatWentWrong, Body: "..."} or {Title: SectionHowToFix, ...}.
// Body is free-form preformatted text; keep lines under ~76 columns so they
// survive an 80-column console.
type FailureSection struct {
	Title string
	Body  string
}

// bannerLine renders one full-width line of the red banner bar, centered on
// 78 columns (standard 80-col console minus a 1-col margin either side).
func bannerLine(title string) string {
	const width = 78
	pad := (width - len(title)) / 2
	if pad < 1 {
		pad = 1
	}
	return fmt.Sprintf("%s%s%s%s%s\n",
		ansiBanner,
		strings.Repeat(" ", pad),
		title,
		strings.Repeat(" ", width-pad-len(title)),
		ansiReset,
	)
}

// RenderFailureScreen builds the standard operator-facing failure screen:
//
//	┌──────────────────────────────────────┐
//	│          KAIROS BOOT FAILED          │  <- red banner
//	│              <subtitle>              │
//	└──────────────────────────────────────┘
//	<intro paragraph>
//
//	<section title>            <- bold
//	<section body>
//	...
//
// subtitle names the feature that failed (e.g. "RAM mode: required disk
// partitions are missing"). intro is a short paragraph of context in plain
// language — what the user asked for and where it broke. sections carry the
// details; by convention start with "What went wrong" and include a "How to
// fix" with copy-pasteable cmdline stanzas.
//
// The returned string starts with a clear-screen escape so it owns the
// console when written. HaltWithBanner appends the press-any-key footer —
// do not add one here.
func RenderFailureScreen(subtitle, intro string, sections ...FailureSection) string {
	var b strings.Builder
	b.WriteString(ansiClear)
	b.WriteString(bannerLine(""))
	b.WriteString(bannerLine("KAIROS BOOT FAILED"))
	b.WriteString(bannerLine(subtitle))
	b.WriteString(bannerLine(""))
	b.WriteString("\n")
	if intro != "" {
		b.WriteString(intro)
		if !strings.HasSuffix(intro, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	for _, s := range sections {
		b.WriteString(ansiHeading + s.Title + ansiReset + "\n")
		b.WriteString(s.Body)
		if !strings.HasSuffix(s.Body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Emphasize wraps s in the heading style for inline emphasis inside intro or
// section bodies (e.g. a cmdline token the user typed).
func Emphasize(s string) string {
	return ansiHeading + s + ansiReset
}
