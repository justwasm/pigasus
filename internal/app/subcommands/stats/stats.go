package stats

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	featstats "github.com/justwasm/pigasus/internal/features/stats"
)

// Spec returns the subcommand specification for `pigasus stats`.
func Spec() cliflags.SubcommandSpec {
	return cliflags.SubcommandSpec{
		Name:    "stats",
		Aliases: []string{"stat"},
		Short:   "Show usage statistics and analytics",
		Hidden:  true,
		HelpFn:  Usage,
		Run:     Run,
	}
}

// Usage returns the help message for `pigasus stats`.
func Usage(prog string) string {
	return fmt.Sprintf(`Usage of %s stats:

  %s stats [flags]

Flags:
      --all           Aggregate stats from all known projects
  -c, --cwd <dir>     Current working directory to filter stats for
  -D, --data-dir <dir> Root store directory (default ~/.gemini/antigravity-cli)
      --crawl-dir <dir> Crawl a directory recursively for all sessions/projects
  -h, --help          Show help for stats subcommand
`, prog, prog)
}

// Run executes the stats subcommand.
func Run(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(w)

	var (
		all      bool
		cwd      string
		dataDir  string
		crawlDir string
		help     bool
	)

	fs.BoolVar(&all, "all", false, "Aggregate stats from all known projects")
	fs.StringVar(&cwd, "cwd", "", "Current working directory to filter stats for")
	fs.StringVar(&cwd, "c", "", "Current working directory to filter stats for (short)")
	fs.StringVar(&dataDir, "data-dir", "", "Root store directory (default ~/.gemini/antigravity-cli)")
	fs.StringVar(&dataDir, "D", "", "Root store directory (short)")
	fs.StringVar(&crawlDir, "crawl-dir", "", "Crawl a directory recursively for all sessions/projects")
	fs.BoolVar(&help, "help", false, "Show help for stats subcommand")
	fs.BoolVar(&help, "h", false, "Show help for stats subcommand (short)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if help {
		fmt.Fprint(w, Usage("pigasus"))
		return nil
	}

	if cwd == "" {
		if dir, err := os.Getwd(); err == nil {
			cwd = dir
		}
	}

	var projName string
	switch {
	case crawlDir != "":
		projName = crawlDir
	case all:
		projName = "all projects"
	default:
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
			projName = "~" + strings.TrimPrefix(cwd, home)
		} else {
			projName = filepath.Base(cwd)
		}
	}

	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	if username == "" {
		username = "user"
	}

	opts := featstats.CollectOptions{
		DataDir:  dataDir,
		CrawlDir: crawlDir,
		Cwd:      cwd,
		All:      all,
	}

	statsData, projectStats, err := featstats.Collect(opts)
	if err != nil {
		return fmt.Errorf("collect stats: %w", err)
	}

	if statsData == nil || statsData.Total.TotalSessions == 0 {
		fmt.Fprintln(w, "ERROR")
		fmt.Fprintln(w, "No data available: no sessions found in database.")
		return errors.New("no data available: no sessions found in database")
	}

	htmlPath, err := featstats.WriteTempHTML(statsData, projectStats, projName, username)
	if err != nil {
		return fmt.Errorf("generate html: %w", err)
	}

	fmt.Fprintf(w, "Stats generated: %s\n", htmlPath)
	fmt.Fprintf(w, "Total Sessions: %d | Total Messages: %d | Total Tokens: %d | Estimated Cost: $%.2f\n",
		statsData.Total.TotalSessions,
		statsData.Total.TotalMessages,
		statsData.Total.TotalTokens,
		statsData.Total.TotalCost,
	)

	openBrowser(htmlPath)
	return nil
}

func openBrowser(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}

func init() {
	cliflags.RegisterProvider("stats", Spec)
	subcommands.RegisterProvider(Spec)
}
