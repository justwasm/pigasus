/**
 * OpenAGY Full-Duplex Live Voice Web Client
 *
 * Implements browser microphone streaming (16kHz PCM), 24kHz PCM playback
 * with natural barge-in / interruption clearing, and WebSocket communication
 * using the Jetski voice protocol ('jetski-voice-proto').
 */

(function (root, factory) {
  if (typeof define === 'function' && define.amd) {
    define([], factory);
  } else if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    var exp = factory();
    root.OpenAGYVoiceClient = exp.OpenAGYVoiceClient;
    root.openagyVoiceClient = exp;
  }
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // --- Protobuf Minimal Reader & Writer for Jetski Voice Proto ---

  var ProtoReader = (function () {
    function ProtoReader(buf) {
      if (buf instanceof ArrayBuffer) {
        this.view = new DataView(buf);
      } else if (buf && buf.buffer instanceof ArrayBuffer) {
        this.view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
      } else {
        throw new Error('ProtoReader: invalid buffer');
      }
      this.pos = 0;
      this.len = this.view.byteLength;
    }

    ProtoReader.prototype.hasMore = function () {
      return this.pos < this.len;
    };

    ProtoReader.prototype.readVarint = function () {
      var res = 0;
      var shift = 0;
      var b;
      do {
        if (this.pos >= this.len) throw new Error('Truncated varint');
        b = this.view.getUint8(this.pos++);
        res |= (b & 0x7f) << shift;
        shift += 7;
      } while (b & 0x80);
      return res >>> 0;
    };

    ProtoReader.prototype.readTag = function () {
      var v = this.readVarint();
      return { field: v >>> 3, wireType: v & 7 };
    };

    ProtoReader.prototype.readBytes = function () {
      var len = this.readVarint();
      if (this.pos + len > this.len) throw new Error('Truncated bytes');
      var bytes = new Uint8Array(this.view.buffer, this.view.byteOffset + this.pos, len);
      this.pos += len;
      return bytes;
    };

    ProtoReader.prototype.readString = function () {
      var bytes = this.readBytes();
      if (typeof TextDecoder !== 'undefined') {
        return new TextDecoder().decode(bytes);
      }
      var str = '';
      for (var i = 0; i < bytes.length; i++) {
        str += String.fromCharCode(bytes[i]);
      }
      return str;
    };

    ProtoReader.prototype.readFloat64 = function () {
      if (this.pos + 8 > this.len) throw new Error('Truncated float64');
      var v = this.view.getFloat64(this.pos, true);
      this.pos += 8;
      return v;
    };

    ProtoReader.prototype.skip = function (wireType) {
      if (wireType === 0) {
        this.readVarint();
      } else if (wireType === 1) {
        this.pos += 8;
      } else if (wireType === 2) {
        var len = this.readVarint();
        this.pos += len;
      } else if (wireType === 5) {
        this.pos += 4;
      } else {
        throw new Error('Unsupported wire type: ' + wireType);
      }
    };

    return ProtoReader;
  })();

  var ProtoWriter = (function () {
    function ProtoWriter() {
      this.buf = [];
    }

    ProtoWriter.prototype.writeVarint = function (val) {
      val = val >>> 0;
      while (val > 0x7f) {
        this.buf.push((val & 0x7f) | 0x80);
        val >>>= 7;
      }
      this.buf.push(val);
    };

    ProtoWriter.prototype.writeTag = function (field, wireType) {
      this.writeVarint((field << 3) | wireType);
    };

    ProtoWriter.prototype.writeBytes = function (field, bytes) {
      this.writeTag(field, 2);
      this.writeVarint(bytes.byteLength || bytes.length);
      for (var i = 0; i < bytes.length; i++) {
        this.buf.push(bytes[i]);
      }
    };

    ProtoWriter.prototype.writeString = function (field, str) {
      var encoded;
      if (typeof TextEncoder !== 'undefined') {
        encoded = new TextEncoder().encode(str);
      } else {
        encoded = [];
        for (var i = 0; i < str.length; i++) {
          encoded.push(str.charCodeAt(i));
        }
      }
      this.writeBytes(field, encoded);
    };

    ProtoWriter.prototype.writeDouble = function (field, val) {
      this.writeTag(field, 1);
      var view = new DataView(new ArrayBuffer(8));
      view.setFloat64(0, val, true);
      for (var i = 0; i < 8; i++) {
        this.buf.push(view.getUint8(i));
      }
    };

    ProtoWriter.prototype.finish = function () {
      return new Uint8Array(this.buf);
    };

    return ProtoWriter;
  })();

  // --- Voice Protocol Serialization Helpers ---

  var VoiceProto = {
    // Encode VoiceClientMessage with Setup
    encodeSetup: function (opts) {
      var setupWriter = new ProtoWriter();
      if (opts.conversationId) setupWriter.writeString(1, opts.conversationId);
      if (opts.voice) setupWriter.writeString(2, opts.voice);
      setupWriter.writeTag(3, 0);
      setupWriter.writeVarint(opts.sampleRate || 16000);
      if (opts.planModel) {
        setupWriter.writeTag(4, 0);
        setupWriter.writeVarint(opts.planModel);
      }
      if (opts.playbackSpeed) {
        setupWriter.writeDouble(5, opts.playbackSpeed);
      }
      // runtime_environment = 1 (BROWSER)
      setupWriter.writeTag(7, 0);
      setupWriter.writeVarint(1);

      var topWriter = new ProtoWriter();
      topWriter.writeBytes(1, setupWriter.finish()); // field 1 = setup
      return topWriter.finish();
    },

    // Encode VoiceClientMessage with MicAudio
    encodeMicAudio: function (pcmBytes) {
      var topWriter = new ProtoWriter();
      topWriter.writeBytes(2, pcmBytes); // field 2 = mic_audio
      return topWriter.finish();
    },

    // Encode VoiceClientMessage with SettingsUpdate
    encodeSettingsUpdate: function (opts) {
      var updateWriter = new ProtoWriter();
      if (opts.planModel) {
        updateWriter.writeTag(1, 0);
        updateWriter.writeVarint(opts.planModel);
      }
      if (opts.playbackSpeed) {
        updateWriter.writeDouble(2, opts.playbackSpeed);
      }

      var topWriter = new ProtoWriter();
      topWriter.writeBytes(3, updateWriter.finish()); // field 3 = settings_update
      return topWriter.finish();
    },

    // Encode VoiceClientMessage with UserTranscript
    encodeUserTranscript: function (text, isFinal) {
      var transcriptWriter = new ProtoWriter();
      transcriptWriter.writeString(1, text);
      transcriptWriter.writeTag(2, 0);
      transcriptWriter.writeVarint(isFinal ? 1 : 0);

      var topWriter = new ProtoWriter();
      topWriter.writeBytes(5, transcriptWriter.finish()); // field 5 = user_transcript
      return topWriter.finish();
    },

    // Decode VoiceServerMessage
    decodeServerMessage: function (data) {
      var reader = new ProtoReader(data);
      var msg = {};

      while (reader.hasMore()) {
        var tag = reader.readTag();
        switch (tag.field) {
          case 1: // voice_audio (bytes)
            msg.voiceAudio = reader.readBytes();
            break;
          case 2: // status (SessionStatus)
            msg.status = VoiceProto.decodeStatus(reader.readBytes());
            break;
          case 7: // voice_transcript (VoiceTranscript)
            msg.voiceTranscript = VoiceProto.decodeVoiceTranscript(reader.readBytes());
            break;
          case 8: // user_speech (UserSpeech)
            msg.userSpeech = VoiceProto.decodeUserSpeech(reader.readBytes());
            break;
          case 9: // clear_audio (ClearAudio)
            reader.readBytes(); // empty
            msg.clearAudio = true;
            break;
          case 10: // audio_timing (AudioTiming)
            msg.audioTiming = VoiceProto.decodeAudioTiming(reader.readBytes());
            break;
          default:
            reader.skip(tag.wireType);
            break;
        }
      }
      return msg;
    },

    decodeStatus: function (bytes) {
      var reader = new ProtoReader(bytes);
      var status = { state: 0, failureReason: 0, message: '', attempt: 1, maxAttempts: 5, cascadeId: '' };
      while (reader.hasMore()) {
        var tag = reader.readTag();
        switch (tag.field) {
          case 1: status.state = reader.readVarint(); break;
          case 2: status.failureReason = reader.readVarint(); break;
          case 3: status.message = reader.readString(); break;
          case 4: status.attempt = reader.readVarint(); break;
          case 5: status.maxAttempts = reader.readVarint(); break;
          case 6: status.cascadeId = reader.readString(); break;
          default: reader.skip(tag.wireType); break;
        }
      }
      return status;
    },

    decodeVoiceTranscript: function (bytes) {
      var reader = new ProtoReader(bytes);
      var transcript = { text: '', seq: 0, audioMs: 0 };
      while (reader.hasMore()) {
        var tag = reader.readTag();
        switch (tag.field) {
          case 1: transcript.text = reader.readString(); break;
          case 2: transcript.seq = reader.readVarint(); break;
          case 3: transcript.audioMs = reader.readVarint(); break;
          default: reader.skip(tag.wireType); break;
        }
      }
      return transcript;
    },

    decodeUserSpeech: function (bytes) {
      var reader = new ProtoReader(bytes);
      var speech = { text: '', seq: 0, interim: false };
      while (reader.hasMore()) {
        var tag = reader.readTag();
        switch (tag.field) {
          case 1: speech.text = reader.readString(); break;
          case 2: speech.seq = reader.readVarint(); break;
          case 3: speech.interim = reader.readVarint() !== 0; break;
          default: reader.skip(tag.wireType); break;
        }
      }
      return speech;
    },

    decodeAudioTiming: function (bytes) {
      var reader = new ProtoReader(bytes);
      var timing = { seq: 0, audioMs: 0, playbackSpeed: 1.0 };
      while (reader.hasMore()) {
        var tag = reader.readTag();
        switch (tag.field) {
          case 1: timing.seq = reader.readVarint(); break;
          case 2: timing.audioMs = reader.readVarint(); break;
          case 3: timing.playbackSpeed = reader.readFloat64(); break;
          default: reader.skip(tag.wireType); break;
        }
      }
      return timing;
    }
  };

  // --- 24kHz PCM Playback Queue with Natural Barge-in ---

  var PCM24kPlayer = (function () {
    function PCM24kPlayer(audioContext, initialVolume) {
      this.ctx = audioContext;
      this.gain = this.ctx.createGain();
      this.gain.connect(this.ctx.destination);
      this.volume = initialVolume !== undefined ? initialVolume : 1.0;
      this.gain.gain.setValueAtTime(this.volume, this.ctx.currentTime);
      this.nextPlayTime = 0;
      this.activeSources = [];
      this.sampleRate = 24000;
    }

    PCM24kPlayer.prototype.enqueue = function (pcmBytes) {
      if (!pcmBytes || pcmBytes.byteLength === 0) return;
      if (this.ctx.state === 'suspended') {
        this.ctx.resume().catch(function () {});
      }

      // 16-bit mono PCM -> Float32
      var int16 = new Int16Array(
        pcmBytes.buffer,
        pcmBytes.byteOffset,
        pcmBytes.byteLength / 2
      );
      var float32 = new Float32Array(int16.length);
      for (var i = 0; i < int16.length; i++) {
        var s = int16[i];
        float32[i] = s < 0 ? s / 32768.0 : s / 32767.0;
      }

      var buffer = this.ctx.createBuffer(1, float32.length, this.sampleRate);
      buffer.copyToChannel(float32, 0);

      var source = this.ctx.createBufferSource();
      source.buffer = buffer;
      source.connect(this.gain);

      var now = this.ctx.currentTime;
      var startTime = Math.max(now + 0.015, this.nextPlayTime);
      source.start(startTime);
      this.nextPlayTime = startTime + buffer.duration;

      var entry = { source: source, endTime: this.nextPlayTime };
      this.activeSources.push(entry);

      var self = this;
      source.onended = function () {
        var idx = self.activeSources.indexOf(entry);
        if (idx !== -1) {
          self.activeSources.splice(idx, 1);
        }
      };
    };

    PCM24kPlayer.prototype.clearWithRamp = function () {
      var now = this.ctx.currentTime;
      var fadeDuration = 0.015; // 15ms rapid fade-out to prevent pop
      this.gain.gain.cancelScheduledValues(now);
      this.gain.gain.setValueAtTime(this.gain.gain.value, now);
      this.gain.gain.linearRampToValueAtTime(0, now + fadeDuration);

      var self = this;
      setTimeout(function () {
        for (var i = 0; i < self.activeSources.length; i++) {
          try {
            self.activeSources[i].source.stop();
            self.activeSources[i].source.disconnect();
          } catch (_) {}
        }
        self.activeSources = [];
        self.nextPlayTime = 0;
        self.gain.gain.setValueAtTime(self.volume, self.ctx.currentTime);
      }, Math.round(fadeDuration * 1000) + 5);
    };

    PCM24kPlayer.prototype.setVolume = function (vol) {
      this.volume = Math.max(0, Math.min(1, vol));
      this.gain.gain.setValueAtTime(this.volume, this.ctx.currentTime);
    };

    PCM24kPlayer.prototype.isPlaying = function () {
      return this.activeSources.length > 0 && this.nextPlayTime > this.ctx.currentTime;
    };

    PCM24kPlayer.prototype.close = function () {
      this.clearWithRamp();
      this.gain.disconnect();
    };

    return PCM24kPlayer;
  })();

  // --- OpenAGYVoiceClient ---

  function OpenAGYVoiceClient(options) {
    this.opts = options || {};
    this.conversationId = this.opts.conversationId || '';
    this.voice = this.opts.voice || 'Fola';
    this.sampleRate = this.opts.sampleRate || 16000;
    this.audioProcessorUrl = this.opts.audioProcessorUrl || '/audio_processor.js';
    this.volume = this.opts.volume !== undefined ? this.opts.volume : 1.0;
    this.listeners = {};
    this.ws = null;
    this.audioCtx = null;
    this.player = null;
    this.mediaStream = null;
    this.sourceNode = null;
    this.workletNode = null;
    this.scriptProcessor = null;
    this.isRecording = false;
    this.muted = false;
    this.state = 'idle'; // idle | connecting | listening | closed
  }

  OpenAGYVoiceClient.prototype.on = function (event, cb) {
    if (!this.listeners[event]) this.listeners[event] = [];
    this.listeners[event].push(cb);
    return this;
  };

  OpenAGYVoiceClient.prototype.off = function (event, cb) {
    if (!this.listeners[event]) return this;
    if (!cb) {
      delete this.listeners[event];
    } else {
      this.listeners[event] = this.listeners[event].filter(function (fn) {
        return fn !== cb;
      });
    }
    return this;
  };

  OpenAGYVoiceClient.prototype.emit = function (event, data) {
    var cbs = this.listeners[event];
    if (cbs) {
      for (var i = 0; i < cbs.length; i++) {
        try {
          cbs[i](data);
        } catch (e) {
          console.error('[OpenAGYVoiceClient] Event callback error for', event, e);
        }
      }
    }
    // Also dispatch browser CustomEvent for interoperability
    if (typeof window !== 'undefined' && window.dispatchEvent && typeof CustomEvent !== 'undefined') {
      try {
        var ev = new CustomEvent('openagy-voice-' + event, { detail: data });
        window.dispatchEvent(ev);
      } catch (_) {}
    }
  };

  OpenAGYVoiceClient.prototype.start = function () {
    var self = this;
    return new Promise(function (resolve, reject) {
      try {
        self.state = 'connecting';
        self.emit('status', { state: 'connecting' });

        // 1. Initialize AudioContext
        var AudioCtx = window.AudioContext || window.webkitAudioContext;
        if (!AudioCtx) {
          throw new Error('Web Audio API not supported in this browser');
        }
        self.audioCtx = new AudioCtx();
        self.player = new PCM24kPlayer(self.audioCtx, self.volume);

        // 2. Resolve WebSocket URL
        var wsUrl = self.opts.wsUrl;
        if (!wsUrl) {
          var proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
          var params = new URLSearchParams({
            conversation_id: self.conversationId,
            sample_rate: String(self.sampleRate),
            voice: self.voice
          });
          wsUrl = proto + '//' + window.location.host + '/voice/ws?' + params.toString();
        }

        // 3. Open WebSocket with jetski-voice-proto
        self.ws = new WebSocket(wsUrl, ['jetski-voice-proto']);
        self.ws.binaryType = 'arraybuffer';

        self.ws.onopen = function () {
          self.state = 'listening';
          self.emit('status', { state: 'listening' });

          // Send Setup message
          var setupMsg = VoiceProto.encodeSetup({
            conversationId: self.conversationId,
            voice: self.voice,
            sampleRate: self.sampleRate,
            playbackSpeed: 1.0
          });
          self.ws.send(setupMsg);

          // 4. Start Microphone Capture
          self._startMicCapture()
            .then(function () {
              resolve();
            })
            .catch(function (err) {
              self.emit('error', err);
              reject(err);
            });
        };

        self.ws.onmessage = function (event) {
          if (event.data instanceof ArrayBuffer) {
            self._handleServerMessage(event.data);
          }
        };

        self.ws.onerror = function (event) {
          var err = new Error('WebSocket error occurred');
          self.emit('error', err);
        };

        self.ws.onclose = function (event) {
          self.state = 'closed';
          self.emit('close', event);
          self.stop();
        };
      } catch (err) {
        self.stop();
        reject(err);
      }
    });
  };

  OpenAGYVoiceClient.prototype._handleServerMessage = function (buffer) {
    try {
      var msg = VoiceProto.decodeServerMessage(buffer);

      if (msg.status) {
        this.emit('status', msg.status);
      }

      if (msg.clearAudio) {
        if (this.player) {
          this.player.clearWithRamp();
        }
        this.emit('interrupted', {});
      }

      if (msg.voiceAudio) {
        if (this.player) {
          this.player.enqueue(msg.voiceAudio);
        }
        this.emit('audio', msg.voiceAudio);
      }

      if (msg.voiceTranscript) {
        this.emit('agentSpeech', msg.voiceTranscript);
      }

      if (msg.userSpeech) {
        this.emit('userSpeech', msg.userSpeech);
      }

      if (msg.audioTiming) {
        this.emit('audioTiming', msg.audioTiming);
      }
    } catch (e) {
      console.warn('[OpenAGYVoiceClient] Error parsing server message', e);
    }
  };

  OpenAGYVoiceClient.prototype._startMicCapture = function () {
    var self = this;
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      return Promise.reject(new Error('Microphone access not available'));
    }

    return navigator.mediaDevices
      .getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: false,
          channelCount: 1,
          sampleRate: self.sampleRate
        }
      })
      .then(function (stream) {
        self.mediaStream = stream;
        self.sourceNode = self.audioCtx.createMediaStreamSource(stream);

        // Try AudioWorklet first
        if (self.audioCtx.audioWorklet && typeof self.audioCtx.audioWorklet.addModule === 'function') {
          return self.audioCtx.audioWorklet
            .addModule(self.audioProcessorUrl)
            .then(function () {
              self.workletNode = new AudioWorkletNode(self.audioCtx, 'audio-streaming-processor');
              self.workletNode.port.onmessage = function (event) {
                if (event.data instanceof ArrayBuffer) {
                  self._sendMicChunk(new Uint8Array(event.data));
                }
              };
              self.sourceNode.connect(self.workletNode);
              self.isRecording = true;
            })
            .catch(function (workletErr) {
              console.warn('[OpenAGYVoiceClient] AudioWorklet init failed, falling back to ScriptProcessor', workletErr);
              self._initScriptProcessorFallback();
            });
        } else {
          self._initScriptProcessorFallback();
        }
      });
  };

  OpenAGYVoiceClient.prototype._initScriptProcessorFallback = function () {
    var self = this;
    var bufferSize = 4096;
    var scriptNode = self.audioCtx.createScriptProcessor(bufferSize, 1, 1);
    self.scriptProcessor = scriptNode;

    var targetRate = 16000;
    var ctxRate = self.audioCtx.sampleRate || 44100;
    var sampleRatio = ctxRate / targetRate;
    var chunkSamples = 640;
    var pcmBuffer = new Int16Array(chunkSamples);
    var pcmIdx = 0;

    scriptNode.onaudioprocess = function (e) {
      if (!self.isRecording) return;
      var input = e.inputBuffer.getChannelData(0);

      for (var i = 0; i < input.length; i += sampleRatio) {
        var s = input[Math.floor(i)];
        var clamped = Math.max(-1, Math.min(1, s));
        pcmBuffer[pcmIdx++] = Math.round(clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff);

        if (pcmIdx >= chunkSamples) {
          var bytes = new Uint8Array(pcmBuffer.buffer.slice(0));
          self._sendMicChunk(bytes);
          pcmIdx = 0;
        }
      }
    };

    self.sourceNode.connect(scriptNode);
    scriptNode.connect(self.audioCtx.destination);
    self.isRecording = true;
  };

  OpenAGYVoiceClient.prototype._sendMicChunk = function (bytes) {
    if (this.muted) {
      // Send silence when muted so server keeps connection active
      bytes = new Uint8Array(bytes.byteLength);
    }
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      var wirePayload = VoiceProto.encodeMicAudio(bytes);
      this.ws.send(wirePayload);
    }
  };

  OpenAGYVoiceClient.prototype.sendText = function (text, isFinal) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN && text) {
      var wirePayload = VoiceProto.encodeUserTranscript(text, isFinal !== false);
      this.ws.send(wirePayload);
    }
  };

  OpenAGYVoiceClient.prototype.setPlaybackSpeed = function (speed) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      var wirePayload = VoiceProto.encodeSettingsUpdate({ playbackSpeed: speed });
      this.ws.send(wirePayload);
    }
  };

  OpenAGYVoiceClient.prototype.mute = function (muted) {
    this.muted = !!muted;
    this.emit('mute', { muted: this.muted });
  };

  OpenAGYVoiceClient.prototype.isMuted = function () {
    return this.muted;
  };

  OpenAGYVoiceClient.prototype.setVolume = function (vol) {
    this.volume = vol;
    if (this.player) {
      this.player.setVolume(vol);
    }
  };

  OpenAGYVoiceClient.prototype.clearAudio = function () {
    if (this.player) {
      this.player.clearWithRamp();
    }
  };

  OpenAGYVoiceClient.prototype.stop = function () {
    this.isRecording = false;

    if (this.workletNode) {
      try {
        this.workletNode.port.postMessage('stop');
        this.workletNode.disconnect();
      } catch (_) {}
      this.workletNode = null;
    }

    if (this.scriptProcessor) {
      try {
        this.scriptProcessor.disconnect();
      } catch (_) {}
      this.scriptProcessor = null;
    }

    if (this.sourceNode) {
      try {
        this.sourceNode.disconnect();
      } catch (_) {}
      this.sourceNode = null;
    }

    if (this.mediaStream) {
      try {
        var tracks = this.mediaStream.getTracks();
        for (var i = 0; i < tracks.length; i++) {
          tracks[i].stop();
        }
      } catch (_) {}
      this.mediaStream = null;
    }

    if (this.player) {
      try {
        this.player.close();
      } catch (_) {}
      this.player = null;
    }

    if (this.audioCtx) {
      try {
        this.audioCtx.close();
      } catch (_) {}
      this.audioCtx = null;
    }

    if (this.ws) {
      try {
        if (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING) {
          this.ws.close();
        }
      } catch (_) {}
      this.ws = null;
    }

    this.state = 'idle';
    this.emit('status', { state: 'idle' });
  };

  // Static helper
  OpenAGYVoiceClient.create = function (options) {
    return new OpenAGYVoiceClient(options);
  };

  return {
    OpenAGYVoiceClient: OpenAGYVoiceClient,
    VoiceProto: VoiceProto,
    PCM24kPlayer: PCM24kPlayer
  };
});
