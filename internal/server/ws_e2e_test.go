package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/config"
)

func TestE2E_WebSocketHandshake(t *testing.T) {
	env := newTestServer(t)
	alice := env.register("alice")

	t.Run("bearer header", func(t *testing.T) {
		env.connect(alice)
	})

	t.Run("token query param", func(t *testing.T) {
		conn, _, err := env.dialRaw(env.wsURL()+"?token="+url.QueryEscape(alice.AccessToken), nil)
		require.NoError(t, err)
		conn.Close()
	})

	rejected := map[string]string{
		"no token":      "",
		"refresh token": alice.RefreshToken,
		"garbage":       "garbage",
	}
	for name, tok := range rejected {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			if tok != "" {
				h.Set("Authorization", "Bearer "+tok)
			}
			_, status, err := env.dialRaw(env.wsURL(), h)
			require.Error(t, err)
			require.Equal(t, http.StatusUnauthorized, status)
		})
	}
}

func TestE2E_Chat(t *testing.T) {
	env := newTestServer(t)
	alice, bob := env.register("alice"), env.register("bob")
	general := env.createChannel(alice.AccessToken, "general", "text")
	a, b := env.connect(alice), env.connect(bob)

	a.send("sendMessage", map[string]string{"channelId": general.ID, "content": "hello"}, "req-1")
	var created struct{ ID, UserID, ChannelID, Content, CreatedAt, UpdatedAt string }
	b.expect("messageCreated").decode(t, &created)
	require.Equal(t, alice.ID, created.UserID)
	require.Equal(t, general.ID, created.ChannelID)
	require.Equal(t, "hello", created.Content)
	ev := a.expect("messageCreated")
	require.Empty(t, ev.RequestID, "broadcasts carry no requestId; the sender gets the same broadcast")

	// Persisted: visible in REST history.
	var history []map[string]any
	env.call(http.MethodGet, "/api/channels/"+general.ID+"/messages", bob.AccessToken, nil, http.StatusOK, &history)
	require.Len(t, history, 1)

	t.Run("only the author can edit or delete", func(t *testing.T) {
		b.send("editMessage", map[string]string{"messageId": created.ID, "content": "hacked"}, "req-2")
		b.expectError("FORBIDDEN", "req-2")
		b.send("deleteMessage", map[string]string{"messageId": created.ID}, "req-3")
		b.expectError("FORBIDDEN", "req-3")
	})

	t.Run("edit and delete", func(t *testing.T) {
		a.send("editMessage", map[string]string{"messageId": created.ID, "content": "hello!"})
		b.expect("messageUpdated", field("id", created.ID), field("content", "hello!"))

		a.send("deleteMessage", map[string]string{"messageId": created.ID})
		b.expect("messageDeleted", field("messageId", created.ID))
	})

	t.Run("errors", func(t *testing.T) {
		a.sendRaw("{not json")
		a.expectError("PARSE_ERROR", "")

		a.send("doSomething", map[string]string{}, "req-4")
		a.expectError("UNKNOWN_EVENT", "req-4")

		a.send("sendMessage", map[string]string{"channelId": general.ID}, "req-5")
		a.expectError("VALIDATION_ERROR", "req-5")

		a.send("sendMessage", map[string]string{"channelId": "nope", "content": "x"}, "req-6")
		a.expectError("NOT_FOUND", "req-6")

		a.send("editMessage", map[string]string{"messageId": "nope", "content": "x"}, "req-7")
		a.expectError("NOT_FOUND", "req-7")
	})
}

func TestE2E_Presence(t *testing.T) {
	env := newTestServer(t)
	alice, bob := env.register("alice"), env.register("bob")

	b := env.connect(bob)
	a := env.connect(alice)
	b.expect("presenceBroadcast", field("userId", alice.ID), field("status", "online"))

	t.Run("snapshot", func(t *testing.T) {
		conn, _, err := env.dialRaw(env.wsURL(), http.Header{"Authorization": {"Bearer " + bob.AccessToken}})
		require.NoError(t, err)
		defer conn.Close()
		var snap struct {
			Type string
			Data struct {
				Presences []map[string]any
			}
		}
		require.NoError(t, conn.ReadJSON(&snap))
		require.Equal(t, "presenceSnapshot", snap.Type)
		ids := map[string]map[string]any{}
		for _, p := range snap.Data.Presences {
			ids[p["userId"].(string)] = p
		}
		require.Contains(t, ids, alice.ID)
		require.NotContains(t, ids[alice.ID], "invisible", "never exposed for other users")
		require.Equal(t, false, ids[bob.ID]["invisible"], "own entry carries invisible")
	})

	t.Run("invisible", func(t *testing.T) {
		a.send("presenceUpdate", map[string]bool{"invisible": true})
		b.expect("presenceBroadcast", field("userId", alice.ID), field("status", "offline"))
		a.expect("presenceBroadcast", field("userId", alice.ID), field("status", "online"), field("invisible", true))

		var me map[string]any
		env.call(http.MethodGet, "/api/users/me", alice.AccessToken, nil, http.StatusOK, &me)

		a.send("presenceUpdate", map[string]bool{"invisible": false})
		b.expect("presenceBroadcast", field("userId", alice.ID), field("status", "online"))

		a.send("presenceUpdate", map[string]string{}, "req-1")
		a.expectError("VALIDATION_ERROR", "req-1")
	})

	t.Run("offline after the grace period", func(t *testing.T) {
		a.conn.Close()
		b.expect("presenceBroadcast", field("userId", alice.ID), field("status", "offline"))
	})
}

