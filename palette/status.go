package palette

// StatusModeColor is the reference's per-mode colour on the status row's
// mode prefix (agy 1.5.12 truecolour capture): auto is Google Amber #F9AB00,
// plan is Google Blue #8AB4F8, accept-edits is Google Green #81C995.
// The default mode has no prefix.
func StatusModeColor(mode string) string {
	switch mode {
	case "auto":
		return "\x1b[38;2;249;171;0m" // #F9AB00
	case "plan":
		return "\x1b[38;2;138;180;248m" // #8AB4F8
	case "accept-edits":
		return "\x1b[38;2;129;201;149m" // #81C995
	}
	return DimGrayOpen
}
