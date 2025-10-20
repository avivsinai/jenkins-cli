package root

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type helpDocument struct {
	SchemaVersion string            `json:"schemaVersion"`
	Commands      []helpCommand     `json:"commands"`
	ExitCodes     map[string]string `json:"exitCodes,omitempty"`
}

type helpCommand struct {
	Name        string        `json:"name"`
	Use         string        `json:"use"`
	Description string        `json:"description,omitempty"`
	Long        string        `json:"long,omitempty"`
	Examples    []string      `json:"examples,omitempty"`
	Flags       []helpFlag    `json:"flags,omitempty"`
	Subcommands []helpCommand `json:"subcommands,omitempty"`
}

type helpFlag struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	Persistent  bool   `json:"persistent,omitempty"`
}

func attachJSONHelp(root *cobra.Command) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if wantsJSONOutput(cmd) {
			doc := buildHelpDocument(cmd, cmd == root)
			_ = printHelpJSON(cmd, doc)
			return
		}
		if cmd == root {
			printRootHelp(cmd)
			return
		}
		defaultHelp(cmd, args)
	})
	root.SetHelpCommand(newHelpCommand(root))
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	helpCmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := root
			if len(args) > 0 {
				found, _, err := root.Find(args)
				if err != nil || found == nil {
					return fmt.Errorf("unknown help topic: %s", strings.Join(args, " "))
				}
				target = found
			}

			if wantsJSONOutput(cmd) {
				doc := buildHelpDocument(target, target == root)
				return printHelpJSON(cmd, doc)
			}
			return target.Help()
		},
	}
	return helpCmd
}

func buildHelpDocument(cmd *cobra.Command, includeExitCodes bool) helpDocument {
	doc := helpDocument{
		SchemaVersion: "1.0",
		Commands:      []helpCommand{buildHelpCommand(cmd)},
	}
	if includeExitCodes {
		doc.ExitCodes = defaultExitCodes()
	}
	return doc
}

func buildHelpCommand(cmd *cobra.Command) helpCommand {
	hc := helpCommand{
		Name:        cmd.Name(),
		Use:         strings.TrimSpace(cmd.UseLine()),
		Description: strings.TrimSpace(cmd.Short),
	}
	if long := strings.TrimSpace(cmd.Long); long != "" && long != hc.Description {
		hc.Long = long
	}
	if examples := collectExamples(cmd.Example); len(examples) > 0 {
		hc.Examples = examples
	}
	hc.Flags = collectFlags(cmd)

	children := cmd.Commands()
	sort.Slice(children, func(i, j int) bool {
		return children[i].Name() < children[j].Name()
	})
	for _, child := range children {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		hc.Subcommands = append(hc.Subcommands, buildHelpCommand(child))
	}
	return hc
}

func collectFlags(cmd *cobra.Command) []helpFlag {
	var flags []helpFlag
	seen := make(map[string]struct{})
	appendFlags := func(fs *pflag.FlagSet, persistent bool) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(flag *pflag.Flag) {
			if _, ok := seen[flag.Name]; ok {
				return
			}
			seen[flag.Name] = struct{}{}
			flags = append(flags, helpFlag{
				Name:        flag.Name,
				Shorthand:   flag.Shorthand,
				Type:        flag.Value.Type(),
				Description: strings.TrimSpace(flag.Usage),
				Default:     flag.DefValue,
				Persistent:  persistent,
			})
		})
	}

	appendFlags(cmd.NonInheritedFlags(), false)
	appendFlags(cmd.InheritedFlags(), true)

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Name < flags[j].Name
	})
	return flags
}

func collectExamples(example string) []string {
	example = strings.TrimSpace(example)
	if example == "" {
		return nil
	}
	rawBlocks := strings.Split(example, "\n\n")
	result := make([]string, 0, len(rawBlocks))
	for _, block := range rawBlocks {
		cleaned := strings.TrimSpace(block)
		if cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func wantsJSONOutput(cmd *cobra.Command) bool {
	root := cmd.Root()
	if root == nil {
		return false
	}
	flag := root.PersistentFlags().Lookup("json")
	if flag == nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(flag.Value.String()))
	return value == "true"
}

func printHelpJSON(cmd *cobra.Command, doc helpDocument) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}

func defaultExitCodes() map[string]string {
	return map[string]string{
		"0": "Success",
		"1": "General error",
		"2": "Validation error",
		"3": "Not found",
		"4": "Authentication failure",
		"5": "Permission denied",
		"6": "Connectivity/DNS/TLS failure",
		"7": "Timeout",
		"8": "Feature unsupported",
	}
}

type commandSection struct {
	Title    string
	Commands []*cobra.Command
}

func printRootHelp(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	short := strings.TrimSpace(cmd.Short)
	if short != "" {
		_, _ = fmt.Fprintln(out, short)
		_, _ = fmt.Fprintln(out)
	}

	_, _ = fmt.Fprintf(out, "USAGE\n  %s <command> [subcommand] [flags]\n\n", cmd.CommandPath())

	sections := buildCommandSections(cmd)
	for _, section := range sections {
		printCommandSection(out, section)
	}

	if flags := collectRootFlagRows(cmd); len(flags) > 0 {
		printFlagSection(out, flags)
	}

	printExamplesSection(out)
	printLearnMoreSection(out)
}

