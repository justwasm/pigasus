// Package rchost — instance_name.go: the daemon-side watcher that
// pushes a rename to the cloud without dropping the tunnel.
//
// # Why a watcher and not a reconnect
//
// The reference's own language server does not restart the tunnel
// when the instance's name changes. It watches the configuration, and
// when the name changes it renames the instance over the API:
//
//	[RemoteControl] Hostname changed from %q to %q, updating via API
//	[RemoteControl] Failed to update instance metadata: %v
//
// 【反编译】 the caller is language_server.(*Server).
// updateRemoteControlConnection.func1 at 0x1026a6020 in the 1.4.11
// binary; it calls remotecontrol.UpdateInstanceMetadata at
// 0x1025aad50, which posts to "{proxyURL}/v1internal:updateInstance"
// with the body
//
//	{"instanceId":"…","metadata":{"name":"…"},"updateMask":"metadata.name"}
//
// and then updates the server's cached metadata under a mutex.
//
// 【反编译】 the reference's config is the source of truth: the CLI
// side writes the name into the config proto
// (jetski/cli/remotecontrol.(*Daemon).SetInstanceName at 0x102d35320
// → config.ApplyProtoUpdate), and the language server reads it back.
//
// So the clone does the same: the name lives in
// ~/.gemini/config/config.json under userSettings.cliRemoteControlHostname,
// the daemon polls it, and a change travels over the API.
//
// # The reference's own wording
//
// The reference's Web UI says "Changing it reconnects" — that is the
// *user-facing* consequence of the change reaching the dashboard, not
// the mechanism. The mechanism the reference ships is the API call
// above, and it is the one the clone uses: the tunnel session stays
// up.
//
// # What the watcher watches
//
// The *configured* name — the value of
// userSettings.cliRemoteControlHostname — not the resolved one.
//
// The resolved name (persistentrc.ResolveInstanceName) falls back to
// the machine's host name when the configuration carries none, and
// that fallback would look like a rename the user never made: the
// watcher would see a name it did not write and push it. The
// configured name is the one the user writes, so a change in it is a
// rename and nothing else.
//
// A configured name that is empty is skipped — the reference refuses
// an empty hostname outright ("[RemoteControl] Cannot update hostname
// to empty. Rejecting update.").
package rchost

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rctunnel"
)

// instanceNamePollInterval is how often the daemon looks at the
// configuration file.
//
// The file is a few kilobytes and the read is a stat plus a parse, so
// the interval is short enough that a rename shows up on the dashboard
// while the user is still looking at it, and long enough that the
// daemon is not a busy loop.
//
// It is a variable so a test can shorten it; nothing else writes it.
var instanceNamePollInterval = 2 * time.Second

// watchInstanceName polls the configured instance name and pushes a
// rename to the cloud over the tunnel session that is already up.
//
// The watcher owns no state beyond the last name it saw: the
// configuration file is the source of truth, and the tunnel's own
// registration name is the baseline.
//
// baseline is the configured name at the moment the daemon started.
// It is passed in rather than read here so that the baseline is
// captured *before* the watcher begins — a name written between the
// daemon's start-up and the watcher's first tick would otherwise look
// like a rename the watcher had already seen.
//
// A failed push is not fatal: the next tick tries again, because the
// configuration still carries the new name.
func watchInstanceName(ctx context.Context, client *rctunnel.Client, errOut io.Writer, label, baseline string) {
	if client == nil {
		return
	}
	if label == "" {
		label = "remote-control serve"
	}
	last := baseline
	lastPushed := client.InstanceName()

	ticker := time.NewTicker(instanceNamePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		configured := persistentrc.ConfiguredInstanceName()
		if configured == "" || configured == last {
			continue
		}
		last = configured

		// The name the dashboard shows is the resolved one — the
		// clone's own brand prefix and all.
		name := persistentrc.ResolveInstanceName(configured)
		if name == "" || name == lastPushed {
			continue
		}
		fmt.Fprintf(errOut, "%s: [RemoteControl] Hostname changed from %q to %q, updating via API\n",
			label, lastPushed, name)
		if err := client.UpdateInstanceName(ctx, name); err != nil {
			fmt.Fprintf(errOut, "%s: [RemoteControl] Failed to update instance metadata: %v\n", label, err)
			continue
		}
		// The tunnel re-registers on every reconnect, and the
		// re-registration carries the name it holds — so a successful
		// rename has to move it, or the next reconnect would put the
		// old name back.
		client.SetInstanceName(name)
		lastPushed = name
	}
}
