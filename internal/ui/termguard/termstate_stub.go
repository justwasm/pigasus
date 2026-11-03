//go:build !(unix || windows)

package termguard

type termState struct{}

func captureState() *termState {
	return nil
}

func restoreState(s *termState) {
}
