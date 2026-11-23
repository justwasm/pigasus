//go:build !unix

package perfrecorder

import "os"

func defaultDumpSignals() []os.Signal {
	return nil
}
