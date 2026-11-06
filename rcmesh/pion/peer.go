//go:build !js && !wasip1 && !wasm

// Package rcmeshpion implements the native rcmesh answerer. It deliberately
// lives in a separate Go module so WASM hosts never acquire Pion dependencies.
package rcmeshpion

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/justwasm/pigasus/rcmesh"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

const maxBufferedBytes = 256 << 10
const maxConcurrentRPCs = 32

func init() {
	rcmesh.RegisterDefaultAnswerer(NewAnswer)
}

type Peer struct {
	pc          *webrtc.PeerConnection
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	authDone    chan struct{}
	once        sync.Once
	mu          sync.Mutex
	err         error
	channel     *webrtc.DataChannel
	authed      bool
	answer      string
	auth        *rcmesh.Authenticator
	config      rcmesh.PeerConfig
	sendMu      sync.Mutex
	low         chan struct{}
	slots       chan struct{}
	active      map[string]context.CancelFunc
	reassembler *rcmesh.ChunkReassembler
}

var _ rcmesh.Session = (*Peer)(nil)

// NewAnswer consumes an offer and returns a fully gathered answer. It does not
// contact a signaling service, register an instance, or enable a feature flag.
// Gathering and challenge authentication have separate bounded timeouts.
func NewAnswer(ctx context.Context, offerSDP string, config rcmesh.PeerConfig) (rcmesh.Session, error) {
	if ctx == nil || config.Handler == nil {
		return nil, errors.New("rcmeshpion: context and HTTP handler are required")
	}
	remoteFP, err := rcmesh.FingerprintFromSDP(offerSDP)
	if err != nil {
		return nil, err
	}
	if len(config.AuthorizedKeys) == 0 {
		return nil, rcmesh.ErrAuthentication
	}
	for _, key := range config.AuthorizedKeys {
		if _, err := rcmesh.ParsePublicKey(key); err != nil {
			return nil, err
		}
	}
	if config.GatherTimeout <= 0 {
		config.GatherTimeout = 15 * time.Second
	}
	if config.AuthTimeout <= 0 {
		config.AuthTimeout = 15 * time.Second
	}
	if config.SendTimeout <= 0 {
		config.SendTimeout = 5 * time.Second
	}
	servers := make([]webrtc.ICEServer, 0, len(config.ICEServers))
	for _, server := range config.ICEServers {
		servers = append(servers, webrtc.ICEServer{URLs: append([]string(nil), server.URLs...), Username: server.Username, Credential: server.Credential})
	}
	// Override NewAPI's default media codecs/interceptors: this adapter carries
	// data channels only. Bound SCTP messages before application JSON decoding.
	settings := webrtc.SettingEngine{}
	settings.SetSCTPMaxMessageSize(rcmesh.MaxFrameBytes)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&webrtc.MediaEngine{}),
		webrtc.WithInterceptorRegistry(&interceptor.Registry{}), webrtc.WithSettingEngine(settings))
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: servers})
	if err != nil {
		return nil, err
	}
	peerCtx, cancel := context.WithCancel(ctx)
	p := &Peer{pc: pc, ctx: peerCtx, cancel: cancel, done: make(chan struct{}), authDone: make(chan struct{}), config: config,
		low: make(chan struct{}, 1), slots: make(chan struct{}, maxConcurrentRPCs), active: make(map[string]context.CancelFunc),
		reassembler: rcmesh.NewChunkReassembler()}
	failed := func(err error) (rcmesh.Session, error) { p.fail(err); <-p.done; return nil, err }
	pc.OnDataChannel(p.bind)
	if config.OnLocalCandidate != nil {
		pc.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil {
				return
			}
			init := c.ToJSON()
			config.OnLocalCandidate(rcmesh.ICECandidate{
				Candidate:        init.Candidate,
				SDPMid:           init.SDPMid,
				SDPMLineIndex:    init.SDPMLineIndex,
				UsernameFragment: init.UsernameFragment,
			})
		})
	}
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if peerCtx.Err() == nil && (state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed) {
			p.fail(fmt.Errorf("rcmeshpion: peer %s", state))
		}
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return failed(err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return failed(err)
	}
	localFP, err := rcmesh.FingerprintFromSDP(answer.SDP)
	if err != nil {
		return failed(err)
	}
	p.auth, err = rcmesh.NewAuthenticator(localFP, remoteFP, config.AuthorizedKeys)
	if err != nil {
		return failed(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return failed(err)
	}
	// 【反编译】 In negotiateAnswer (0x102599d00), the reference does not wait
	// for complete ICE gathering when Trickle ICE is active (OnLocalCandidate != nil).
	// It captures the local SDP answer immediately so the browser receives the answer
	// in milliseconds without timing out or downgrading to WebChannel.
	if config.OnLocalCandidate == nil {
		timer := time.NewTimer(config.GatherTimeout)
		defer timer.Stop()
		select {
		case <-gathered:
		case <-timer.C:
			return failed(errors.New("rcmeshpion: ICE gathering timed out"))
		case <-peerCtx.Done():
			return failed(peerCtx.Err())
		case <-p.done:
			return nil, p.Err()
		}
	}
	p.answer = pc.LocalDescription().SDP
	go func() {
		timer := time.NewTimer(config.AuthTimeout)
		defer timer.Stop()
		select {
		case <-p.authDone:
			<-peerCtx.Done()
			p.fail(peerCtx.Err())
		case <-peerCtx.Done():
			p.fail(peerCtx.Err())
		case <-timer.C:
			p.fail(errors.New("rcmeshpion: authentication timed out"))
		}
	}()
	return p, nil
}

