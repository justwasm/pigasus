// parser.go — argument parsing flat helper for slash codesearch.
package codesearch

// ParseArgsFlat provides a flat-unpacked helper for callers needing separate returns.
func ParseArgsFlat(args string) (query string, literal bool, include, exclude []string, err error) {
	a, perr := ParseArgs(args)
	if perr != nil {
		if perr == ErrEmptyQuery {
			return "", false, nil, nil, ErrEmptyQuery
		}
		return "", false, nil, nil, perr
	}
	return a.Query, a.Literal, a.Include, a.Exclude, nil
}