func buildCommandSections(cmd *cobra.Command) []commandSection {
	children := availableCommandMap(cmd)
	seen := make(map[string]struct{})

	type sectionDef struct {
		Title string
		Names []string
	}

	defs := []sectionDef{
		{Title: "CORE COMMANDS", Names: []string{"auth", "context", "search", "run", "job"}},
		{Title: "BUILD & RELEASE COMMANDS", Names: []string{"artifact", "log", "test", "queue"}},
		{Title: "JENKINS ADMIN COMMANDS", Names: []string{"node", "plugin", "cred"}},
	}

	var sections []commandSection
	for _, def := range defs {
		var cmds []*cobra.Command
		for _, name := range def.Names {
			child, ok := children[name]
			if !ok {
				continue
			}
			cmds = append(cmds, child)
			seen[name] = struct{}{}
		}
		if len(cmds) == 0 {
			continue
		}
		sections = append(sections, commandSection{
			Title:    def.Title,
			Commands: cmds,
		})
	}

	var extras []*cobra.Command
	for name, child := range children {
		if _, ok := seen[name]; ok {
			continue
		}
		if name == "help" {
			continue
		}
		extras = append(extras, child)
	}
	sort.Slice(extras, func(i, j int) bool {
		return extras[i].Name() < extras[j].Name()
	})
	if len(extras) > 0 {
		sections = append(sections, commandSection{
			Title:    "ADDITIONAL COMMANDS",
			Commands: extras,
		})
	}

	return sections
}

func availableCommandMap(cmd *cobra.Command) map[string]*cobra.Command {
	children := make(map[string]*cobra.Command)
	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		children[child.Name()] = child
	}
	return children
}

func printCommandSection(out io.Writer, section commandSection) {
	if len(section.Commands) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "%s\n", section.Title)
	width := commandLabelWidth(section.Commands)
	for _, child := range section.Commands {
		label := fmt.Sprintf("%s:", child.Name())
		_, _ = fmt.Fprintf(out, "  %-*s %s\n", width, label, strings.TrimSpace(child.Short))
	}
	_, _ = fmt.Fprintln(out)
}

func commandLabelWidth(cmds []*cobra.Command) int {
	max := 0
	for _, child := range cmds {
		label := fmt.Sprintf("%s:", child.Name())
		if w := utf8.RuneCountInString(label); w > max {
			max = w
		}
	}
	// add padding for the space before descriptions
	return max + 2
}

type flagRow struct {
	Label string
	Usage string
}

func collectRootFlagRows(cmd *cobra.Command) []flagRow {
	seen := make(map[string]struct{})
	var rows []flagRow
	appendFlags := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(flag *pflag.Flag) {
			if _, ok := seen[flag.Name]; ok {
				return
			}
			seen[flag.Name] = struct{}{}
			rows = append(rows, flagRow{
				Label: formatFlagLabel(flag),
				Usage: strings.TrimSpace(flag.Usage),
			})
		})
	}

	appendFlags(cmd.LocalFlags())
	appendFlags(cmd.PersistentFlags())

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Label < rows[j].Label
	})
	return rows
}

func formatFlagLabel(flag *pflag.Flag) string {
	label := ""
	if flag.Shorthand != "" {
		label = fmt.Sprintf("-%s, --%s", flag.Shorthand, flag.Name)
	} else {
		label = fmt.Sprintf("    --%s", flag.Name)
	}
	if flag.Value != nil && flag.Value.Type() != "bool" {
		label += " " + flag.Value.Type()
	}
	return label
}

func printFlagSection(out io.Writer, rows []flagRow) {
	if len(rows) == 0 {
		return
	}

	_, _ = fmt.Fprintln(out, "FLAGS")

	width := 0
	for _, row := range rows {
		if w := utf8.RuneCountInString(row.Label); w > width {
			width = w
		}
	}
	width += 2

	for _, row := range rows {
		usage := row.Usage
		if usage == "" {
			usage = "(undocumented)"
		}
		_, _ = fmt.Fprintf(out, "  %-*s %s\n", width, row.Label, usage)
	}
	_, _ = fmt.Fprintln(out)
}

func printExamplesSection(out io.Writer) {
	examples := []string{
		"jk search --job-glob '*deploy-*' --limit 5",
		"jk run start team/app/pipeline --follow",
		"jk artifact download team/app/pipeline 128 -p \"**/*.xml\" -o out/",
	}
	if len(examples) == 0 {
		return
	}
	_, _ = fmt.Fprintln(out, "EXAMPLES")
	for _, ex := range examples {
		_, _ = fmt.Fprintf(out, "  $ %s\n", ex)
	}
	_, _ = fmt.Fprintln(out)
}

func printLearnMoreSection(out io.Writer) {
	_, _ = fmt.Fprintln(out, "LEARN MORE")
	_, _ = fmt.Fprintln(out, "  Use `jk <command> <subcommand> --help` for more information about a command.")
	_, _ = fmt.Fprintln(out, "  Read the docs at https://github.com/avivsinai/jenkins-cli#readme.")
	_, _ = fmt.Fprintln(out, "  View structured help with `jk --json help`.")
}