func (p *Peer) AnswerSDP() string { return p.answer }
func (p *Peer) AddICECandidate(c rcmesh.ICECandidate) error {
	return p.pc.AddICECandidate(webrtc.ICECandidateInit{Candidate: c.Candidate, SDPMid: c.SDPMid, SDPMLineIndex: c.SDPMLineIndex, UsernameFragment: c.UsernameFragment})
}

// SetSessionPin pins server-vouched public keys directly on this peer's authenticator.
func (p *Peer) SetSessionPin(keys []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var pubKeys []*ecdsa.PublicKey
	for _, k := range keys {
		pk, err := rcmesh.ParsePublicKey(k)
		if err != nil {
			return err
		}
		pubKeys = append(pubKeys, pk)
	}
	if p.auth != nil {
		p.auth.SetPinnedKeys(pubKeys)
	}
	return nil
}
func (p *Peer) Done() <-chan struct{} { return p.done }
func (p *Peer) Authenticated() bool   { p.mu.Lock(); defer p.mu.Unlock(); return p.authed }
func (p *Peer) Err() error            { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Peer) Close() error {
	p.once.Do(func() { p.cancel(); _ = p.pc.Close(); close(p.done) })
	return nil
}
func (p *Peer) fail(err error) {
	p.mu.Lock()
	first := p.err == nil
	if first {
		p.err = err
	}
	p.mu.Unlock()
	// Pion invokes some callbacks from its own transport loops; closing those
	// loops synchronously from a callback can deadlock.
	if first {
		p.cancel()
		go p.Close()
	}
}

func (p *Peer) bind(dc *webrtc.DataChannel) {
	p.mu.Lock()
	if p.channel != nil || !dc.Ordered() || dc.MaxRetransmits() != nil || dc.MaxPacketLifeTime() != nil {
		p.mu.Unlock()
		go dc.Close()
		return
	}
	p.channel = dc
	p.mu.Unlock()
	dc.SetBufferedAmountLowThreshold(maxBufferedBytes / 2)
	dc.OnBufferedAmountLow(func() {
		select {
		case p.low <- struct{}{}:
		default:
		}
	})
	dc.OnError(p.fail)
	dc.OnClose(func() {
		if p.ctx.Err() == nil {
			p.fail(errors.New("rcmeshpion: data channel closed"))
		}
	})
	dc.OnMessage(p.message)
	dc.OnOpen(func() {
		frame, err := json.Marshal(p.auth.Challenge())
		if err == nil {
			err = p.send(frame)
		}
		if err != nil {
			p.fail(err)
		}
	})
}

