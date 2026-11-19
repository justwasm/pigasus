//go:build js

package voice

import (
	"sync"
	"sync/atomic"
	"syscall/js"
)

type jsCaptureDriver struct {
	mu          sync.Mutex
	sampleRate  int
	onAudio     func([]byte)
	running     atomic.Bool
	audioCtx    js.Value
	mediaStream js.Value
	sourceNode  js.Value
	scriptNode  js.Value
	processCb   js.Func
	feedChunkCb js.Func
}

func initCaptureDriver(sampleRate int, onAudio func([]byte)) (captureDriver, error) {
	drv := &jsCaptureDriver{
		sampleRate: sampleRate,
		onAudio:    onAudio,
	}
	drv.registerGlobalFeed()
	return drv, nil
}

func (d *jsCaptureDriver) registerGlobalFeed() {
	global := js.Global()
	if !global.Truthy() {
		return
	}

	d.feedChunkCb = js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		val := args[0]

		// If ArrayBuffer passed, wrap in Uint8Array
		if val.InstanceOf(global.Get("ArrayBuffer")) {
			val = global.Get("Uint8Array").New(val)
		}

		if val.InstanceOf(global.Get("Uint8Array")) {
			length := val.Get("byteLength").Int()
			if length > 0 {
				buf := make([]byte, length)
				js.CopyBytesToGo(buf, val)
				if d.onAudio != nil {
					d.onAudio(buf)
				}
			}
			return nil
		}

		if val.InstanceOf(global.Get("Float32Array")) {
			length := val.Get("length").Int()
			if length > 0 {
				pcm := make([]byte, length*2)
				for i := 0; i < length; i++ {
					s := float32(val.Index(i).Float())
					if s > 1.0 {
						s = 1.0
					} else if s < -1.0 {
						s = -1.0
					}
					var v int16
					if s < 0 {
						v = int16(s * 32768.0)
					} else {
						v = int16(s * 32767.0)
					}
					pcm[i*2] = byte(v)
					pcm[i*2+1] = byte(v >> 8)
				}
				if d.onAudio != nil {
					d.onAudio(pcm)
				}
			}
			return nil
		}

		return nil
	})

	global.Set("piggaFeedAudioChunk", d.feedChunkCb)
	global.Set("openagyFeedAudio", d.feedChunkCb)
}

