// Command voicebot is a headless voice client for developing and testing the
// SFU without the real client. It logs in, joins a voice channel, answers the
// server's offers, optionally sends Opus packets (with an RFC 6464 audio level)
// and reports how many packets it receives from each participant.
//
//	go run ./cmd/voicebot -user alice -register -channel lounge -speak
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

var debugSDP bool

// opusSilence is a valid 20ms Opus frame. The SFU never decodes audio, so its
// content does not matter; only the RTP flow and the audio level do.
var opusSilence = []byte{0xf8, 0xff, 0xfe}

type bot struct {
	server   string
	token    string
	channel  string
	speak    bool
	level    uint8
	talk     time.Duration
	pause    time.Duration
	namesMu  sync.Mutex
	names    map[string]string
	ws       *websocket.Conn
	wsMu     sync.Mutex
	pc       *webrtc.PeerConnection
	mic      *webrtc.TrackLocalStaticRTP // nil unless -speak
	micAdded bool                        // only touched by readLoop
	answered atomic.Bool
	pendMu   sync.Mutex
	pending  []webrtc.ICECandidateInit
	counts   sync.Map // userID -> *atomic.Int64
}

func main() {
	server := flag.String("server", "http://localhost:8080", "Harmony server base URL")
	user := flag.String("user", "bot", "username; email is <user>@voicebot.local")
	password := flag.String("password", "voicebot123", "password")
	register := flag.Bool("register", false, "register the account first (ignored if it exists)")
	channel := flag.String("channel", "", "voice channel name or ID to join (required)")
	create := flag.Bool("create", false, "create the voice channel if it does not exist")
	speak := flag.Bool("speak", false, "send audio")
	level := flag.Uint("level", 30, "audio level in -dBov sent while talking (0 loudest, 127 silent)")
	talk := flag.Duration("talk", 0, "with -speak: talk for this long, then pause (0 = talk continuously)")
	pause := flag.Duration("pause", 2*time.Second, "with -talk: pause duration between talk bursts")
	showSDP := flag.Bool("sdp", false, "print SDP offers/answers and ICE candidates (debugging)")
	mute := flag.Bool("mute", false, "self-mute right after joining but keep sending audio (server must not report speaking)")
	deafen := flag.Bool("deafen", false, "deafen right after joining (should then receive nothing)")
	duration := flag.Duration("duration", 0, "exit after this long (0 = until Ctrl-C)")
	flag.Parse()
	if *channel == "" {
		log.Fatal("-channel is required")
	}

	email := *user + "@voicebot.local"
	if *register {
		post(*server, "/api/auth/register", "", map[string]string{"username": *user, "email": email, "password": *password}, nil)
	}
	var tokens struct{ AccessToken string }
	if err := post(*server, "/api/auth/login", "", map[string]string{"email": email, "password": *password}, &tokens); err != nil || tokens.AccessToken == "" {
		log.Fatalf("login failed (use -register for a new account): %v", err)
	}

	debugSDP = *showSDP
	b := &bot{server: *server, token: tokens.AccessToken, speak: *speak, level: uint8(*level), talk: *talk, pause: *pause, names: map[string]string{}}
	b.loadNames()
	b.channel = b.resolveChannel(*channel, *create)

	if err := b.newPeerConnection(); err != nil {
		log.Fatal(err)
	}
	if b.speak {
		mic, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "voicebot")
		if err != nil {
			log.Fatal(err)
		}
		b.mic = mic
	}
	if err := b.connect(); err != nil {
		log.Fatal(err)
	}
	b.send("joinVoice", map[string]string{"channelId": b.channel})
	if *deafen {
		b.send("updateVoiceState", map[string]bool{"deafened": true})
	}
	if *mute {
		b.send("updateVoiceState", map[string]bool{"muted": true})
	}

	go b.report()
	if b.speak {
		go b.sendAudio()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	if *duration > 0 {
		select {
		case <-stop:
		case <-time.After(*duration):
		}
	} else {
		<-stop
	}
	b.send("leaveVoice", map[string]string{})
	time.Sleep(200 * time.Millisecond)
	b.ws.Close()
	b.pc.Close()
}

func (b *bot) connect() error {
	u, _ := url.Parse(b.server)
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	u.Path = "/api/ws"
	header := http.Header{"Authorization": {"Bearer " + b.token}}
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return fmt.Errorf("websocket: %w", err)
	}
	b.ws = ws
	go b.readLoop()
	return nil
}

func (b *bot) readLoop() {
	for {
		_, raw, err := b.ws.ReadMessage()
		if err != nil {
			log.Printf("websocket closed: %v", err)
			os.Exit(1)
		}
		var env struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		switch env.Type {
		case "voiceOffer":
			var d struct{ SDP string }
			json.Unmarshal(env.Data, &d)
			if err := b.handleOffer(d.SDP); err != nil {
				log.Printf("handle offer: %v", err)
			}
		case "voiceStateUpdate", "voiceSpeaking", "voiceDisconnected", "voiceStateSnapshot", "error":
			log.Printf("<- %s %s", env.Type, b.pretty(env.Data))
		}
	}
}

func (b *bot) handleOffer(offer string) error {
	if debugSDP {
		log.Printf("offer:\n%s", offer)
	}
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return fmt.Errorf("set remote: %w", err)
	}
	if b.mic != nil && !b.micAdded {
		// Attaches to the recvonly slot the server offered for our mic.
		if _, err := b.pc.AddTrack(b.mic); err != nil {
			return fmt.Errorf("add mic: %w", err)
		}
		b.micAdded = true
	}
	answer, err := b.pc.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("create answer: %w", err)
	}
	if err := b.pc.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("set local: %w", err)
	}
	// The answer must reach the server before our trickled candidates.
	b.send("voiceAnswer", map[string]string{"sdp": b.pc.LocalDescription().SDP})
	b.pendMu.Lock()
	b.answered.Store(true)
	for _, c := range b.pending {
		b.send("voiceCandidate", c)
	}
	b.pending = nil
	b.pendMu.Unlock()
	return nil
}

