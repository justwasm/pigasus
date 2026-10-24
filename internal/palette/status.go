package palette

// StatusModeColor is the reference's per-mode colour on the status row's
// mode prefix (agy 1.4.3, truecolour capture): auto is Solarized orange,
// plan the blue, accept-edits the green. The default mode has no prefix.
func StatusModeColor(mode string) string {
	switch mode {
	case "auto":
		return "\x1b[38;2;203;75;22m" // #CB4B16
	case "plan":
		return BlueOpen // #268BD2
	case "accept-edits":
		return "\x1b[38;2;133;153;0m" // #859900
	}
	return DimGrayOpen
}
