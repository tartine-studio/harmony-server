package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestE2E_Auth(t *testing.T) {
	env := newTestServer(t)
	alice := env.register("alice")
	require.NotEmpty(t, alice.ID)

	t.Run("register validation", func(t *testing.T) {
		env.expectError(http.MethodPost, "/api/auth/register", "", map[string]string{
			"username": "a", "email": "x@example.com", "password": "password123",
		}, http.StatusBadRequest, "VALIDATION_ERROR")
		env.expectError(http.MethodPost, "/api/auth/register", "", map[string]string{
			"username": "bob", "email": "not-an-email", "password": "password123",
		}, http.StatusBadRequest, "VALIDATION_ERROR")
		env.expectError(http.MethodPost, "/api/auth/register", "", map[string]string{
			"username": "bob", "email": "bob@example.com", "password": "short",
		}, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("duplicate email is not revealed", func(t *testing.T) {
		env.expectError(http.MethodPost, "/api/auth/register", "", map[string]string{
			"username": "alice2", "email": alice.Email, "password": "password123",
		}, http.StatusBadRequest, "REGISTRATION_FAILED")
	})

	t.Run("bad credentials", func(t *testing.T) {
		env.expectError(http.MethodPost, "/api/auth/login", "", map[string]string{
			"email": alice.Email, "password": "wrong-password",
		}, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	})

	t.Run("refresh", func(t *testing.T) {
		var pair struct{ AccessToken, RefreshToken string }
		env.call(http.MethodPost, "/api/auth/refresh", "", map[string]string{"refreshToken": alice.RefreshToken}, http.StatusOK, &pair)
		require.NotEmpty(t, pair.AccessToken)
		require.NotEmpty(t, pair.RefreshToken)

		env.expectError(http.MethodPost, "/api/auth/refresh", "", map[string]string{"refreshToken": alice.AccessToken},
			http.StatusUnauthorized, "INVALID_TOKEN")
	})

	t.Run("protected routes", func(t *testing.T) {
		env.expectError(http.MethodGet, "/api/users/me", "", nil, http.StatusUnauthorized, "UNAUTHORIZED")
		env.expectError(http.MethodGet, "/api/users/me", "garbage", nil, http.StatusUnauthorized, "UNAUTHORIZED")
		env.expectError(http.MethodGet, "/api/users/me", alice.RefreshToken, nil, http.StatusUnauthorized, "UNAUTHORIZED")

		var me map[string]any
		env.call(http.MethodGet, "/api/users/me", alice.AccessToken, nil, http.StatusOK, &me)
		require.Equal(t, alice.ID, me["id"])
		require.Equal(t, "alice", me["username"])
		require.NotContains(t, me, "password")
	})
}

func TestE2E_UsersAndChannels(t *testing.T) {
	env := newTestServer(t)
	alice := env.register("alice")
	bob := env.register("bob")
	tok := alice.AccessToken

	var users []map[string]any
	env.call(http.MethodGet, "/api/users", tok, nil, http.StatusOK, &users)
	require.Len(t, users, 2)

	var me map[string]any
	env.call(http.MethodPatch, "/api/users/me", tok, map[string]string{"username": "Alice"}, http.StatusOK, &me)
	require.Equal(t, "Alice", me["username"])
	env.expectError(http.MethodGet, "/api/users/nope", tok, nil, http.StatusNotFound, "NOT_FOUND")

	general := env.createChannel(tok, "general", "text")
	require.Equal(t, "text", general.Type)
	env.expectError(http.MethodPost, "/api/channels", tok, map[string]string{"name": "x", "type": "video"},
		http.StatusBadRequest, "VALIDATION_ERROR")

	var renamed channel
	env.call(http.MethodPatch, "/api/channels/"+general.ID, tok, map[string]string{"name": "lobby"}, http.StatusOK, &renamed)
	require.Equal(t, "lobby", renamed.Name)

	var list []channel
	env.call(http.MethodGet, "/api/channels", tok, nil, http.StatusOK, &list)
	require.Len(t, list, 1)

	env.call(http.MethodDelete, "/api/channels/"+general.ID, tok, nil, http.StatusNoContent, nil)
	env.expectError(http.MethodGet, "/api/channels/"+general.ID, tok, nil, http.StatusNotFound, "NOT_FOUND")

	t.Run("email already taken", func(t *testing.T) {
		env.expectError(http.MethodPatch, "/api/users/me", tok, map[string]string{"email": bob.Email},
			http.StatusBadRequest, "UPDATE_FAILED")
		env.expectError(http.MethodPatch, "/api/users/"+bob.ID, tok, map[string]string{"email": alice.Email},
			http.StatusBadRequest, "UPDATE_FAILED")
		env.call(http.MethodPatch, "/api/users/me", tok, map[string]string{"email": alice.Email}, http.StatusOK, nil)
	})

	t.Run("deleting a user keeps their messages", func(t *testing.T) {
		random := env.createChannel(tok, "random", "text")
		path := "/api/channels/" + random.ID + "/messages"
		env.call(http.MethodPost, path, bob.AccessToken, map[string]string{"content": "bye"}, http.StatusCreated, nil)

		// No roles yet: any user can delete any user.
		env.call(http.MethodDelete, "/api/users/"+bob.ID, tok, nil, http.StatusNoContent, nil)

		var history []map[string]any
		env.call(http.MethodGet, path, tok, nil, http.StatusOK, &history)
		require.Len(t, history, 1)
		require.Contains(t, history[0], "userId")
		require.Nil(t, history[0]["userId"], "deleted author is null")
		require.Equal(t, "bye", history[0]["content"])
	})
}

func TestE2E_MessagesREST(t *testing.T) {
	env := newTestServer(t)
	alice := env.register("alice")
	bob := env.register("bob")
	general := env.createChannel(alice.AccessToken, "general", "text")
	path := "/api/channels/" + general.ID + "/messages"

	type message struct {
		ID, UserID, ChannelID, Content, CreatedAt, UpdatedAt string
	}
	var m message
	env.call(http.MethodPost, path, alice.AccessToken, map[string]string{"content": "hello"}, http.StatusCreated, &m)
	require.Equal(t, alice.ID, m.UserID)
	require.Equal(t, general.ID, m.ChannelID)
	require.NotEmpty(t, m.CreatedAt)

	var history []message
	env.call(http.MethodGet, path, bob.AccessToken, nil, http.StatusOK, &history)
	require.Len(t, history, 1)

	env.expectError(http.MethodPatch, path+"/"+m.ID, bob.AccessToken, map[string]string{"content": "x"}, http.StatusForbidden, "FORBIDDEN")
	env.expectError(http.MethodDelete, path+"/"+m.ID, bob.AccessToken, nil, http.StatusForbidden, "FORBIDDEN")
	env.expectError(http.MethodPost, path, alice.AccessToken, map[string]string{"content": ""}, http.StatusBadRequest, "VALIDATION_ERROR")
	env.expectError(http.MethodGet, "/api/channels/nope/messages", alice.AccessToken, nil, http.StatusNotFound, "NOT_FOUND")

	env.call(http.MethodPatch, path+"/"+m.ID, alice.AccessToken, map[string]string{"content": "edited"}, http.StatusOK, &m)
	require.Equal(t, "edited", m.Content)
	env.call(http.MethodDelete, path+"/"+m.ID, alice.AccessToken, nil, http.StatusNoContent, nil)
	env.expectError(http.MethodDelete, path+"/"+m.ID, alice.AccessToken, nil, http.StatusNotFound, "NOT_FOUND")
}
