//go:build !cgo

package voice

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sync"
)

type processDriver struct {
	mu         sync.Mutex
	sampleRate int
	onAudio    func([]byte)
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	done       chan struct{}
}

func initCaptureDriver(sampleRate int, onAudio func([]byte)) (captureDriver, error) {
	return &processDriver{
		sampleRate: sampleRate,
		onAudio:    onAudio,
		done:       make(chan struct{}),
	}, nil
}

func (d *processDriver) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if ffmpeg is installed
	ffmpegPath, _ := exec.LookPath("ffmpeg")
	if ffmpegPath == "" {
		for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg"} {
			if _, statErr := os.Stat(p); statErr == nil {
				ffmpegPath = p
				break
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && ffmpegPath != "" {
		// macOS AVFoundation default microphone capture
		cmd = exec.CommandContext(ctx, ffmpegPath,
			"-loglevel", "quiet",
			"-f", "avfoundation",
			"-i", ":default",
			"-ar", "16000",
			"-ac", "1",
			"-f", "s16le",
			"pipe:1",
		)
	} else if runtime.GOOS == "linux" || runtime.GOOS == "android" {
		if ffmpegPath != "" {
			// Linux PulseAudio / ALSA or Android Termux PulseAudio via ffmpeg
			cmd = exec.CommandContext(ctx, ffmpegPath,
				"-loglevel", "quiet",
				"-f", "pulse",
				"-i", "default",
				"-ar", "16000",
				"-ac", "1",
				"-f", "s16le",
				"pipe:1",
			)
		} else if parecPath, err := exec.LookPath("parec"); err == nil {
			// Linux / Android Termux native parec capture
			cmd = exec.CommandContext(ctx, parecPath,
				"--rate=16000",
				"--channels=1",
				"--format=s16le",
			)
		}
	}

	if cmd == nil {
		cancel()
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil
	}
	d.cmd = cmd
	d.done = make(chan struct{})

	go func() {
		defer close(d.done)
		buf := make([]byte, 1024) // 32ms chunk at 16kHz 16-bit mono
		for {
			n, err := stdout.Read(buf)
			if n > 0 && d.onAudio != nil {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				d.onAudio(chunk)
			}
			if err != nil {
				return
			}
		}
	}()

	return nil
}

func (d *processDriver) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	if d.cmd != nil {
		_ = d.cmd.Wait()
		d.cmd = nil
	}
	return nil
}

func (d *processDriver) Close() error {
	return d.Stop()
}
