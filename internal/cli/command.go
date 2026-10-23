package cli

// Command pairs an entrypoint with its schema. Command groups (e.g. `cache`)
// expose their subcommands as a map of these for main's dispatch table.
type Command struct {
	Run    func(args []string, flags map[string][]string) error
	Schema Schema
}
