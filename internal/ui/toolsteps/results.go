// results.go — the raw tool outputs a set of steps carries.
//
// # Where the fact lives
//
// A tool step's Result is the raw output the wire carried in
// functionResponse.response.output (cycle 292); a thought step has
// none. Which of a set of steps are tool steps is this package's
// business — it owns the Step vocabulary — so the collection lives here
// and not in the host that gathers the steps.
//
// The token arithmetic over the collected outputs belongs to the panel
// that shows them (internal/ui/views/contextpanel); this package only
// says which outputs there are.

package toolsteps

// ToolResults returns the raw tool outputs of the given steps.
//
// A thought step carries no output, so it contributes nothing; a tool
// step that is still running carries an empty one, which the token
// count reads as zero.
func ToolResults(steps []Step) []string {
	var out []string
	for _, s := range steps {
		if s.Kind == KindTool {
			out = append(out, s.Result)
		}
	}
	return out
}
