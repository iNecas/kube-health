package print

// Code for printing the status of resources in a tabular format.

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"k8s.io/utils/integer"

	"github.com/inecas/kube-health/pkg/status"
)

var (
	controlRe = regexp.MustCompile(fmt.Sprintf("%c\\[\\d+m", ESC))
	cellSep   = "  "
)

// Column defines a column in a table.
type Column struct {
	Header      string
	Width       int
	MaxLineWrap int // Maximum number of lines to wrap the content to.
	WrapPrefix  string
	FormatFn    func(obj interface{}) string
}

// Cell is a single cell in a table in a specific column.
type Cell struct {
	Column  Column
	Content string
}

// FormatFn is a wrapper of a function of specific type to a function
// of interface{}. It acts as an adapter to allow using the function
// with the Column.FormatFn.
func FormatFn[T any](formatFn func(T) string) func(interface{}) string {
	return func(obj interface{}) string {
		return formatFn(obj.(T))
	}
}

// Format turns the object into a string for the Cell using the FormatFn.
func (c Column) Format(obj interface{}) Cell {
	return Cell{
		Content: c.FormatFn(obj),
		Column:  c,
	}
}

func formatRow(cols []Column, obj interface{}) []Cell {
	row := make([]Cell, len(cols))
	for i, col := range cols {
		cell := col.Format(obj)
		row[i] = cell
	}
	return row
}

func blankColumn(header string, width int) Column {
	return Column{
		Header:   header,
		Width:    width,
		FormatFn: func(obj interface{}) string { return "" },
	}
}

var (
	// Blank column to align with the resource column.
	objectIndentCol = blankColumn("OBJECT", 15)
	conditionsCols  = []Column{
		objectIndentCol,
		{
			Header:   "CONDITION",
			Width:    30,
			FormatFn: FormatFn(formatConditionType),
		},
		{
			Header:   "AGE",
			Width:    5,
			FormatFn: FormatFn(formatConditionAge),
		},
		{
			Header:   "REASON",
			Width:    30,
			FormatFn: FormatFn(formatConditionReason),
		},
	}
	conditionMessageCols = []Column{
		objectIndentCol,
		// Indent the message under the condition column.
		// Although the width is 0, we wan't to keep it to preserve the spacing.
		blankColumn("", 0),
		{
			Header: "MESSAGE",
			// The 40 is the minimal width: it gets adjusted to the terminal width
			// as it's the last column.
			Width:       40,
			MaxLineWrap: 3,
			WrapPrefix:  "    ",
			FormatFn:    FormatFn(formatConditionMessage),
		},
	}
)

func formatConditionType(cond status.ConditionStatus) string {
	color, setColor := statusColor(cond.Status())
	if setColor {
		return SprintfWithColor(color, cond.Type)
	} else {
		return cond.Type
	}
}

func formatStatus(obj status.ObjectStatus) string {
	s := obj.Status()
	color, setColor := statusColor(s)
	ret := statusMessage(s)
	if setColor {
		ret = SprintfWithColor(color, ret)
	}
	return ret
}

func statusColor(s status.Status) (Color, bool) {
	if s.Progressing {
		return YELLOW, true
	}

	switch s.Result {
	case status.Ok:
		return GREEN, true
	case status.Warning:
		return YELLOW, true
	case status.Error:
		return RED, true
	}
	return 0, false
}

func statusMessage(s status.Status) string {
	if s.Progressing {
		return "Progressing"
	} else {
		return s.Status
	}
}

func formatConditionAge(cond status.ConditionStatus) string {
	return formatTimeSince(cond.Condition.LastTransitionTime.Time)
}

func formatTimeSince(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	since := time.Since(t)
	switch {
	case since.Seconds() <= 90:
		return fmt.Sprintf("%ds", integer.RoundToInt32(since.Round(time.Second).Seconds()))
	case since.Minutes() <= 90:
		return fmt.Sprintf("%dm", integer.RoundToInt32(since.Round(time.Minute).Minutes()))
	default:
		return fmt.Sprintf("%dh", integer.RoundToInt32(since.Round(time.Hour).Hours()))
	}
}

func formatConditionReason(cond status.ConditionStatus) string {
	return cond.Reason
}

func formatConditionMessage(cond status.ConditionStatus) string {
	return cond.Message
}

func formatObject(obj status.ObjectStatus, root, printGroups bool) string {
	status := formatStatus(obj)
	fullName := ""
	if root {
		fullName += obj.Object.GetNamespace() + "/"
	}
	fullName += fmt.Sprintf("%s/%s", obj.Object.Kind, obj.Object.GetName())
	if printGroups {
		fullName += fmt.Sprintf(" [%s]", obj.Object.GroupVersionKind().Group)
	}

	text := fmt.Sprintf("%s %s", status, fullName)
	return text
}

// TablePrinter implements StatusPrinter interface for printing the status
// of resources in a tabular format.
type TablePrinter struct {
	PrintOpts PrintOptions
}

func NewTablePrinter(opts PrintOptions) *TablePrinter {
	return &TablePrinter{
		PrintOpts: opts,
	}
}

