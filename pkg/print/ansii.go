package print

// Helper functions for printing to the terminal.

import "fmt"

const (
	// RESET is the escape sequence for unsetting any previous commands.
	RESET = 0
	// ESC is the escape sequence used to send ANSI commands in the terminal.
	ESC = 27
)

// color is a type that captures the ANSI code for colors on the
// terminal.
type Color int

var (
	RED    Color = 31
	GREEN  Color = 32
	YELLOW Color = 33
)

// SprintfWithColor formats according to the provided pattern and returns
// the result as a string with the necessary ansii escape codes for
// color
func SprintfWithColor(color Color, format string, a ...interface{}) string {
	return fmt.Sprintf("%c[%dm", ESC, color) +
		fmt.Sprintf(format, a...) +
		fmt.Sprintf("%c[%dm", ESC, RESET)
}
