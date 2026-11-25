package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/youthesame/elport/internal/help"
)

var Version = ""

var (
	entities = []string{"experiments", "items"}
	formats  = []string{"markdown", "html"}
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
	Name          string
	Profile       string
	Entity        string
	Docs          []string
	Doc           string
	Text          string
	ID            string
	Title         string
	Output        string
	Topic         string
	Action        string
	ProfileName   string
	Scope         string
	Query         string
	Tags          []string
	Order         string
	Sort          string
	Limit         int
	All           bool
	HasAll        bool
	AllRoot       string
	Offset        int
	JSON          bool
	Team          bool
	DryRun        bool
	Force         bool
	Yes           bool
	ConfirmRemote string
	Format        string
	Base          bool
	Resolved      bool
}

type usageError struct {
	msg string
}

func (e usageError) Error() string {
	return e.msg
}

type choiceValue struct {
	target   *string
	flagName string
	allowed  []string
}

func (c *choiceValue) String() string {
	return *c.target
}

func (c *choiceValue) Set(v string) error {
	if !slices.Contains(c.allowed, v) {
		return fmt.Errorf("invalid %s %q (choose from %s)", c.flagName, v, strings.Join(c.allowed, ", "))
	}
	*c.target = v
	return nil
}

func (c *choiceValue) Type() string {
	return "string"
}

type decimalIntValue struct {
	target *int
}

func (d *decimalIntValue) String() string {
	return strconv.Itoa(*d.target)
}

func (d *decimalIntValue) Set(v string) error {
	parsed, err := strconv.ParseInt(v, 10, 0)
	if err != nil {
		return fmt.Errorf("invalid integer %q", v)
	}
	*d.target = int(parsed)
	return nil
}

func (d *decimalIntValue) Type() string {
	return "int"
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
		"side effects:\n" +
		"  read-only       list, view (without -o), activity, comments, status, diff,\n" +
		"                  whoami, profile list, push -n\n" +
		"  local writes    pull, fetch, clone, merge, view -o DIR, login, logout,\n" +
		"                  profile use\n" +
		"  remote writes   push (without -n; also writes locally),\n" +
		"                  new (also writes locally), comment\n\n" +
		"defaults & auth:\n" +
		"  document commands default to report.md (sync commands also accept directories)\n" +
		"  exit codes: 0 success, 1 failure, 2 usage error\n" +
		"  non-interactive auth: set both ELABFTW_BASE_URL and ELABFTW_API_KEY\n\n" +
		help.Summary()
}

func subcommandUsage(name string) string {
	switch name {
	case "help":
		return "usage: elport help [agents|workflow|documents]"
	case "push", "pull", "fetch", "status", "diff":
		return fmt.Sprintf("usage: elport %s [options] [doc ...]", name)
	case "comments", "merge":
		return fmt.Sprintf("usage: elport %s [options] [doc]", name)
	case "comment":
		return "usage: elport comment [options] [doc] <text>"
	case "new":
		return "usage: elport new [options] <title>"
	case "view":
		return "usage: elport view [options] <ID_OR_URL>"
	case "clone":
		return "usage: elport clone [options] [ID_OR_URL | --all ROOT]"
	case "login", "logout":
		return fmt.Sprintf("usage: elport %s [profile]", name)
	case "profile":
		return "usage: elport profile [list|use] [name]"
	default:
		return fmt.Sprintf("usage: elport %s [options]", name)
	}
}

func newFlagSet(name string) *pflag.FlagSet {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.SortFlags = false
	return fs
}

func addChoiceFlag(fs *pflag.FlagSet, target *string, name, defaultVal, usage string, allowed []string) {
	*target = defaultVal
	fs.Var(&choiceValue{target: target, flagName: "--" + name, allowed: allowed}, name, usage)
}

func addDecimalIntFlag(fs *pflag.FlagSet, target *int, name string, defaultVal int, usage string) {
	*target = defaultVal
	fs.Var(&decimalIntValue{target: target}, name, usage)
}

func addTargetFlags(fs *pflag.FlagSet, cmd *Command) {
	fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	addChoiceFlag(fs, &cmd.Entity, "entity", "", "target entity type (default: experiments)", entities)
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
		if showVersion {
			_, _ = fmt.Fprintf(stdout, "elport %s\n", versionString())
			return nil, ExitOK, nil
		}
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
		fs.StringVar(&cmd.ConfirmRemote, "confirm-remote", "", "confirm overwriting a reviewed remote HTML body by SHA-256 prefix (12+ hex chars)")
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
		addChoiceFlag(fs, &cmd.Entity, "entity", "experiments", "entity type to create (default: experiments)", entities)
		addChoiceFlag(fs, &cmd.Format, "format", "", "publish format: markdown or html (default: markdown)", formats)
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
		fs.StringVarP(&cmd.Output, "output", "o", "report.md", "local document to create (default: report.md)")
	case "list":
		addChoiceFlag(fs, &cmd.Entity, "entity", "", "entity type to list (default: experiments)", entities)
		addChoiceFlag(fs, &cmd.Scope, "scope", "self", "whose entities to list: self, team, all (default: self)", scopes)
		fs.StringVarP(&cmd.Query, "query", "q", "", "search term")
		fs.StringArrayVar(&cmd.Tags, "tag", nil, "only entities with this tag (repeat to require several)")
		addChoiceFlag(fs, &cmd.Order, "order", "", "sort by this field", orders)
		addChoiceFlag(fs, &cmd.Sort, "sort", "", "sort direction (default: desc)", sorts)
		addDecimalIntFlag(fs, &cmd.Limit, "limit", 50, "maximum number of results (default: 50)")
		fs.BoolVar(&cmd.All, "all", false, "page through every result")
		addDecimalIntFlag(fs, &cmd.Offset, "offset", 0, "skip this many results")
		fs.BoolVar(&cmd.JSON, "json", false, "emit the raw API rows as JSON")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "activity":
		fs.BoolVar(&cmd.Team, "team", false, "one heatmap per author in your team")
		addChoiceFlag(fs, &cmd.Entity, "entity", "", "entity type to count (default: experiments)", entities)
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "view":
		addChoiceFlag(fs, &cmd.Entity, "entity", "", "entity type to view (default: experiments)", entities)
		fs.StringVarP(&cmd.Output, "output", "o", "", "write body and attachments into DIR")
		fs.StringVar(&cmd.Profile, "profile", "", "config profile to use")
	case "clone":
		fs.StringVar(&cmd.AllRoot, "all", "", "clone every entity not yet under ROOT")
		addChoiceFlag(fs, &cmd.Entity, "entity", "", "entity type to clone (default: experiments)", entities)
		addChoiceFlag(fs, &cmd.Scope, "scope", "self", "whose entities --all clones (default: self)", scopes)
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
			_, _ = fmt.Fprintf(stdout, "%s\n\n%s", subcommandUsage(name), fs.FlagUsages())
			return nil, ExitOK, nil
		}
		return nil, ExitUsage, usageError{msg: err.Error()}
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
		cmd.HasAll = fs.Changed("all")
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
