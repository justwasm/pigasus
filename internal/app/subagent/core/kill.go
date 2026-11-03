// kill.go — the manager's kill: the reference's "Kill Active Subagent".
//
// The reference's /agents panel kills the *active* subagent. Its
// keybinding table says "Instantly cancel (`CancelSubagent`) the
// highlighted running subagent" (official-docs/cli/commands/agents.md:115)
// and its "Common mistakes" table says the key "Only targets active
// (`running`) subagent processes" (same file).
//
// The policy therefore lives here, with the state it mutates: the
// manager owns the tracked subagents and their states, so the manager
// decides whether a kill is allowed. The panel only names the
// subagent.
//
// The kill itself is the subagent's own: Subagent.Kill terminates the
// process and moves it to the killed state, so the row keeps its place
// in the /agents panel and shows its final status — the reference's
// status list has "killed: Terminated manually by you or the parent
// process" (official-docs/cli/commands/agents.md:88).
//
// 【推测】 the reference's "and all its child threads" (the same
// document, line 99) is the descendant kill the reference's own
// `manage_subagents` kill does — its recovered message is "Successfully
// killed %d subagent(s) and their descendants.\nKilled roles: %s"
// (internal/app/subagenttools/manage.go, 【反编译】). A tracked
// subagent here is a child *process*; the daemon tracker kills its
// process group, so the process's own children go with it.

package core

// KillRunning terminates the named subagent when it is running.
//
// It answers whether the subagent was killed. A subagent that is not
// running — done, errored, killed, stopped or not started yet — is
// left alone: there is no active process to cancel, and the
// reference's key targets the running ones only.
//
// The killed subagent stays in the manager's list, so the /agents panel
// keeps showing the row with its final state. Its pending
// authorizations go with it: a killed subagent will never answer them.
func (m *Manager) KillRunning(id string) bool {
	if m == nil || id == "" {
		return false
	}
	sa, ok := m.Get(id)
	if !ok || sa == nil {
		return false
	}
	if sa.StatusLine() != StatusRunning {
		return false
	}
	// The kill moves the subagent to the killed state even when the
	// process is already gone, so the error is not the answer: the
	// answer is "a running subagent was killed".
	_ = sa.Kill()
	m.dropAuthorizations(id)
	return true
}
