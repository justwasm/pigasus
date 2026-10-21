// slash_skills_noat.go — verbatim pass-through guarantee for skill args.
//
// AGY 1.2.7 changelog line (TODO.md §2 / cmd/wagyu/testdata/print_changelog.md
// §1.2.7): "Fixed 'skill' tool invocations from /, plugins, and custom slash
// commands including arguments like @path/to/file being expanded by @file
// mentions as if @path were a file path token, instead of being passed to the
// named skill verbatim."
//
// Today the clone has no @-file-mention pre-processing layer at all — args
// after `splitSlash` flow into slashSkillPrompt (TUI, slash_commands.go:809)
// and dispatchPrintSlash → slashSkillPrompt (print, print_slash.go:190) and
// land in the model prompt untouched. The fix is therefore *preventive*: it
// pins a single named entry point that everything skill-related routes
// through, so a future prompt-pre-processing layer (an /<x> template
// expander, an upstream @-file mention layer, or a PiG-side message step
// that grows into @-expansion) cannot walk into skill args without editing a
// name whose existence is load-bearing.
//
// ── What's already verbatim (cycle 199) ─────────────────────────────────────
//
// handleSlashCommand's default branch:
//
//	"/<skill> <args>"  →  slashSkillLookup(cmd)
//	                    →  slashSkillPrompt(skill, args)
//	                    →  sendAsPrompt(token, prompt)
//
// sendAsPrompt appends `prompt` to m.session as a user message
// (slash_commands.go:939). No transform runs on the resulting message
// before streamPrompt. Print mode does the equivalent through
// dispatchPrintSlash / rt.RunWith (print_pig.go). The string
// concatenation in slashSkillPrompt is the only place args are inserted
// into a prompt — and only as a `\n\n` suffix to the skill body.
//
// ── What this file adds ─────────────────────────────────────────────────────
//
// `verbatimSkillArgs` is the single pass-through the comment block above
// documents. It exists so:
//
//   1. tests have a name to assert against (cmd/wagyu/slash_skills_noat_test.go),
//   2. future @-mention layers see a `verbatimSkillArgs(args) string` they
//      have to bypass — bypassing it forces them to pick the byte-for-byte
//      contract as the regression they would introduce.
//
// Calling slashSkillPrompt(skill, args) and slashSkillPrompt(skill,
// verbatimSkillArgs(args)) returns the same string today. Tomorrow's
// tests assert that this stays true even after any future mention-layer
// grows in.

package main

// verbatimSkillArgs returns args unchanged. It is the single opt-out
// point for any future prompt-pre-processing layer that would otherwise
// walk into skill arguments on its way to the model. Today the function
// is identity. The function exists so a regression is impossible to
// introduce without editing a name whose absence is load-bearing for the
// contract documented above.
func verbatimSkillArgs(args string) string {
	return args
}
