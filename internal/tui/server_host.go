package tui

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentserver"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// ---- server_host.go ----

// Package cliapp — server_host.go: the `pigasus server` subcommand.
//
// The server is the host half of the client/server split: it owns the
// workspace, the tools and the credentials, and it serves the turns a
// client asks for over a websocket.
//
// The server always demands a bearer token. It runs shell commands
// against its own workspace, so an unauthenticated peer is a peer with
// the machine's shell — the token is the only thing between them. A
// token is minted when the command line does not name one, and it is
// printed when the server starts.
//
// The bind address is the server's own fact: the loopback on a fixed
// port, because the server must not listen on the network unless the
// user asks for it. The host supplies the address, the host and the
// port the user gave; the server decides what they mean
// (internal/app/agentserver).

// serverFlagSet builds the flag set the server's own options live in.
//
// The server is a command of its own, but the transport and the agent
// are resolved through the same flag names the root command uses, so
// the same helpers read them.
func serverFlagSet(args cli.ServerArgs) *flag.FlagSet {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.String("backend", strings.TrimSpace(args.Backend), "")
	fs.String("agent", strings.TrimSpace(args.Agent), "")
	return fs
}

// runServer hosts the agent and serves it over a websocket.
func runServer(j *cli.JetskiCLI, args cli.ServerArgs) error {
	addr := agentserver.ResolveAddr(args.Addr, args.Host, args.Port)

	workspace := strings.TrimSpace(args.Workspace)
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("server: cannot read the working directory: %w", err)
		}
		workspace = wd
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("server: cannot resolve %q: %w", workspace, err)
	}

	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = defaultModel
	}

	out := io.Writer(os.Stdout)
	if j != nil && j.Out != nil {
		out = j.Out
	}

	fs := serverFlagSet(args)
	srv, err := agentserver.New(agentserver.Config{
		Addr:            addr,
		Token:           strings.TrimSpace(args.Token),
		Workspace:       abs,
		Model:           model,
		Effort:          strings.TrimSpace(args.Effort),
		PermissionMode:  strings.TrimSpace(args.Mode),
		SkipPermissions: args.SkipPermissions,
		AddDirs:         args.AddDirs,
		ACPVersion:      strings.TrimSpace(args.ACPVersion),
		AgentBody:       agents.AgentBody(agents.CustomAgentFromFlags(fs)),
		BackendFor:      func(name string) backend.Client { return backendselect.Pick(fs, name, modelpicker.ResolveCatalogueID) },
		Log:             out,
	})
	if err != nil {
		return err
	}

	ln, err := srv.Listen()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "pigasus server listening on ws://%s\n", srv.Addr())
	fmt.Fprintf(out, "protocols: acp (ws://%s/acp, default), ahp (ws://%s/ahp), agentlink (ws://%s/, legacy)\n", srv.Addr(), srv.Addr(), srv.Addr())
	fmt.Fprintf(out, "workspace: %s\n", abs)
	fmt.Fprintf(out, "model: %s\n", model)
	fmt.Fprintf(out, "token: %s\n", srv.Token())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Serve(ctx, ln)
}

// ---- account_identity.go ----

// account_identity.go — cycle multi-account-switch: keeping the
// session's account identity in step with the shared credential.
//
// # Why this exists
//
// The credential is shared: the clone, the `agy accounts` command and
// the installed `agy` all read the same macOS keychain entry (and the
// same on-disk copies). A switch made *outside* the TUI — the CLI
// command, a sign-in in another terminal — must still reach the running
// session, so the session re-reads the credential when it can matter:
// before a model turn and when a slash command opens a panel.
//
// The reference caches its credential in memory for the whole session,
// so an outside switch only reaches it on its next token refresh (or on
// the next start-up). The clone re-reads it instead — the superset the
// /accounts panel promises.

