package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/voiceclient"
)

// attachVoice gives the WebSocket client a WebRTC peer: voiceOffer frames are
// answered automatically by the read loop.
func (c *wsClient) attachVoice(opts voiceclient.Options) *voiceclient.Client {
	c.t.Helper()
	send := func(eventType string, data any) {
		// Called from Pion goroutines: never FailNow here.
		c.writeM.Lock()
		defer c.writeM.Unlock()
		if err := c.conn.WriteJSON(map[string]any{"type": eventType, "data": data}); err != nil {
			c.t.Logf("send %s: %v", eventType, err)
		}
	}
	v, err := voiceclient.New(send, opts)
	require.NoError(c.t, err)
	c.t.Cleanup(func() { v.Close() })
	c.voiceM.Lock()
	c.voice = v
	c.voiceM.Unlock()
	return v
}

// requireNoFlow checks that no packet from userID reaches v anymore (after
// letting in-flight packets drain).
func requireNoFlow(t *testing.T, v *voiceclient.Client, userID string) {
	t.Helper()
	time.Sleep(300 * time.Millisecond)
	before := v.Received(userID)
	time.Sleep(600 * time.Millisecond)
	require.Equal(t, before, v.Received(userID), "packets from %s must not be forwarded", userID)
}

func requireFlow(t *testing.T, v *voiceclient.Client, userID string) {
	t.Helper()
	start := v.Received(userID)
	require.Eventually(t, func() bool { return v.Received(userID) > start+20 }, 10*time.Second, 50*time.Millisecond,
		"packets from %s should be forwarded", userID)
}

func TestE2E_VoiceMedia(t *testing.T) {
	if testing.Short() {
		t.Skip("WebRTC media test skipped in -short mode")
	}
	env := newTestServer(t)
	alice, bob := env.register("alice"), env.register("bob")
	lounge := env.createChannel(alice.AccessToken, "lounge", "voice")
	a, b := env.connect(alice), env.connect(bob)

	const loud uint8 = 30
	av := a.attachVoice(voiceclient.Options{Speak: true, Level: loud})
	bv := b.attachVoice(voiceclient.Options{})

	a.send("joinVoice", map[string]string{"channelId": lounge.ID})
	b.send("joinVoice", map[string]string{"channelId": lounge.ID})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go av.SendAudio(ctx)

	require.Eventually(t, func() bool { return av.Connected() && bv.Connected() }, 10*time.Second, 50*time.Millisecond)

	t.Run("audio is forwarded, track msid is the speaker's userId", func(t *testing.T) {
		requireFlow(t, bv, alice.ID)
	})

	t.Run("speaking detection", func(t *testing.T) {
		b.expect("voiceSpeaking", field("userId", alice.ID), field("speaking", true))
		av.SetLevel(voiceclient.LevelSilent)
		b.expect("voiceSpeaking", field("userId", alice.ID), field("speaking", false))
		av.SetLevel(loud)
		b.expect("voiceSpeaking", field("userId", alice.ID), field("speaking", true))
	})

	t.Run("mute is enforced server-side", func(t *testing.T) {
		// Alice keeps sending audio: the server must drop it.
		a.send("updateVoiceState", map[string]bool{"muted": true})
		b.expect("voiceStateUpdate", field("userId", alice.ID), field("muted", true), field("selfMuted", true))
		b.expect("voiceSpeaking", field("userId", alice.ID), field("speaking", false))
		requireNoFlow(t, bv, alice.ID)
		b.expectNone("voiceSpeaking", 200*time.Millisecond)

		a.send("updateVoiceState", map[string]bool{"muted": false})
		requireFlow(t, bv, alice.ID)
	})

	t.Run("deafened users receive nothing", func(t *testing.T) {
		b.send("updateVoiceState", map[string]bool{"deafened": true})
		b.expect("voiceStateUpdate", field("userId", bob.ID), field("deafened", true))
		requireNoFlow(t, bv, alice.ID)

		b.send("updateVoiceState", map[string]bool{"deafened": false})
		requireFlow(t, bv, alice.ID)
	})

	t.Run("a third participant triggers renegotiation", func(t *testing.T) {
		carol := env.register("carol")
		c := env.connect(carol)
		cv := c.attachVoice(voiceclient.Options{})
		c.send("joinVoice", map[string]string{"channelId": lounge.ID})
		requireFlow(t, cv, alice.ID)
		requireFlow(t, bv, alice.ID) // existing subscribers are unaffected
	})
}
