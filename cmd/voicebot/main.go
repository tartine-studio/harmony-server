// Command voicebot is a headless voice client for developing and testing the
// SFU without the real client. It logs in, joins a voice channel, answers the
// server's offers, optionally sends Opus packets (with an RFC 6464 audio level)
// and reports how many packets it receives from each participant.
//
//	go run ./cmd/voicebot -user alice -register -channel lounge -speak
package main

import (
	"bytes"
	"context"
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
	"time"

	"github.com/gorilla/websocket"

	"github.com/tartine-studio/harmony-server/internal/voiceclient"
)

type bot struct {
	server  string
	token   string
	channel string
	namesMu sync.Mutex
	names   map[string]string
	ws      *websocket.Conn
	wsMu    sync.Mutex
	voice   *voiceclient.Client
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
	mute := flag.Bool("mute", false, "self-mute right after joining but keep sending audio (the server must drop it)")
	unmuteAfter := flag.Duration("unmute-after", 0, "with -mute: unmute after this long (0 = stay muted)")
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

	b := &bot{server: *server, token: tokens.AccessToken, names: map[string]string{}}
	b.loadNames()
	b.channel = b.resolveChannel(*channel, *create)

	opts := voiceclient.Options{Speak: *speak, Level: uint8(*level), Logf: log.Printf, DebugSDP: *showSDP}
	voice, err := voiceclient.New(b.send, opts)
	if err != nil {
		log.Fatal(err)
	}
	b.voice = voice
	if err := b.connect(); err != nil {
		log.Fatal(err)
	}
	b.send("joinVoice", map[string]string{"channelId": b.channel})
	if *deafen {
		b.send("updateVoiceState", map[string]bool{"deafened": true})
	}
	if *mute {
		b.send("updateVoiceState", map[string]bool{"muted": true})
		if *unmuteAfter > 0 {
			time.AfterFunc(*unmuteAfter, func() {
				log.Printf("unmuting")
				b.send("updateVoiceState", map[string]bool{"muted": false})
			})
		}
	}

	go b.report()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *speak {
		go voice.SendAudio(ctx)
		if *talk > 0 {
			go burst(ctx, voice, uint8(*level), *talk, *pause)
		}
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
	b.voice.Close()
}

// burst alternates talking (level) and silence: talk for talk, pause for pause.
func burst(ctx context.Context, voice *voiceclient.Client, level uint8, talk, pause time.Duration) {
	for {
		voice.SetLevel(level)
		select {
		case <-ctx.Done():
			return
		case <-time.After(talk):
		}
		voice.SetLevel(voiceclient.LevelSilent)
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
	}
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
			if err := b.voice.HandleOffer(d.SDP); err != nil {
				log.Printf("handle offer: %v", err)
			}
		case "voiceStateUpdate", "voiceSpeaking", "voiceDisconnected", "voiceStateSnapshot", "error":
			log.Printf("<- %s %s", env.Type, b.pretty(env.Data))
		}
	}
}

func (b *bot) report() {
	for range time.Tick(2 * time.Second) {
		var parts []string
		for id, n := range b.voice.Counts() {
			parts = append(parts, fmt.Sprintf("%s=%d", b.name(id), n))
		}
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
