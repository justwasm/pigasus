//go:build cgo

package voice

import (
	"errors"
	"sync"

	"github.com/gen2brain/malgo"
)

type malgoDriver struct {
	mu      sync.Mutex
	ctx     *malgo.AllocatedContext
	device  *malgo.Device
	onAudio func([]byte)
}

func initCaptureDriver(sampleRate int, onAudio func([]byte)) (captureDriver, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}

	drv := &malgoDriver{
		ctx:     ctx,
		onAudio: onAudio,
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.Alsa.NoMMap = 1

	callbacks := malgo.DeviceCallbacks{
		Data: func(pOutput, pInput []byte, framecount uint32) {
			if len(pInput) == 0 || drv.onAudio == nil {
				return
			}
			buf := make([]byte, len(pInput))
			copy(buf, pInput)
			drv.onAudio(buf)
		},
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, callbacks)
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, err
	}
	drv.device = device

	return drv, nil
}

func (d *malgoDriver) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.device == nil {
		return errors.New("malgo device not initialized")
	}
	return d.device.Start()
}

func (d *malgoDriver) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.device == nil {
		return nil
	}
	return d.device.Stop()
}

func (d *malgoDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error
	if d.device != nil {
		d.device.Uninit()
		d.device = nil
	}
	if d.ctx != nil {
		if err := d.ctx.Uninit(); err != nil {
			errs = append(errs, err)
		}
		d.ctx.Free()
		d.ctx = nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
