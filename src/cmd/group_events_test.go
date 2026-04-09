package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	wstypes "whatsapp-summarizer/src/types"
)

type groupEventsWhatsAppMock struct {
	mentionMessages []string
	plainMessages   []string
}

func (m *groupEventsWhatsAppMock) SendMessage(_ types.JID, message string) error {
	m.plainMessages = append(m.plainMessages, message)
	return nil
}

func (m *groupEventsWhatsAppMock) SendMessageReply(_ types.JID, _ types.MessageID, _ string) error {
	return nil
}

func (m *groupEventsWhatsAppMock) EditMessage(_ types.JID, _ types.MessageID, _ string) error {
	return nil
}

func (m *groupEventsWhatsAppMock) SendRawMessage(_ context.Context, _ types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
	return whatsmeow.SendResponse{}, nil
}

func (m *groupEventsWhatsAppMock) GetGroupInfo(_ context.Context, _ types.JID) (*types.GroupInfo, error) {
	return nil, nil
}

func (m *groupEventsWhatsAppMock) DownloadToFile(_ context.Context, _ whatsmeow.DownloadableMessage, _ *os.File) error {
	return nil
}

func (m *groupEventsWhatsAppMock) DownloadToMemory(_ context.Context, _ whatsmeow.DownloadableMessage) ([]byte, error) {
	return nil, nil
}

func (m *groupEventsWhatsAppMock) GetBotJID() types.JID { return types.JID{} }

func (m *groupEventsWhatsAppMock) SendMentionMessage(_ context.Context, _ types.JID, text string, _ []string) error {
	m.mentionMessages = append(m.mentionMessages, text)
	return nil
}

func (m *groupEventsWhatsAppMock) Connect(_ context.Context) error { return nil }
func (m *groupEventsWhatsAppMock) Disconnect()                     {}
func (m *groupEventsWhatsAppMock) IsConnected() bool               { return true }

func makeGroupEventsHandler(waSvc wstypes.WhatsAppService, welcome, farewell string) *Handler {
	return &Handler{
		config: &wstypes.Config{
			Bot: wstypes.BotConfig{
				WelcomeMessage:  welcome,
				FarewellMessage: farewell,
			},
		},
		whatsappService: waSvc,
		logger:          &noopLogger{},
		botStartTime:    time.Now().Add(-time.Minute),
		whitelistMap: map[string]bool{
			"120363000000000001": true,
		},
	}
}

func TestHandleGroupInfoEvent_JoinOnlySendsWelcome(t *testing.T) {
	waSvc := &groupEventsWhatsAppMock{}
	h := makeGroupEventsHandler(waSvc, "Seja bem-vindo(a), @numero!", "@numero saiu do grupo.")

	participant := types.JID{User: "5511999990001", Server: types.DefaultUserServer}
	evt := &events.GroupInfo{
		JID:       types.JID{User: "120363000000000001", Server: types.GroupServer},
		Timestamp: time.Now(),
		Join:      []types.JID{participant},
	}

	h.handleGroupInfoEvent(evt)

	if len(waSvc.mentionMessages) != 1 {
		t.Fatalf("expected 1 mention message for welcome, got %d", len(waSvc.mentionMessages))
	}
	if len(waSvc.plainMessages) != 0 {
		t.Fatalf("expected 0 plain messages, got %d", len(waSvc.plainMessages))
	}
	if !strings.Contains(waSvc.mentionMessages[0], "bem-vindo") {
		t.Fatalf("expected welcome message content, got %q", waSvc.mentionMessages[0])
	}
}

func TestHandleGroupInfoEvent_LeaveOnlySendsFarewell(t *testing.T) {
	waSvc := &groupEventsWhatsAppMock{}
	h := makeGroupEventsHandler(waSvc, "Seja bem-vindo(a), @numero!", "@numero saiu do grupo.")

	participant := types.JID{User: "5511999990001", Server: types.DefaultUserServer}
	evt := &events.GroupInfo{
		JID:       types.JID{User: "120363000000000001", Server: types.GroupServer},
		Timestamp: time.Now(),
		Leave:     []types.JID{participant},
	}

	h.handleGroupInfoEvent(evt)

	if len(waSvc.mentionMessages) != 1 {
		t.Fatalf("expected 1 mention message for farewell, got %d", len(waSvc.mentionMessages))
	}
	if !strings.Contains(waSvc.mentionMessages[0], "saiu do grupo") {
		t.Fatalf("expected farewell message content, got %q", waSvc.mentionMessages[0])
	}
}

func TestHandleGroupInfoEvent_LeaveAndJoinSameParticipantSendsOnlyFarewell(t *testing.T) {
	waSvc := &groupEventsWhatsAppMock{}
	h := makeGroupEventsHandler(waSvc, "Seja bem-vindo(a), @numero!", "@numero saiu do grupo.")

	participant := types.JID{User: "5511999990001", Server: types.DefaultUserServer}
	evt := &events.GroupInfo{
		JID:       types.JID{User: "120363000000000001", Server: types.GroupServer},
		Timestamp: time.Now(),
		Join:      []types.JID{participant},
		Leave:     []types.JID{participant},
	}

	h.handleGroupInfoEvent(evt)

	if len(waSvc.mentionMessages) != 1 {
		t.Fatalf("expected only 1 mention message, got %d", len(waSvc.mentionMessages))
	}
	if !strings.Contains(waSvc.mentionMessages[0], "saiu do grupo") {
		t.Fatalf("expected only farewell message content, got %q", waSvc.mentionMessages[0])
	}
}
