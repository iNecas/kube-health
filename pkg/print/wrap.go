package print

// Helper functions for wrapping text and padding strings.

import (
	"bufio"
	"regexp"
	"strings"
)

var (
	wordSeparator = regexp.MustCompile(`\s+`)
	ellipsis      = "..."
)

func wrapLines(s string, width, maxLineWrap int, wrapPrefix string) string {
	w := &strings.Builder{}
	w.Grow(len(s))

	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := scanner.Text()
		writeLineWrapped(w, line, width, maxLineWrap, wrapPrefix)
	}

	return w.String()
}

func writeLineWrapped(w *strings.Builder, s string, width, maxLineWrap int, wrapPrefix string) {
	if len(s) <= width {
		w.WriteString(s + "\n")
		return
	}

	start := 0
	last := []int{0, 0}
	lines := 0
	done := false

	writeLine := func(chunkStart, chunkEnd int) {
		line := s[chunkStart:chunkEnd]
		if maxLineWrap > 0 && lines >= (maxLineWrap-1) {
			done = true
			// Not done with the string, we need to add ellipsis.
			if chunkEnd != len(s) {
				// Make room for ellipsis.
				line = line[:min(len(line), width-len(ellipsis))] + ellipsis
			}
		}

		if lines == 0 {
			w.WriteString(line + "\n")
			// We need to subtract the length of the wrapPrefix from the width
			width -= len(wrapPrefix)
		} else {
			w.WriteString(wrapPrefix + line + "\n")
		}
		lines++
	}

	breakPoints := wordSeparator.FindAllStringIndex(s, -1)

	for _, bp := range breakPoints {
		if bp[0]-start > width {
			if start < last[0] {
				writeLine(start, last[0])
			}
			if done {
				return
			}
			// Set new start.
			start = last[1]
			// Force a break if the word is longer than the width.
			// With this, we should keep the invariant: last[0] - start <= width
			for bp[0]-start > width {
				writeLine(start, start+width)
				if done {
					return
				}

				start += width
			}
		}
		last = bp
	}
	for start < len(s) {
		rest := min(len(s)-start, width)
		writeLine(start, start+rest)
		if done {
			return
		}
		start += rest
	}
}

// padStringKeepControl pads the string to the specified length, but
// keeps the control characters in the string.
func padStringKeepControl(s string, length int) string {
	// Find all control characters in the string.
	controls := controlRe.FindAllStringIndex(s, -1)
	// To make sure we process the last part of the string.
	controls = append(controls, []int{len(s), len(s)})

	cur := 0
	remaining := length

	sb := &strings.Builder{}
	sb.Grow(length)

	for _, control := range controls {
		chunk := []rune(s[cur:control[0]])
		chunkLength := len(chunk)
		if remaining < chunkLength {
			chunk = chunk[:remaining]
			chunkLength = remaining
		}
		for _, r := range chunk {
			sb.WriteRune(r)
		}
		remaining -= chunkLength
		sb.WriteString(s[control[0]:control[1]])
		cur = control[1]
	}

	if remaining > 0 {
		sb.WriteString(strings.Repeat(" ", remaining))
	}

	return sb.String()
}