func (t *TablePrinter) PrintStatuses(objects []status.ObjectStatus, w io.Writer) {
	t.printHeader(w, conditionsCols)

	sortObjects(objects)

	for _, obj := range objects {
		subObjects := obj.SubStatuses
		prefixTail := ""
		printSubResources := len(subObjects) > 0 && t.shouldPrintDetails(obj)
		if printSubResources {
			prefixTail = "│ "
		}
		t.printObjectWithConditions(w, obj, "", prefixTail)

		if printSubResources {
			t.printSubTable(w, subObjects, "")
		}
	}
}

// shouldPrintDetails decides whether to print the details of the object.
func (t *TablePrinter) shouldPrintDetails(obj status.ObjectStatus) bool {
	if t.PrintOpts.ShowOk {
		return true
	}
	return obj.Status().Result > status.Ok || obj.Status().Progressing
}

func (t *TablePrinter) printObjectWithConditions(w io.Writer, obj status.ObjectStatus, prefixHead, prefixTail string) {
	t.printObject(w, obj, prefixHead)
	if t.shouldPrintDetails(obj) {
		t.printConditions(w, obj, prefixTail)
	}
}

func (t *TablePrinter) printObject(w io.Writer, obj status.ObjectStatus, prefix string) {
	t.printf(w, "%s%s\n", prefix, formatObject(obj, prefix == "", t.PrintOpts.ShowGroup))
}

func (t *TablePrinter) printConditions(w io.Writer, obj status.ObjectStatus, prefix string) {
	for _, cond := range obj.Conditions {
		row := formatRow(conditionsCols, cond)
		t.printRow(w, row, prefix, prefix)
		if cond.Status().Result > status.Ok || cond.Status().Progressing {
			row = formatRow(conditionMessageCols, cond)
			t.printRow(w, row, prefix, prefix)
		}
	}
}

func (t *TablePrinter) printHeader(w io.Writer, cols []Column) {
	row := make([]Cell, len(cols))
	for i, col := range cols {
		row[i] = Cell{
			Column:  col,
			Content: col.Header,
		}
	}

	t.printRow(w, row, "", "")
}

func (t *TablePrinter) printRow(w io.Writer, row []Cell, prefixHead, prefixTail string) {
	maxLines := 0
	cellTxt := make([]string, len(row))
	curWidth := 0
	for i, cell := range row {
		txt := cell.Content
		width := cell.Column.Width
		if i == len(row)-1 && t.PrintOpts.Width > 0 {
			// Try to allocate the rest of the width for the last column,
			// if known.
			// We use len(cellSep) to keep some space on the right edge.
			width = max(width, t.PrintOpts.Width-curWidth-len(cellSep))
			txt = wrapLines(txt, width, cell.Column.MaxLineWrap, cell.Column.WrapPrefix)
		}

		cellTxt[i] = strings.TrimSpace(txt)

		curWidth += width + len(cellSep)
	}

	// Some cells in the row might have multiple lines. We need to know
	// the maximum number of lines to print for the whole row.
	cellLines := make([][]string, len(row))
	for i, txt := range cellTxt {
		cellLines[i] = strings.Split(txt, "\n")
	}

	for _, lines := range cellLines {
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}

	// Iterate over the lines that need to be printed for the row and combine
	// the content of individual cells.
	for i := 0; i < maxLines; i++ {
		for j, cell := range row {
			txt := ""
			lines := cellLines[j]
			if j == 0 {
				if i == 0 {
					txt = prefixHead
				} else {
					txt = prefixTail
				}
			}

			if i < len(lines) {
				txt += lines[i]
			}

			// Don't pad the last column.
			if j != len(row)-1 {
				txt = padStringKeepControl(txt, cell.Column.Width) + cellSep
			}

			t.printf(w, txt)
		}
		t.printf(w, "\n")
	}
}

// printSubTable prints out any subresources that belong to the
// object. This function takes care of printing the correct tree
// structure and indentation.
func (t *TablePrinter) printSubTable(w io.Writer, objects []status.ObjectStatus, prefix string) {
	sortObjects(objects)
	for j, obj := range objects {
		var newPrefixHead, newPrefixTail string
		if j < len(objects)-1 {
			newPrefixHead = `├─ `
			newPrefixTail = `│  `
		} else {
			newPrefixHead = `└─ `
			newPrefixTail = "   "
		}

		if t.shouldPrintDetails(obj) && len(obj.SubStatuses) > 0 {
			// Add an extra level of indentation if there are subresources to print.
			newPrefixTail += "│ "
		}

		t.printObjectWithConditions(w, obj, prefix+newPrefixHead, prefix+newPrefixTail)

		var newPrefix string
		if j < len(objects)-1 {
			newPrefix = `│  `
		} else {
			newPrefix = "   "
		}
		if t.shouldPrintDetails(obj) {
			t.printSubTable(w, obj.SubStatuses, prefix+newPrefix)
		}
	}
}

func (t *TablePrinter) printf(w io.Writer, format string, a ...interface{}) {
	_, err := fmt.Fprintf(w, format, a...)
	if err != nil {
		panic(err)
	}
}

func sortObjects(objects []status.ObjectStatus) {
	fullName := func(obj status.ObjectStatus) string {
		return fmt.Sprintf("%s %s %s", obj.Object.GetNamespace(), obj.Object.Kind, obj.Object.GetName())
	}
	slices.SortFunc(objects, func(a, b status.ObjectStatus) int {
		return strings.Compare(fullName(a), fullName(b))
	})
}