func (d *jsCaptureDriver) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running.Load() {
		return nil
	}
	d.running.Store(true)

	global := js.Global()
	if !global.Truthy() {
		return nil
	}

	navigator := global.Get("navigator")
	if !navigator.Truthy() {
		return nil
	}
	mediaDevices := navigator.Get("mediaDevices")
	if !mediaDevices.Truthy() {
		return nil
	}

	constraints := global.Get("Object").New()
	audioConstraints := global.Get("Object").New()
	audioConstraints.Set("channelCount", 1)
	audioConstraints.Set("echoCancellation", true)
	audioConstraints.Set("noiseSuppression", true)
	audioConstraints.Set("autoGainControl", true)
	if d.sampleRate > 0 {
		audioConstraints.Set("sampleRate", d.sampleRate)
	}
	constraints.Set("audio", audioConstraints)

	promise := mediaDevices.Call("getUserMedia", constraints)

	var successCb, errorCb js.Func

	successCb = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer successCb.Release()
		defer errorCb.Release()

		d.mu.Lock()
		defer d.mu.Unlock()

		if !d.running.Load() {
			if len(args) > 0 && args[0].Truthy() {
				tracks := args[0].Call("getTracks")
				length := tracks.Get("length").Int()
				for i := 0; i < length; i++ {
					tracks.Index(i).Call("stop")
				}
			}
			return nil
		}

		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		stream := args[0]
		d.mediaStream = stream

		audioCtxCtor := global.Get("AudioContext")
		if !audioCtxCtor.Truthy() {
			audioCtxCtor = global.Get("webkitAudioContext")
		}
		if !audioCtxCtor.Truthy() {
			return nil
		}

		var audioCtx js.Value
		if d.sampleRate > 0 {
			ctxOpts := global.Get("Object").New()
			ctxOpts.Set("sampleRate", d.sampleRate)
			// Some browsers throw if constructor options contain custom sampleRate
			audioCtx = audioCtxCtor.New(ctxOpts)
		} else {
			audioCtx = audioCtxCtor.New()
		}
		d.audioCtx = audioCtx

		if audioCtx.Get("state").String() == "suspended" {
			audioCtx.Call("resume")
		}

		source := audioCtx.Call("createMediaStreamSource", stream)
		d.sourceNode = source

		bufferSize := 4096
		scriptProcessor := audioCtx.Call("createScriptProcessor", bufferSize, 1, 1)
		d.scriptNode = scriptProcessor

		ctxSampleRate := audioCtx.Get("sampleRate").Int()
		targetRate := d.sampleRate
		if targetRate <= 0 {
			targetRate = 16000
		}

		d.processCb = js.FuncOf(func(pThis js.Value, pArgs []js.Value) any {
			if !d.running.Load() {
				return nil
			}
			if len(pArgs) == 0 || !pArgs[0].Truthy() {
				return nil
			}
			event := pArgs[0]
			inputBuffer := event.Get("inputBuffer")
			if !inputBuffer.Truthy() {
				return nil
			}
			channelData := inputBuffer.Call("getChannelData", 0)
			numSamples := channelData.Get("length").Int()
			if numSamples == 0 {
				return nil
			}

			floatSamples := make([]float32, numSamples)
			for i := 0; i < numSamples; i++ {
				floatSamples[i] = float32(channelData.Index(i).Float())
			}

			resampled := resampleFloat32(floatSamples, ctxSampleRate, targetRate)

			pcm := make([]byte, len(resampled)*2)
			for i, sample := range resampled {
				if sample > 1.0 {
					sample = 1.0
				} else if sample < -1.0 {
					sample = -1.0
				}
				var val int16
				if sample < 0 {
					val = int16(sample * 32768.0)
				} else {
					val = int16(sample * 32767.0)
				}
				pcm[i*2] = byte(val)
				pcm[i*2+1] = byte(val >> 8)
			}

			if d.onAudio != nil && len(pcm) > 0 {
				d.onAudio(pcm)
			}
			return nil
		})

		scriptProcessor.Set("onaudioprocess", d.processCb)
		source.Call("connect", scriptProcessor)
		scriptProcessor.Call("connect", audioCtx.Get("destination"))
		return nil
	})

	errorCb = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer successCb.Release()
		defer errorCb.Release()
		return nil
	})

	promise.Call("then", successCb).Call("catch", errorCb)
	return nil
}

func (d *jsCaptureDriver) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.running.Store(false)

	if d.scriptNode.Truthy() {
		d.scriptNode.Set("onaudioprocess", js.Null())
		d.scriptNode.Call("disconnect")
		d.scriptNode = js.Null()
	}
	if d.sourceNode.Truthy() {
		d.sourceNode.Call("disconnect")
		d.sourceNode = js.Null()
	}
	if d.processCb.Truthy() {
		d.processCb.Release()
	}
	if d.mediaStream.Truthy() {
		tracks := d.mediaStream.Call("getTracks")
		length := tracks.Get("length").Int()
		for i := 0; i < length; i++ {
			tracks.Index(i).Call("stop")
		}
		d.mediaStream = js.Null()
	}
	if d.audioCtx.Truthy() {
		if d.audioCtx.Get("state").String() != "closed" {
			d.audioCtx.Call("close")
		}
		d.audioCtx = js.Null()
	}
	return nil
}

func (d *jsCaptureDriver) Close() error {
	_ = d.Stop()
	if d.feedChunkCb.Truthy() {
		d.feedChunkCb.Release()
	}
	return nil
}

func resampleFloat32(input []float32, inRate, outRate int) []float32 {
	if inRate <= 0 || outRate <= 0 || inRate == outRate || len(input) == 0 {
		return input
	}
	ratio := float64(inRate) / float64(outRate)
	outLen := int(float64(len(input)) / ratio)
	if outLen <= 0 {
		return nil
	}
	out := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := float32(srcPos - float64(srcIdx))
		if srcIdx+1 < len(input) {
			out[i] = input[srcIdx]*(1.0-frac) + input[srcIdx+1]*frac
		} else if srcIdx < len(input) {
			out[i] = input[srcIdx]
		}
	}
	return out
}