func TestE2E_VoiceSignaling(t *testing.T) {
	env := newTestServer(t, func(c *config.Config) { c.VoiceMaxParticipants = 2 })
	alice, bob, carol := env.register("alice"), env.register("bob"), env.register("carol")
	tok := alice.AccessToken
	general := env.createChannel(tok, "general", "text")
	lounge := env.createChannel(tok, "lounge", "voice")
	a, b, c := env.connect(alice), env.connect(bob), env.connect(carol)

	a.send("joinVoice", map[string]string{"channelId": general.ID}, "r1")
	a.expectError("NOT_VOICE_CHANNEL", "r1")
	a.send("joinVoice", map[string]string{"channelId": "nope"}, "r2")
	a.expectError("NOT_FOUND", "r2")
	a.send("updateVoiceState", map[string]bool{"muted": true}, "r3")
	a.expectError("NOT_IN_VOICE", "r3")
	a.send("voiceAnswer", map[string]string{"sdp": "v=0"}, "r4")
	a.expectError("NOT_IN_VOICE", "r4")

	a.send("joinVoice", map[string]string{"channelId": lounge.ID})
	a.expect("voiceOffer")
	c.expect("voiceStateUpdate", field("userId", alice.ID), field("channelId", lounge.ID), field("muted", false))

	a.send("voiceAnswer", map[string]string{"sdp": "garbage"}, "r5")
	a.expectError("INVALID_SIGNALING", "r5")

	b.send("joinVoice", map[string]string{"channelId": lounge.ID})
	c.expect("voiceStateUpdate", field("userId", bob.ID), field("channelId", lounge.ID))
	// No renegotiation offer for alice yet: her first offer is still unanswered
	// (offers are serialized per peer). Covered by TestE2E_VoiceMedia.

	t.Run("channel full", func(t *testing.T) {
		c.send("joinVoice", map[string]string{"channelId": lounge.ID}, "r6")
		c.expectError("VOICE_CHANNEL_FULL", "r6")
	})

	t.Run("deafen implies muted", func(t *testing.T) {
		b.send("updateVoiceState", map[string]bool{"deafened": true})
		c.expect("voiceStateUpdate", field("userId", bob.ID), field("deafened", true), field("muted", true), field("selfMuted", false))
	})

	t.Run("snapshot for new connections", func(t *testing.T) {
		conn, _, err := env.dialRaw(env.wsURL(), http.Header{"Authorization": {"Bearer " + carol.AccessToken}})
		require.NoError(t, err)
		defer conn.Close()
		var ev struct {
			Type string
			Data struct{ States []map[string]any }
		}
		require.NoError(t, conn.ReadJSON(&ev)) // presenceSnapshot
		require.NoError(t, conn.ReadJSON(&ev))
		require.Equal(t, "voiceStateSnapshot", ev.Type)
		require.Len(t, ev.Data.States, 2)
	})

	t.Run("other device", func(t *testing.T) {
		a2 := env.connect(alice)
		a2.send("joinVoice", map[string]string{"channelId": lounge.ID})
		a.expect("voiceDisconnected", field("reason", "otherDevice"))
		a2.expect("voiceOffer")
		c.expect("voiceStateUpdate", field("userId", alice.ID), field("channelId", lounge.ID))
		// The kicked session can no longer act on voice.
		a.send("leaveVoice", map[string]string{})
		c.expectNone("voiceStateUpdate", 200*time.Millisecond)
	})

	t.Run("leave", func(t *testing.T) {
		b.send("leaveVoice", map[string]string{})
		c.expect("voiceStateUpdate", field("userId", bob.ID), field("channelId", nil))
	})

	t.Run("channel deleted", func(t *testing.T) {
		env.call(http.MethodDelete, "/api/channels/"+lounge.ID, tok, nil, http.StatusNoContent, nil)
		c.expect("voiceStateUpdate", field("userId", alice.ID), field("channelId", nil))
	})
}

func TestE2E_VoiceLeaveOnDisconnect(t *testing.T) {
	env := newTestServer(t)
	alice, bob := env.register("alice"), env.register("bob")
	lounge := env.createChannel(alice.AccessToken, "lounge", "voice")
	a, b := env.connect(alice), env.connect(bob)

	a.send("joinVoice", map[string]string{"channelId": lounge.ID})
	b.expect("voiceStateUpdate", field("userId", alice.ID), field("channelId", lounge.ID))
	a.conn.Close()
	b.expect("voiceStateUpdate", field("userId", alice.ID), field("channelId", nil))
}

func TestE2E_VoiceKickOnChannelDelete(t *testing.T) {
	env := newTestServer(t)
	alice := env.register("alice")
	lounge := env.createChannel(alice.AccessToken, "lounge", "voice")
	a := env.connect(alice)

	a.send("joinVoice", map[string]string{"channelId": lounge.ID})
	a.expect("voiceOffer")
	env.call(http.MethodDelete, "/api/channels/"+lounge.ID, alice.AccessToken, nil, http.StatusNoContent, nil)
	a.expect("voiceDisconnected", field("reason", "channelDeleted"))
}
