package fbhttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

func TestCommandsHandlerLimitsWebSocketMessageSize(t *testing.T) {
	root := t.TempDir()
	key := []byte("test-signing-key")
	perm := users.Permissions{Execute: true}
	st := scopedUserStorage(t, root, perm, key)
	server := httptest.NewServer(handle(commandsHandler, "", st, &settings.Server{EnableExec: true}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	serverURL.Scheme = "ws"
	requestHeader := http.Header{"X-Auth": []string{signToken(t, perm, key)}}
	conn, _, err := websocket.DefaultDialer.Dial(serverURL.String(), requestHeader)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), maxCommandMessageSize+1)); err != nil {
		t.Fatalf("write oversized command: %v", err)
	}
	_, _, err = conn.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok || closeErr.Code != websocket.CloseMessageTooBig {
		t.Fatalf("oversized command error = %v, want WebSocket close 1009", err)
	}
}
