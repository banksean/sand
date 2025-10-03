package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
)

// MarkdownHelpPrinter is a kong.HelpPrinter that formats help output as markdown.
func MarkdownHelpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	w := ctx.Stdout
	if w == nil {
		w = io.Discard
	}

	// Get the root node
	root := ctx.Model.Node

	// Print main title
	fmt.Fprintf(w, "# %s\n\n", ctx.Model.Name)

	// Print app-level description if available
	if root.Help != "" && !options.NoAppSummary {
		fmt.Fprintf(w, "%s\n\n", root.Help)
	}

	// Print global flags
	printGlobalFlags(w, ctx)

	// Print all commands recursively
	fmt.Fprintf(w, "## Commands\n\n")
	printCommands(w, ctx, root, ctx.Model.Name, 2)

	return nil
}

// printGlobalFlags prints the global flags section
func printGlobalFlags(w io.Writer, ctx *kong.Context) {
	// Get only the flags defined at the root level
	var globalFlags []*kong.Flag
	for _, flag := range ctx.Model.Flags {
		if !flag.Hidden && flag.Group == nil {
			globalFlags = append(globalFlags, flag)
		}
	}

	if len(globalFlags) > 0 {
		fmt.Fprintf(w, "## Global Flags\n\n")
		for _, flag := range globalFlags {
			printFlag(w, flag)
		}
		fmt.Fprintf(w, "\n")
	}
}

// printCommands recursively prints all commands and their subcommands
func printCommands(w io.Writer, ctx *kong.Context, node *kong.Node, prefix string, level int) {
	for _, child := range node.Children {
		if child.Hidden || child.Type != kong.CommandNode {
			continue
		}

		cmdPath := prefix + " " + child.Name
		heading := strings.Repeat("#", level)

		// Print command heading
		fmt.Fprintf(w, "%s `%s`\n\n", heading, cmdPath)

		// Print command description
		if child.Help != "" {
			fmt.Fprintf(w, "%s\n\n", child.Help)
		}

		// Build usage string with flags and args
		usage := buildUsage(cmdPath, child)
		fmt.Fprintf(w, "**Usage:**\n\n```\n%s\n```\n\n", usage)

		// Print command-specific flags
		if len(child.Flags) > 0 {
			fmt.Fprintf(w, "**Flags:**\n\n")
			for _, flag := range child.Flags {
				if !flag.Hidden {
					printFlag(w, flag)
				}
			}
			fmt.Fprintf(w, "\n")
		}

		// Recurse into subcommands if any
		if len(child.Children) > 0 {
			printCommands(w, ctx, child, cmdPath, level+1)
		}
	}
}

// printFlag prints a single flag in markdown format
func printFlag(w io.Writer, flag *kong.Flag) {
	// Build flag signature
	var flagSig strings.Builder
	if flag.Short != 0 {
		flagSig.WriteString(fmt.Sprintf("`-%c", flag.Short))
		if flag.Name != "" {
			flagSig.WriteString(fmt.Sprintf(", --%s", flag.Name))
		}
		flagSig.WriteString("`")
	} else {
		flagSig.WriteString(fmt.Sprintf("`--%s`", flag.Name))
	}

	// Add type info if not a boolean
	if !flag.IsBool() {
		flagSig.WriteString(fmt.Sprintf(" _%s_", flag.FormatPlaceHolder()))
	}

	fmt.Fprintf(w, "- %s", flagSig.String())

	// Add help text
	if flag.Help != "" {
		fmt.Fprintf(w, " - %s", flag.Help)
	}

	// Add default value if present
	if flag.Default != "" {
		fmt.Fprintf(w, " (default: `%s`)", flag.Default)
	}

	fmt.Fprintf(w, "\n")
}

// buildUsage constructs a usage string including flags and positional arguments
func buildUsage(cmdPath string, node *kong.Node) string {
	usage := cmdPath

	// Add flags indicator if there are flags
	if len(node.Flags) > 0 {
		usage += " [flags]"
	}

	// Add positional arguments
	for _, arg := range node.Positional {
		argName := strings.ToUpper(arg.Name)

		// Check if it's optional or required
		if arg.Required {
			usage += fmt.Sprintf(" <%s>", argName)
		} else {
			usage += fmt.Sprintf(" [%s]", argName)
		}

		// Handle variadic args
		if arg.Passthrough {
			usage += "..."
		}
	}

	return usage
}
