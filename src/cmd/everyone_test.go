package cmd

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow"

	wstypes "whatsapp-summarizer/src/types"
)

// mockWhatsAppService is a minimal WhatsAppService mock for testing.
type mockWhatsAppService struct {
	groupInfo *types.GroupInfo
	groupErr  error
}

func (m *mockWhatsAppService) SendMessage(_ types.JID, _ string) error                { return nil }
func (m *mockWhatsAppService) SendMessageReply(_ types.JID, _ types.MessageID, _ string) error {
	return nil
}
func (m *mockWhatsAppService) EditMessage(_ types.JID, _ types.MessageID, _ string) error {
	return nil
}
func (m *mockWhatsAppService) SendRawMessage(_ context.Context, _ types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
	return whatsmeow.SendResponse{}, nil
}
func (m *mockWhatsAppService) GetGroupInfo(_ context.Context, _ types.JID) (*types.GroupInfo, error) {
	return m.groupInfo, m.groupErr
}
func (m *mockWhatsAppService) DownloadToFile(_ context.Context, _ whatsmeow.DownloadableMessage, _ *os.File) error {
	return nil
}
func (m *mockWhatsAppService) DownloadToMemory(_ context.Context, _ whatsmeow.DownloadableMessage) ([]byte, error) {
	return nil, nil
}
func (m *mockWhatsAppService) GetBotJID() types.JID { return types.JID{} }
func (m *mockWhatsAppService) SendMentionMessage(_ context.Context, _ types.JID, _ string, _ []string) error {
	return nil
}
func (m *mockWhatsAppService) Connect(_ context.Context) error { return nil }
func (m *mockWhatsAppService) Disconnect()                     {}
func (m *mockWhatsAppService) IsConnected() bool               { return true }

// helpers

func makeHandler(admins []string, svc wstypes.WhatsAppService) *EveryoneHandler {
	cfg := &wstypes.Config{
		WhatsApp: wstypes.WhatsAppConfig{
			EveryoneAdmins: admins,
		},
	}
	return NewEveryoneHandler(cfg, &noopLogger{}, time.UTC, svc)
}

func makeGroupInfo(participants []types.GroupParticipant) *types.GroupInfo {
	return &types.GroupInfo{Participants: participants}
}

func participant(jidUser string, isAdmin, isSuperAdmin bool) types.GroupParticipant {
	return types.GroupParticipant{
		JID:          types.JID{User: jidUser},
		IsAdmin:      isAdmin,
		IsSuperAdmin: isSuperAdmin,
	}
}

var testChat = types.JID{Server: "g.us", User: "120363000000000001"}

type noopLogger struct{}

func (n *noopLogger) Debug(_ string, _ ...interface{}) {}
func (n *noopLogger) Info(_ string, _ ...interface{})  {}
func (n *noopLogger) Warn(_ string, _ ...interface{})  {}
func (n *noopLogger) Error(_ string, _ ...interface{}) {}
func (n *noopLogger) Fatal(_ string, _ ...interface{}) {}

// Tests

func TestIsEveryoneAdmin_NativeGroupAdmin(t *testing.T) {
	svc := &mockWhatsAppService{
		groupInfo: makeGroupInfo([]types.GroupParticipant{
			participant("5511999990001", true, false),
			participant("5511999990002", false, false),
		}),
	}
	h := makeHandler(nil, svc)

	if !h.IsEveryoneAdmin(context.Background(), testChat, "5511999990001") {
		t.Error("expected native group admin to be authorized")
	}
}

func TestIsEveryoneAdmin_NativeSuperAdmin(t *testing.T) {
	svc := &mockWhatsAppService{
		groupInfo: makeGroupInfo([]types.GroupParticipant{
			participant("5511999990001", false, true),
		}),
	}
	h := makeHandler(nil, svc)

	if !h.IsEveryoneAdmin(context.Background(), testChat, "5511999990001") {
		t.Error("expected superadmin to be authorized")
	}
}

func TestIsEveryoneAdmin_ConfigAllowlist(t *testing.T) {
	// Sender is NOT a group admin, but is in the config allowlist by JID
	svc := &mockWhatsAppService{
		groupInfo: makeGroupInfo([]types.GroupParticipant{
			participant("5511999990099", false, false),
		}),
	}
	h := makeHandler([]string{"5511999990099"}, svc)

	if !h.IsEveryoneAdmin(context.Background(), testChat, "5511999990099") {
		t.Error("expected JID in config allowlist to be authorized")
	}
}

func TestIsEveryoneAdmin_NotAdmin(t *testing.T) {
	svc := &mockWhatsAppService{
		groupInfo: makeGroupInfo([]types.GroupParticipant{
			participant("5511999990001", false, false),
		}),
	}
	h := makeHandler(nil, svc)

	if h.IsEveryoneAdmin(context.Background(), testChat, "5511999990001") {
		t.Error("expected non-admin with no config allowlist to be denied")
	}
}

func TestIsEveryoneAdmin_EmptyAllowlist_NonAdmin(t *testing.T) {
	// Empty EVERYONE_ADMINS + non-admin should be denied (no longer allows everyone)
	svc := &mockWhatsAppService{
		groupInfo: makeGroupInfo([]types.GroupParticipant{
			participant("5511999990001", false, false),
		}),
	}
	h := makeHandler([]string{}, svc)

	if h.IsEveryoneAdmin(context.Background(), testChat, "5511999990001") {
		t.Error("expected non-admin with empty allowlist to be denied")
	}
}

func TestIsEveryoneAdmin_GroupInfoError_FallsBackToAllowlist(t *testing.T) {
	// GetGroupInfo fails — only config allowlist should work
	svc := &mockWhatsAppService{
		groupErr: fmt.Errorf("network error"),
	}
	h := makeHandler([]string{"5511999990001"}, svc)

	if !h.IsEveryoneAdmin(context.Background(), testChat, "5511999990001") {
		t.Error("expected config allowlist to still work when GetGroupInfo fails")
	}
}

func TestIsEveryoneAdmin_GroupInfoError_NoAllowlist_Denied(t *testing.T) {
	svc := &mockWhatsAppService{
		groupErr: fmt.Errorf("network error"),
	}
	h := makeHandler(nil, svc)

	if h.IsEveryoneAdmin(context.Background(), testChat, "5511999990001") {
		t.Error("expected denial when GetGroupInfo fails and no config allowlist")
	}
}
