package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/tartine-studio/harmony-server/internal/application"
	"github.com/tartine-studio/harmony-server/internal/config"
	"github.com/tartine-studio/harmony-server/internal/voiceclient"
)

const eventTimeout = 5 * time.Second

func TestMain(m *testing.M) {
	application.BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// testEnv is a real Harmony server (SQLite in a temp dir, SFU on a random UDP
// port) served by httptest.
type testEnv struct {
	t   *testing.T
	url string
}

func newTestServer(t *testing.T, tweak ...func(*config.Config)) *testEnv {
	t.Helper()
	cfg := config.Config{
		DataDir:              t.TempDir(),
		JWTSecret:            "test-secret",
		JWTAccessTTL:         time.Minute,
		JWTRefreshTTL:        time.Hour,
		PresenceIdleTimeout:  time.Minute,
		PresenceOfflineGrace: 50 * time.Millisecond,
		VoiceUDPPort:         0, // OS-assigned: tests can run in parallel
		VoiceMaxParticipants: 25,
	}
	for _, f := range tweak {
		f(&cfg)
	}
	srv, err := New(cfg, zap.NewNop())
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(func() {
		ts.Close()
		srv.Close()
	})
	return &testEnv{t: t, url: ts.URL}
}

// do performs a JSON request and returns the status and raw body.
func (e *testEnv) do(method, path, token string, body any) (int, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(e.t, err)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.url+path, r)
	require.NoError(e.t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	return resp.StatusCode, raw
}

// call performs a request, checks the status and decodes the body into out.
func (e *testEnv) call(method, path, token string, body any, wantStatus int, out any) {
	e.t.Helper()
	status, raw := e.do(method, path, token, body)
	require.Equal(e.t, wantStatus, status, "%s %s: %s", method, path, raw)
	if out != nil {
		require.NoError(e.t, json.Unmarshal(raw, out))
	}
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// expectError checks the {error, code} body of a failed request.
func (e *testEnv) expectError(method, path, token string, body any, wantStatus int, wantCode string) {
	e.t.Helper()
	var got apiError
	e.call(method, path, token, body, wantStatus, &got)
	require.Equal(e.t, wantCode, got.Code)
	require.NotEmpty(e.t, got.Error)
}

type account struct {
	ID           string
	Username     string
	Email        string
	AccessToken  string
	RefreshToken string
}

func (e *testEnv) register(name string) account {
	e.t.Helper()
	a := account{Username: name, Email: name + "@example.com"}
	var user struct{ ID string }
	e.call(http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": name, "email": a.Email, "password": "password123",
	}, http.StatusCreated, &user)
	a.ID = user.ID

	var pair struct{ AccessToken, RefreshToken string }
	e.call(http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": a.Email, "password": "password123",
	}, http.StatusOK, &pair)
	a.AccessToken, a.RefreshToken = pair.AccessToken, pair.RefreshToken
	return a
}

type channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (e *testEnv) createChannel(token, name, typ string) channel {
	e.t.Helper()
	var c channel
	e.call(http.MethodPost, "/api/channels", token, map[string]string{"name": name, "type": typ}, http.StatusCreated, &c)
	return c
}

// event is a server → client WebSocket frame.
type event struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"requestId"`
}

func (ev event) decode(t *testing.T, out any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(ev.Data, out))
}

// wsClient is a test WebSocket client. Every received frame is queued; expect
// pops frames until one matches. If voice is set, voiceOffer frames are also
// answered from the read loop (like a real client would).
type wsClient struct {
	t      *testing.T
	conn   *websocket.Conn
	writeM sync.Mutex
	events chan event
	voice  *voiceclient.Client
	voiceM sync.Mutex
}

func (e *testEnv) wsURL() string {
	return "ws" + strings.TrimPrefix(e.url, "http") + "/api/ws"
}

// dialRaw opens a WebSocket with the given header/URL, without waiting for
// snapshots. Returns the HTTP status on handshake failure.
func (e *testEnv) dialRaw(u string, header http.Header) (*websocket.Conn, int, error) {
	conn, resp, err := websocket.DefaultDialer.Dial(u, header)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	return conn, status, err
}

// connect opens an authenticated WebSocket and waits for the two snapshots,
// after which the session is guaranteed to receive broadcasts.
func (e *testEnv) connect(a account) *wsClient {
	e.t.Helper()
	conn, _, err := e.dialRaw(e.wsURL(), http.Header{"Authorization": {"Bearer " + a.AccessToken}})
	require.NoError(e.t, err)
	c := &wsClient{t: e.t, conn: conn, events: make(chan event, 512)}
	go c.readLoop()
	e.t.Cleanup(func() { conn.Close() })
	c.expect("presenceSnapshot")
	c.expect("voiceStateSnapshot")
	return c
}

func (c *wsClient) readLoop() {
	defer close(c.events)
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var ev event
		if json.Unmarshal(raw, &ev) != nil {
			continue
		}
		if ev.Type == "voiceOffer" {
			c.voiceM.Lock()
			v := c.voice
			c.voiceM.Unlock()
			if v != nil {
				var d struct{ SDP string }
				_ = json.Unmarshal(ev.Data, &d)
				if err := v.HandleOffer(d.SDP); err != nil {
					c.t.Logf("handle offer: %v", err)
				}
			}
		}
		c.events <- ev
	}
}

func (c *wsClient) send(eventType string, data any, requestID ...string) {
	c.t.Helper()
	env := map[string]any{"type": eventType, "data": data}
	if len(requestID) > 0 {
		env["requestId"] = requestID[0]
	}
	c.writeM.Lock()
	defer c.writeM.Unlock()
	require.NoError(c.t, c.conn.WriteJSON(env))
}

func (c *wsClient) sendRaw(raw string) {
	c.t.Helper()
	c.writeM.Lock()
	defer c.writeM.Unlock()
	require.NoError(c.t, c.conn.WriteMessage(websocket.TextMessage, []byte(raw)))
}

// expect returns the next event of type eventType matching all predicates,
// skipping others. Fails after eventTimeout.
func (c *wsClient) expect(eventType string, match ...func(event) bool) event {
	c.t.Helper()
	deadline := time.After(eventTimeout)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				c.t.Fatalf("connection closed while waiting for %s", eventType)
			}
			if ev.Type != eventType {
				continue
			}
			matched := true
			for _, m := range match {
				if !m(ev) {
					matched = false
					break
				}
			}
			if matched {
				return ev
			}
		case <-deadline:
			c.t.Fatalf("timed out waiting for %s", eventType)
		}
	}
}

// expectError waits for an error frame with the given code and request ID.
func (c *wsClient) expectError(code, requestID string) {
	c.t.Helper()
	ev := c.expect("error")
	var e apiError
	ev.decode(c.t, &e)
	require.Equal(c.t, code, e.Code, "error: %s", e.Error)
	require.Equal(c.t, requestID, ev.RequestID)
}

// expectNone fails if an event of eventType arrives within d.
func (c *wsClient) expectNone(eventType string, d time.Duration) {
	c.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return
			}
			if ev.Type == eventType {
				c.t.Fatalf("unexpected %s: %s", eventType, ev.Data)
			}
		case <-deadline:
			return
		}
	}
}

// field returns a predicate matching a top-level JSON field of the event data.
func field(name string, want any) func(event) bool {
	return func(ev event) bool {
		var m map[string]any
		if json.Unmarshal(ev.Data, &m) != nil {
			return false
		}
		return fmt.Sprint(m[name]) == fmt.Sprint(want)
	}
}
