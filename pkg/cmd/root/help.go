package root

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
