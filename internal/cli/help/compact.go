package clihelp

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	BasicHelpMaxLines = 35
	BasicHelpWidth    = 80
)

// RenderBasic validates the complete, untruncated navigational view before
// writing it. An overfull command is a command-design defect, not a reason to
// silently hide subcommands or required flags.
func RenderBasic(cmd *cobra.Command, out io.Writer) error {
	var lines []string
	add := func(line string) { lines = append(lines, line) }
	summary := cmd.Short
	if summary == "" {
		summary = strings.SplitN(cmd.Long, "\n", 2)[0]
	}
	if summary != "" {
		add(summary)
		add("")
	}
	add("Usage:")
	for _, line := range wrapWords("  "+cmd.UseLine(), BasicHelpWidth) {
		add(line)
	}
	var children []*cobra.Command
	for _, child := range cmd.Commands() {
		if !child.Hidden && child.IsAvailableCommand() {
			children = append(children, child)
		}
	}
	if len(children) != 0 {
		add("")
		add("Commands:")
		for _, child := range children {
			// Never abbreviate or elide command names. Descriptions are
			// supplementary; avoid wrapping them into a giant command list.
			line := "  " + child.Name()
			if child.Short != "" {
				padding := 18 - utf8.RuneCountInString(line)
				if padding < 2 { padding = 2 }
				label := line + strings.Repeat(" ", padding)
				if utf8.RuneCountInString(label+child.Short) <= BasicHelpWidth {
					line = label + child.Short
				}
			}
			add(line)
		}
	} else {
		var required, optional []string
		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || flag.Name == "help" { return }
			line := "  --" + flag.Name
			if flag.Value.Type() != "bool" {
				line += " " + flag.Value.Type()
			}
			isRequired := len(flag.Annotations[cobra.BashCompOneRequiredFlag]) > 0
			if isRequired {
				required = append(required, line+" (required)")
			} else if flag.Name == "dry-run" || flag.Name == "project" || flag.Name == "owner" ||
				flag.Name == "provider" || flag.Name == "source" || flag.Name == "ranges" ||
				strings.HasPrefix(flag.Name, "confirm-") || strings.HasPrefix(flag.Name, "consent-") {
				optional = append(optional, line)
			}
		})
		if len(required)+len(optional) > 0 {
			add("")
			add("Options:")
			for _, line := range required { add(line) }
			// Optional flags are informative, never allowed to displace
			// required flags or the route to the complete reference.
			available := BasicHelpMaxLines - len(lines) - 4
			if available < 0 { available = 0 }
			if len(optional) > available { optional = optional[:available] }
			for _, line := range optional { add(line) }
		}
	}
	add("")
	if len(children) > 0 {
		add("Run 'aginctus help <command>' for details.")
	}
	add("Run 'aginctus help --full <command>' for the complete reference.")
	if len(lines) > BasicHelpMaxLines {
		return fmt.Errorf("%s: basic help is %d lines (maximum %d); group commands or revise hierarchy", cmd.CommandPath(), len(lines), BasicHelpMaxLines)
	}
	for i, line := range lines {
		if utf8.RuneCountInString(line) > BasicHelpWidth {
			return fmt.Errorf("%s: basic help line %d exceeds %d columns: %q", cmd.CommandPath(), i+1, BasicHelpWidth, line)
		}
	}
	_, err := io.WriteString(out, strings.Join(lines, "\n")+"\n")
	return err
}

func wrapWords(line string, width int) []string {
	if utf8.RuneCountInString(line) <= width { return []string{line} }
	words := strings.Fields(line)
	if len(words) == 0 { return []string{""} }
	lines := []string{}
	current := "  "
	for _, word := range words {
		next := current
		if strings.TrimSpace(current) != "" { next += " " }
		next += word
		if utf8.RuneCountInString(next) > width && strings.TrimSpace(current) != "" {
			lines = append(lines, current)
			current = "    " + word
		} else {
			current = next
		}
	}
	return append(lines, current)
}