func (b *bot) newPeerConnection() error {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"},
		PayloadType:        111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return err
	}
	if err := m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: sdp.AudioLevelURI}, webrtc.RTPCodecTypeAudio); err != nil {
		return err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, registry); err != nil {
		return err
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(registry)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		if debugSDP {
			log.Printf("local candidate: %s", init.Candidate)
		}
		b.pendMu.Lock()
		defer b.pendMu.Unlock()
		if b.answered.Load() {
			b.send("voiceCandidate", init)
		} else {
			b.pending = append(b.pending, init)
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("peer connection: %s", s)
	})
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		userID := t.StreamID()
		log.Printf("receiving audio track from %s", b.name(userID))
		n, _ := b.counts.LoadOrStore(userID, &atomic.Int64{})
		for {
			if _, _, err := t.ReadRTP(); err != nil {
				return
			}
			n.(*atomic.Int64).Add(1)
		}
	})
	b.pc = pc
	return nil
}

func (b *bot) sendAudio() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var seq uint16
	var ts uint32
	burstStart := time.Now()
	for range ticker.C {
		if b.pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			continue
		}
		level := b.level
		if b.talk > 0 {
			cycle := time.Since(burstStart) % (b.talk + b.pause)
			if cycle >= b.talk {
				level = 127 // silence
			}
		}
		pkt := &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: seq, Timestamp: ts},
			Payload: opusSilence,
		}
		if id := b.audioLevelExtID(); id != 0 {
			ext, _ := rtp.AudioLevelExtension{Level: level, Voice: level < 127}.Marshal()
			pkt.Header.SetExtension(id, ext)
		}
		if err := b.mic.WriteRTP(pkt); err != nil {
			log.Printf("write rtp: %v", err)
		}
		seq++
		ts += 960
	}
}

func (b *bot) audioLevelExtID() uint8 {
	for _, t := range b.pc.GetTransceivers() {
		if s := t.Sender(); s != nil && s.Track() == b.mic {
			for _, e := range s.GetParameters().HeaderExtensions {
				if e.URI == sdp.AudioLevelURI {
					return uint8(e.ID)
				}
			}
		}
	}
	return 0
}

func (b *bot) report() {
	for range time.Tick(2 * time.Second) {
		var parts []string
		b.counts.Range(func(k, v any) bool {
			parts = append(parts, fmt.Sprintf("%s=%d", b.name(k.(string)), v.(*atomic.Int64).Load()))
			return true
		})
		if len(parts) > 0 {
			sort.Strings(parts)
			log.Printf("packets received: %s", strings.Join(parts, " "))
		}
	}
}

func (b *bot) send(eventType string, data any) {
	raw, _ := json.Marshal(data)
	b.wsMu.Lock()
	defer b.wsMu.Unlock()
	if err := b.ws.WriteJSON(map[string]any{"type": eventType, "data": json.RawMessage(raw)}); err != nil {
		log.Printf("send %s: %v", eventType, err)
	}
}

func (b *bot) loadNames() {
	var users []struct{ ID, Username string }
	get(b.server, "/api/users", b.token, &users)
	b.namesMu.Lock()
	defer b.namesMu.Unlock()
	for _, u := range users {
		b.names[u.ID] = u.Username
	}
}

func (b *bot) resolveChannel(nameOrID string, create bool) string {
	var channels []struct{ ID, Name, Type string }
	get(b.server, "/api/channels", b.token, &channels)
	for _, c := range channels {
		if c.ID == nameOrID || (c.Name == nameOrID && c.Type == "voice") {
			b.names[c.ID] = "#" + c.Name
			return c.ID
		}
	}
	if !create {
		log.Fatalf("voice channel %q not found (use -create)", nameOrID)
	}
	var c struct{ ID string }
	if err := post(b.server, "/api/channels", b.token, map[string]string{"name": nameOrID, "type": "voice"}, &c); err != nil || c.ID == "" {
		log.Fatalf("create channel: %v", err)
	}
	b.names[c.ID] = "#" + nameOrID
	return c.ID
}

func (b *bot) name(id string) string {
	if n, ok := b.lookup(id); ok {
		return n
	}
	b.loadNames() // someone new joined
	if n, ok := b.lookup(id); ok {
		return n
	}
	return id
}

func (b *bot) lookup(id string) (string, bool) {
	b.namesMu.Lock()
	defer b.namesMu.Unlock()
	n, ok := b.names[id]
	return n, ok
}

func (b *bot) pretty(data []byte) string {
	s := string(data)
	b.namesMu.Lock()
	defer b.namesMu.Unlock()
	for id, n := range b.names {
		s = strings.ReplaceAll(s, id, n)
	}
	return s
}

func post(server, path, token string, body, out any) error {
	raw, _ := json.Marshal(body)
	return do(http.MethodPost, server+path, token, bytes.NewReader(raw), out)
}

func get(server, path, token string, out any) error {
	return do(http.MethodGet, server+path, token, nil, out)
}

func do(method, u, token string, body *bytes.Reader, out any) error {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, u, body)
	} else {
		req, err = http.NewRequest(method, u, nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s", method, u, resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
