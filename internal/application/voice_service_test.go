package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

func newVoice(max int) (*VoiceService, *fakeVoiceNotifier, *fakeMedia) {
	channels := newFakeChannelRepo(
		domain.Channel{ID: "lounge", Type: domain.ChannelTypeVoice},
		domain.Channel{ID: "gaming", Type: domain.ChannelTypeVoice},
		domain.Channel{ID: "general", Type: domain.ChannelTypeText},
	)
	n := &fakeVoiceNotifier{}
	m := newFakeMedia()
	return NewVoiceService(channels, n, m, max), n, m
}

func ptr(b bool) *bool { return &b }

func TestVoice_JoinValidation(t *testing.T) {
	svc, _, _ := newVoice(10)
	ctx := context.Background()

	require.ErrorIs(t, svc.Join(ctx, "alice", "s1", "missing"), ErrChannelNotFound)
	require.ErrorIs(t, svc.Join(ctx, "alice", "s1", "general"), ErrNotVoiceChannel)
	require.Empty(t, svc.Snapshot())
}

func TestVoice_JoinMoveLeave(t *testing.T) {
	svc, n, m := newVoice(10)
	ctx := context.Background()

	require.NoError(t, svc.Join(ctx, "alice", "s1", "lounge"))
	require.True(t, m.hasPeer("s1"))
	require.Equal(t, "lounge", n.lastState().ChannelID)

	require.NoError(t, svc.SetState("alice", "s1", ptr(true), nil))

	// Moving keeps mute/deafen.
	require.NoError(t, svc.Join(ctx, "alice", "s1", "gaming"))
	st := n.lastState()
	require.Equal(t, "gaming", st.ChannelID)
	require.True(t, st.SelfMuted)
	require.Empty(t, n.disconnects, "moving is not a kick")

	svc.Leave("alice", "s1")
	require.Equal(t, "", n.lastState().ChannelID, "leave is published with an empty channel")
	require.False(t, m.hasPeer("s1"))
	require.Empty(t, svc.Snapshot())
}

func TestVoice_OtherDeviceIsKicked(t *testing.T) {
	svc, n, m := newVoice(10)
	ctx := context.Background()

	require.NoError(t, svc.Join(ctx, "alice", "phone", "lounge"))
	require.NoError(t, svc.Join(ctx, "alice", "desktop", "lounge"))

	require.Equal(t, []disconnectCall{{"phone", domain.VoiceDisconnectOtherDevice}}, n.disconnects)
	require.False(t, m.hasPeer("phone"))
	require.True(t, m.hasPeer("desktop"))

	// The stale device can neither leave nor update the new session.
	svc.Leave("alice", "phone")
	require.Len(t, svc.Snapshot(), 1)
	require.ErrorIs(t, svc.SetState("alice", "phone", ptr(true), nil), ErrNotInVoice)
	require.ErrorIs(t, svc.HandleAnswer("alice", "phone", "sdp"), ErrNotInVoice)
	require.ErrorIs(t, svc.AddICECandidate("alice", "phone", domain.ICECandidate{}), ErrNotInVoice)
}

func TestVoice_Capacity(t *testing.T) {
	svc, _, _ := newVoice(2)
	ctx := context.Background()

	require.NoError(t, svc.Join(ctx, "alice", "s1", "lounge"))
	require.NoError(t, svc.Join(ctx, "bob", "s2", "lounge"))
	require.ErrorIs(t, svc.Join(ctx, "carol", "s3", "lounge"), ErrVoiceChannelFull)

	// A participant already inside doesn't count against themselves.
	require.NoError(t, svc.Join(ctx, "bob", "s4", "lounge"))
	require.NoError(t, svc.Join(ctx, "carol", "s3", "gaming"))
}

func TestVoice_MuteDeafen(t *testing.T) {
	svc, n, m := newVoice(10)
	require.NoError(t, svc.Join(context.Background(), "alice", "s1", "lounge"))

	require.ErrorIs(t, svc.SetState("bob", "s9", ptr(true), nil), ErrNotInVoice)

	require.NoError(t, svc.SetState("alice", "s1", nil, ptr(true)))
	st := n.lastState()
	require.True(t, st.Deafened)
	require.True(t, st.Muted(), "deafened implies muted")
	require.False(t, st.SelfMuted)
	require.Len(t, m.updated, 1, "media plane is told to stop forwarding")

	updates := len(n.states)
	require.NoError(t, svc.SetState("alice", "s1", nil, ptr(true)))
	require.Len(t, n.states, updates, "no-op updates are not published")
}

func TestVoice_Speaking(t *testing.T) {
	svc, n, _ := newVoice(10)
	require.NoError(t, svc.Join(context.Background(), "alice", "s1", "lounge"))

	svc.SpeakingChanged("s1", true)
	svc.SpeakingChanged("s1", true) // duplicate
	require.Equal(t, []speakingCall{{"alice", true}}, n.speaking)

	// Muting while speaking publishes speaking=false.
	require.NoError(t, svc.SetState("alice", "s1", ptr(true), nil))
	require.Equal(t, speakingCall{"alice", false}, n.speaking[len(n.speaking)-1])
	require.False(t, n.lastState().Speaking)

	// A muted user is never reported speaking.
	svc.SpeakingChanged("s1", true)
	require.Len(t, n.speaking, 2)

	svc.SpeakingChanged("unknown", true)
	require.Len(t, n.speaking, 2)
}

func TestVoice_PeerFailed(t *testing.T) {
	svc, n, _ := newVoice(10)
	require.NoError(t, svc.Join(context.Background(), "alice", "s1", "lounge"))

	svc.PeerFailed("s1")
	require.Equal(t, []disconnectCall{{"s1", domain.VoiceDisconnectConnectionFailed}}, n.disconnects)
	require.Empty(t, svc.Snapshot())
}

func TestVoice_ChannelDeletedKicksEveryone(t *testing.T) {
	svc, n, _ := newVoice(10)
	ctx := context.Background()
	require.NoError(t, svc.Join(ctx, "alice", "s1", "lounge"))
	require.NoError(t, svc.Join(ctx, "bob", "s2", "lounge"))
	require.NoError(t, svc.Join(ctx, "carol", "s3", "gaming"))

	svc.ChannelDeleted("lounge")
	require.ElementsMatch(t, []disconnectCall{
		{"s1", domain.VoiceDisconnectChannelDeleted},
		{"s2", domain.VoiceDisconnectChannelDeleted},
	}, n.disconnects)
	require.Len(t, svc.Snapshot(), 1)
}

func TestVoice_AddPeerFailure(t *testing.T) {
	svc, _, m := newVoice(10)
	m.addErr = errors.New("boom")

	err := svc.Join(context.Background(), "alice", "s1", "lounge")
	require.Error(t, err)
	require.Empty(t, svc.Snapshot(), "no state left behind when the media plane fails")
}