// syncAccountIdentity re-reads the shared credential and updates the
// session's account identity when it changed outside the TUI.
//
// It returns a command that refreshes the plan tier when the account
// changed, and nil when nothing changed. The credential read is the
// same one auth.LoadTokenUnified does — the login keychain first, the
// on-disk copies after it.
func (m *runMode) syncAccountIdentity() tea.Cmd {
	_, email := accounts.Current()
	if email == "" || email == m.session.email {
		return nil
	}
	m.session.email = email
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	// The transport caches the keychain token it read, so it has to be
	// rebuilt for the session to talk as the new account.
	if m.engine.backendFor != nil {
		if b := m.engine.backendFor(m.session.model); b != nil {
			m.engine.backend = b
		}
	}
	return m.fetchPlanTierCmd()
}

// ---- merged from account_quota.go ----
// account_quota.go — cycle accounts-tier-usage: the per-account quota
// and tier lookups the /accounts panel shows.
//
// # What is queried
//
// Two answers, both per *account* and both read from the server:
//
//   - the plan tier (loadCodeAssist's paidTier.name — "Google AI Pro",
//     "Antigravity Starter Quota");
//   - the quota summary (retrieveUserQuotaSummary — the model groups
//     with their weekly and five-hour buckets).
//
// The tier is queried for the *active* account only: it is what the
// session's banner shows, so one query per session is enough. The
// answer is cached in the registry, so every account that has been
// active once keeps its tier.
//
// The quota is queried for every account, because that is what the
// panel shows. It is fetched one account at a time — a registry with
// five accounts must not fire five refresh-token exchanges at once —
// and the answer is cached in the registry, so opening the panel twice
// in a row costs one fetch and the /usage panel reuses it.
//
// # The credential each query uses
//
// The quota belongs to the account behind a credential, so each account
// is queried with its own access token. The active account uses the
// shared credential (the one the session talks with); an inactive
// account uses its own stored token, refreshed quietly — a refresh must
// not write the shared credential (that would switch the active
// account) and must not clear it (that would sign the user out).

// accountsQuotaMsg carries one account's quota fetch result.
type accountsQuotaMsg struct {
	email   string
	summary *auth.QuotaSummary
	err     error
}

// fetchAccountQuotaCmd fetches one account's quota summary with that
// account's own credential.
//
// The fetch writes the snapshot into the registry, so the /usage panel
// and the status line read the same numbers back instead of querying
// the same account a second time.
//
// The budget is the registry's (accounts.QuotaTimeout): the call can be
// two round trips — a token refresh (the OAuth endpoint measured 9 s
// from this machine) and the quota call itself (6.9 s). A shorter
// budget turned a slow but successful fetch into a timeout.
func fetchAccountQuotaCmd(email string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), accounts.QuotaTimeout)
		defer cancel()
		summary, err := accounts.FetchQuota(ctx, email)
		return accountsQuotaMsg{email: email, summary: summary, err: err}
	}
}

// ---- the quota cache -------------------------------------------------

// cachedQuota returns the account's cached quota summary when the
// registry's snapshot is still fresh enough to render.
//
// The registry owns the snapshot: the /accounts panel's fetch writes it
// and the /usage panel and the status line read it back, so the same
// account is never queried twice. The freshness rule is the registry's
// (accounts.QuotaFreshness), not the host's.
//
// The method is the session's seam to the registry. Its callers live in
// files this tranche does not own, so the seam stays here until they
// name accounts.FreshQuota themselves.
func (m *runMode) cachedQuota(email string) *auth.QuotaSummary {
	if m == nil || email == "" {
		return nil
	}
	return accounts.FreshQuota(email)
}

// cacheQuota stores an account's quota summary in the registry.
//
// The registry owns the snapshot: the /usage panel's answer is written
// there, so the status line reads the same numbers back and the /usage
// panel does not query the same account a second time.
//
// The registry keeps the snapshot, so the write is the registry's and
// not the session's: a second session reads the same numbers.
func (m *runMode) cacheQuota(email string, summary *auth.QuotaSummary) {
	if m == nil || email == "" || summary == nil {
		return
	}
	_ = accounts.SetQuota(email, summary.Snapshot())
}
