package hostactions

import (
	"fmt"
	"strconv"

	"github.com/justwasm/pigasus/internal/core/slashparse"
)

// ResolveCopy implements /copy and /copy <n>. It returns the text to
// put on the clipboard ("" when there is nothing) and the line to
// show.
//
// Indexing is 1-based from the most recent response.
func ResolveCopy(args string, responses []string) (string, string) {
	args = slashparse.Trim(args)
	if args == "" {
		if len(responses) == 0 {
			return "", "No response to copy."
		}
		return responses[len(responses)-1], "Copied last response to clipboard."
	}
	n, err := strconv.Atoi(args)
	if err != nil || n < 1 || n > len(responses) {
		if err != nil || n < 1 {
			// Not an index at all: behave like the bare form.
			if len(responses) == 0 {
				return "", "No response to copy."
			}
			return responses[len(responses)-1], "Copied last response to clipboard."
		}
		return "", "No response to copy."
	}
	return responses[len(responses)-n], fmt.Sprintf("Copied response %d to clipboard.", n)
}