func (p *Peer) message(message webrtc.DataChannelMessage) {
	if p.ctx.Err() != nil {
		return
	}
	if len(message.Data) > rcmesh.MaxFrameBytes {
		// 【反编译】 an ingress message the reference cannot parse is dropped,
		// and the session survives it: "%s Dropping an unparseable %d-byte
		// ingress message on mesh session %s: %v".
		return
	}
	if !p.Authenticated() {
		var response rcmesh.AuthMessage
		err := json.Unmarshal(message.Data, &response)
		if err == nil {
			err = p.auth.Accept(response)
		}
		if err != nil {
			p.fail(err)
			return
		}
		frame, _ := json.Marshal(rcmesh.AuthMessage{Type: "success", SupportsChunking: false})
		if err := p.send(frame); err != nil {
			p.fail(err)
			return
		}
		p.mu.Lock()
		p.authed = true
		p.mu.Unlock()
		close(p.authDone)
		return
	}
	// Reserve before launching: long-lived streams must not block receipt of
	// other calls, while an authenticated peer cannot create unbounded workers.
	chunk, err := rcmesh.DecodeChunk(message.Data)
	if err != nil {
		// 【反编译】 handleWebRTCMessage (0x10259ed10) drops an ingress message
		// it cannot parse and keeps the session: "%s Dropping an unparseable
		// %d-byte ingress message on mesh session %s: %v".
		return
	}
	id := chunk.Request.RequestID
	if rejected := rcmesh.CheckRequestID(id); rejected != nil {
		// 【反编译】 a non-positive request id is answered with HTTP 400 and
		// "request_id must be a positive integer"; the session survives it.
		p.answerError(rejected)
		return
	}
	if rcmesh.IsCancelRequest(chunk.Request) {
		p.reassembler.Abort(p.config.SessionID, id)
		p.mu.Lock()
		cancel := p.active[id]
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	req, err := p.reassembler.AppendFrame(p.config.SessionID, chunk)
	if err != nil {
		var rejected *rcmesh.ChunkError
		if errors.As(err, &rejected) {
			// 【反编译】 a rejected chunk is answered with sendErrorResponse
			// (0x1025b22a0) and the session survives it.
			p.answerError(rejected)
			return
		}
		p.fail(err)
		return
	}
	if req == nil {
		// Non-final chunk received and stored; await remaining chunks
		return
	}
	p.mu.Lock()
	if p.active[id] != nil {
		p.mu.Unlock()
		p.fail(errors.New("rcmeshpion: duplicate active request ID"))
		return
	}
	rpcCtx, cancel := context.WithCancel(p.ctx)
	select {
	case p.slots <- struct{}{}:
		p.active[id] = cancel
	default:
		p.mu.Unlock()
		cancel()
		p.fail(errors.New("rcmeshpion: too many active RPCs"))
		return
	}
	p.mu.Unlock()
	go func() {
		defer cancel()
		defer func() { p.mu.Lock(); delete(p.active, id); <-p.slots; p.mu.Unlock() }()
		if err := (rcmesh.HTTPBridge{Handler: p.config.Handler}).HandleRequest(rpcCtx, req, p.send); err != nil {
			p.fail(err)
		}
	}()
}

// answerError answers a rejected request with the reference's error frame.
//
// 【反编译】 handleWebRTCMessage (0x10259ed10) answers a rejected chunk with
// sendErrorResponse (0x1025b22a0), which builds a ConnectInstanceRequestV2
// proto-JSON frame through errorResponseV2 (0x1025ac8f0). The session
// survives a rejected chunk; only a broken transport fails it.
func (p *Peer) answerError(rejected *rcmesh.ChunkError) {
	frame, err := rcmesh.ErrorResponse(rejected.RequestID, rejected.Status, rejected.Message)
	if err != nil {
		p.fail(err)
		return
	}
	if err := p.send(frame); err != nil {
		p.fail(err)
	}
}

func (p *Peer) send(frame []byte) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.mu.Lock()
	dc := p.channel
	p.mu.Unlock()
	if dc == nil {
		return errors.New("rcmeshpion: no data channel")
	}
	if len(frame) > rcmesh.MaxFrameBytes {
		return errors.New("rcmeshpion: outgoing frame too large")
	}
	timer := time.NewTimer(p.config.SendTimeout)
	defer timer.Stop()
	for dc.BufferedAmount()+uint64(len(frame)) > maxBufferedBytes {
		select {
		case <-p.low:
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-timer.C:
			return errors.New("rcmeshpion: send backpressure timed out")
		}
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	return dc.SendText(string(frame))
}
