package agent

import "fmt"

// InterruptedHintFormat is the reference's own hint for a turn the user
// cancelled with Esc.
//
// 【反编译】 agy 1.4.8 — `cli/model/model.(*PromptModel).interruptedLine`
// (0x102bb48b0) formats exactly this literal. The format string is 48
// bytes long in the binary (the `fmt.Sprintf` length argument is 0x30) and
// lives at 0x103617a41:
//
//	  ⎿  Interrupted · What should %s do instead?
//
// The same function returns a second literal when the Esc cancelled a
// *compaction* instead of a turn — "  ⎿  Compaction cancelled. Conversation
// was not compacted." (0x103647b2e).
//
// The reference renders this line inside the composer (`PromptModel.View`,
// 0x102bb49f0, calls `IsInterrupted` and then `interruptedLine`), not in
// the transcript: a cancelled turn leaves no error row behind.
const InterruptedHintFormat = "  ⎿  Interrupted · What should %s do instead?"

// InterruptedHintLine renders the reference's interrupted hint for the
// given CLI name.
//
// The reference's argument is the name of the CLI the user is talking to:
// it passes "Jetski CLI" when the binary carries the internal "jetski"
// product name and "Antigravity CLI" otherwise. The clone passes its own
// brand's CLI name, which is the same string under the reference's brand.
func InterruptedHintLine(cliName string) string {
	if cliName == "" {
		cliName = "Antigravity CLI"
	}
	return fmt.Sprintf(InterruptedHintFormat, cliName)
}
