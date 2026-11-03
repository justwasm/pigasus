// result_stamp.go — the Created At / Completed At header every tool result
// carries.
//
// Cycle 379: the stamp lived with the file-writing tools, but four
// capabilities put it in front of their results — the file tools, the
// task tools, the web tools and the image generator. A header that four
// packages share belongs to the contract, not to one of them.

package tools

import (
	"time"
)

// ResultStamp renders the Created At / Completed At header the reference puts
// in front of every tool result (captured for run_command in cycle 275
// and for read_file / write_file / edit_file in cycle 293).
func ResultStamp(start time.Time) string {
	return "Created At: " + start.Format(time.RFC3339) + "\nCompleted At: " + time.Now().Format(time.RFC3339) + "\n"
}
