package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/youthesame/elport/internal/help"
)

var Version = ""

var (
	entities = []string{"experiments", "items"}
	scopes   = []string{"self", "team", "all"}
	orders   = []string{"cat", "comment", "customid", "date", "id", "lastchange", "rating", "status", "title", "user"}
	sorts    = []string{"asc", "desc"}
)

const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

type Command struct {
	Name        string
	Profile     string
	Entity      string
	Docs        []string
	Doc         string
	Text        string
	ID          string
	Title       string
	Output      string
	Topic       string
	Action      string
	ProfileName string
	Scope       string
	Query       string
	Tags        []string
	Order       string
	Sort        string
	Limit       int
	All         bool
	AllRoot     string
	Offset      int
	JSON        bool
	Team        bool
	DryRun      bool
	Force       bool
	Yes         bool
	Base        bool
	Resolved    bool
}

type usageError struct {
	msg string
}

func (e usageError) Error() string {
	return e.msg
}

func versionString() string {
	if Version != "" {
		return strings.TrimPrefix(Version, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

func rootHelp() string {
	return "usage: elport [-h] [--version] <command> ...\n\n" +
		"git-like sync CLI for eLabFTW (local is authoritative).\n" +
		"Agents: run `elport help agents` before acting.\n\n" +
		"commands:\n" +
		"  help        print a help topic\n" +
		"  push        upload the local document (-n/--dry-run plans without sending)\n" +
		"  pull        download the remote body and attachments\n" +
		"  fetch       download all remote attachments locally\n" +
		"  status      show local/remote sync state\n" +
		"  comments    show the remote comment thread\n" +
		"  comment     post a remote comment\n" +
		"  diff        show changes against remote or base\n" +
		"  merge       merge local and remote sidecars using git merge-file\n" +
		"  new         create a new remote entity and local document\n" +
		"  list        list remote entities\n" +
		"  activity    heatmap of entities created per day, up to a year\n" +
		"  view        read a remote entity (-o DIR saves a local snapshot)\n" +
		"  clone       start a local document from an existing entity (to read, use view)\n" +
		"  whoami      show the authenticated user and active team\n" +
		"  login       store base_url and api_key for a profile\n" +
		"  logout      remove the stored api_key for a profile\n" +
		"  profile     list profiles or select the default with 'use'\n\n" +
		help.Summary()
}

func newFlagSet(name string) *pflag.FlagSet {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.SortFlags = false
	return fs
}

func addTargetFlags(fs *pflag.FlagSet, cmd *Command) {
	fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	fs.StringVar(&cmd.Entity, "entity", "", "target entity type (default: experiments)")
}

func validateChoice(flagName, value string, allowed []string) error {
	if value == "" {
		return nil
	}
	if !slices.Contains(allowed, value) {
		return usageError{msg: fmt.Sprintf("invalid %s %q (choose from %s)", flagName, value, strings.Join(allowed, ", "))}
	}
	return nil
}

func defaultDocs(args []string) []string {
	if len(args) == 0 {
		return []string{"report.md"}
	}
	return args
}

func Parse(args []string, stdout io.Writer) (*Command, int, error) {
	rootFS := newFlagSet("elport")
	rootFS.SetInterspersed(false)
	var showVersion bool
	rootFS.BoolVar(&showVersion, "version", false, "show version and exit")

	if err := rootFS.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			_, _ = io.WriteString(stdout, rootHelp())
			return nil, ExitOK, nil
		}
		return nil, ExitUsage, usageError{msg: err.Error()}
	}
	if showVersion {
		_, _ = fmt.Fprintf(stdout, "elport %s\n", versionString())
		return nil, ExitOK, nil
	}

	rest := rootFS.Args()
	if len(rest) == 0 {
		return nil, ExitUsage, usageError{msg: "missing command (run `elport --help` for usage)"}
	}

	name := rest[0]
	subArgs := rest[1:]
	cmd := &Command{Name: name}
	fs := newFlagSet("elport " + name)

	switch name {
	case "help":
	case "push":
		addTargetFlags(fs, cmd)
		fs.BoolVarP(&cmd.DryRun, "dry-run", "n", false, "show what would change without uploading")
		fs.BoolVarP(&cmd.Force, "force", "f", false, "overwrite even if the remote has diverged")
		fs.BoolVarP(&cmd.Yes, "yes", "y", false, "skip confirmations (access widening, large uploads)")
	case "pull", "fetch", "status":
		addTargetFlags(fs, cmd)
	case "comments", "comment":
		addTargetFlags(fs, cmd)
	case "diff":
		addTargetFlags(fs, cmd)
		fs.BoolVar(&cmd.Base, "base", false, "diff against the recorded base instead of the remote")
	case "merge":
		addTargetFlags(fs, cmd)
		fs.BoolVar(&cmd.Resolved, "resolved", false, "mark a hand-merged conflict as resolved")
	case "new":
		fs.StringVar(&cmd.Entity, "entity", "experiments", "entity type to create (default: experiments)")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
		fs.StringVarP(&cmd.Output, "output", "o", "report.md", "local document to create (default: report.md)")
	case "list":
		fs.StringVar(&cmd.Entity, "entity", "", "entity type to list (default: experiments)")
		fs.StringVar(&cmd.Scope, "scope", "self", "whose entities to list: self, team, all (default: self)")
		fs.StringVarP(&cmd.Query, "query", "q", "", "search term")
		fs.StringArrayVar(&cmd.Tags, "tag", nil, "only entities with this tag (repeat to require several)")
		fs.StringVar(&cmd.Order, "order", "", "sort by this field")
		fs.StringVar(&cmd.Sort, "sort", "", "sort direction (default: desc)")
		fs.IntVar(&cmd.Limit, "limit", 50, "maximum number of results (default: 50)")
		fs.BoolVar(&cmd.All, "all", false, "page through every result")
		fs.IntVar(&cmd.Offset, "offset", 0, "skip this many results")
		fs.BoolVar(&cmd.JSON, "json", false, "emit the raw API rows as JSON")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "activity":
		fs.BoolVar(&cmd.Team, "team", false, "one heatmap per author in your team")
		fs.StringVar(&cmd.Entity, "entity", "", "entity type to count (default: experiments)")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "view":
		fs.StringVar(&cmd.Entity, "entity", "", "entity type to view (default: experiments)")
		fs.StringVarP(&cmd.Output, "output", "o", "", "write body and attachments into DIR")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "clone":
		fs.StringVar(&cmd.AllRoot, "all", "", "clone every entity not yet under ROOT")
		fs.StringVar(&cmd.Entity, "entity", "", "entity type to clone (default: experiments)")
		fs.StringVar(&cmd.Scope, "scope", "self", "whose entities --all clones (default: self)")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
		fs.StringVarP(&cmd.Output, "output", "o", "report.md", "local document to create (default: report.md)")
	case "whoami":
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "login", "logout", "profile":
	default:
		return nil, ExitUsage, usageError{msg: fmt.Sprintf("unknown command %q (run `elport --help` for usage)", name)}
	}

	if err := fs.Parse(subArgs); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			_, _ = fmt.Fprintf(stdout, "usage: elport %s [options]\n\n%s", name, fs.FlagUsages())
			return nil, ExitOK, nil
		}
		return nil, ExitUsage, usageError{msg: err.Error()}
	}

	if err := validateChoice("--entity", cmd.Entity, entities); err != nil {
		return nil, ExitUsage, err
	}
	if err := validateChoice("--scope", cmd.Scope, scopes); err != nil {
		return nil, ExitUsage, err
	}
	if err := validateChoice("--order", cmd.Order, orders); err != nil {
		return nil, ExitUsage, err
	}
	if err := validateChoice("--sort", cmd.Sort, sorts); err != nil {
		return nil, ExitUsage, err
	}

	pos := fs.Args()
	switch name {
	case "help":
		if len(pos) > 1 {
			return nil, ExitUsage, usageError{msg: "help accepts at most one topic"}
		}
		if len(pos) == 1 {
			if _, ok := help.Read(pos[0]); !ok {
				return nil, ExitUsage, usageError{msg: fmt.Sprintf("invalid help topic %q (choose from agents, workflow, documents)", pos[0])}
			}
			cmd.Topic = pos[0]
		}
	case "push", "pull", "fetch", "status", "diff":
		cmd.Docs = defaultDocs(pos)
	case "comments", "merge":
		if len(pos) > 1 {
			return nil, ExitUsage, usageError{msg: fmt.Sprintf("%s accepts at most one document", name)}
		}
		if len(pos) == 1 {
			cmd.Doc = pos[0]
		} else {
			cmd.Doc = "report.md"
		}
	case "comment":
		switch len(pos) {
		case 1:
			cmd.Doc = "report.md"
			cmd.Text = pos[0]
		case 2:
			cmd.Doc = pos[0]
			cmd.Text = pos[1]
		default:
			return nil, ExitUsage, usageError{msg: "comment requires [doc] <text>"}
		}
	case "new":
		if len(pos) != 1 {
			return nil, ExitUsage, usageError{msg: "new requires <title>"}
		}
		cmd.Title = pos[0]
	case "list":
		if len(pos) != 0 {
			return nil, ExitUsage, usageError{msg: "list does not accept positional arguments"}
		}
		if fs.Changed("limit") && fs.Changed("all") {
			return nil, ExitUsage, usageError{msg: "--all and --limit cannot be used together"}
		}
	case "activity", "whoami":
		if len(pos) != 0 {
			return nil, ExitUsage, usageError{msg: fmt.Sprintf("%s does not accept positional arguments", name)}
		}
	case "view":
		if len(pos) != 1 {
			return nil, ExitUsage, usageError{msg: "view requires <id-or-url>"}
		}
		cmd.ID = pos[0]
	case "clone":
		if len(pos) > 1 {
			return nil, ExitUsage, usageError{msg: "clone accepts at most one id or URL"}
		}
		if len(pos) == 1 {
			cmd.ID = pos[0]
		}
	case "login", "logout":
		if len(pos) > 1 {
			return nil, ExitUsage, usageError{msg: fmt.Sprintf("%s accepts at most one profile name", name)}
		}
		if len(pos) == 1 {
			cmd.ProfileName = pos[0]
		} else {
			cmd.ProfileName = "default"
		}
	case "profile":
		if len(pos) > 2 {
			return nil, ExitUsage, usageError{msg: "profile accepts [list|use] [name]"}
		}
		cmd.Action = "list"
		if len(pos) >= 1 {
			if pos[0] != "list" && pos[0] != "use" {
				return nil, ExitUsage, usageError{msg: fmt.Sprintf("invalid profile action %q (choose from list, use)", pos[0])}
			}
			cmd.Action = pos[0]
		}
		if len(pos) == 2 {
			cmd.ProfileName = pos[1]
		}
	}

	return cmd, ExitOK, nil
}

func Run(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	cmd, code, err := Parse(args, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return code
	}
	if cmd == nil {
		return code
	}

	if cmd.Name == "help" {
		if cmd.Topic == "" {
			_, _ = io.WriteString(stdout, help.Summary())
			return ExitOK
		}
		text, _ := help.Read(cmd.Topic)
		_, _ = io.WriteString(stdout, text)
		return ExitOK
	}

	_, _ = fmt.Fprintf(stderr, "%s: not yet implemented in Go build\n", cmd.Name)
	return ExitError
}
