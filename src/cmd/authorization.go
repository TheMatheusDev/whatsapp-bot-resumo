package cmd

import (
	"context"
	"strings"
	"time"

	watypes "go.mau.fi/whatsmeow/types"
)

// groupInfoTTL is how long a cached GroupInfo entry is considered fresh.
// 6 hours balances staleness (admin promotions/demotions) against network
// cost and WhatsApp rate-limit risk.
const groupInfoTTL = 6 * time.Hour

// cachedGroupInfo is the value stored in groupInfoCache.
type cachedGroupInfo struct {
	info      *watypes.GroupInfo
	expiresAt time.Time
}


// isGroupAdmin checks if senderJIDUser is a native WhatsApp admin or superadmin
// of the given group.
//
// GroupInfo is cached per chat JID for groupInfoTTL (6 hours) to avoid a
// round-trip to the WhatsApp API on every admin command invocation.
// The cache is intentionally short-lived so that admin promotions/demotions
// take effect within minutes without requiring a bot restart.
// On any fetch error the function returns false (fail-safe: deny on doubt).
func (h *Handler) isGroupAdmin(ctx context.Context, chat watypes.JID, senderJIDUser string) bool {
	h.logger.Debug("isGroupAdmin: checking sender",
		"sender_jid_user", senderJIDUser,
		"bot_admins", h.config.WhatsApp.BotAdmins,
	)

	info := h.cachedGetGroupInfo(ctx, chat)

	// Resolve the sender's phone number from the participant list.
	// Needed because WhatsApp now sends LIDs (e.g. "160159498285172") as the
	// Sender.User instead of the actual phone number.
	senderPhone := senderJIDUser
	if info != nil {
		for _, p := range info.Participants {
			if p.JID.User == senderJIDUser || p.LID.User == senderJIDUser {
				if p.PhoneNumber.User != "" {
					senderPhone = p.PhoneNumber.User
					h.logger.Debug("isGroupAdmin: resolved sender phone",
						"lid", senderJIDUser,
						"phone", senderPhone,
					)
				}
				break
			}
		}
	}

	// Check the BotAdmins allowlist against both the raw JID and resolved phone.
	for _, jid := range h.config.WhatsApp.BotAdmins {
		trimmed := strings.TrimSpace(jid)
		if trimmed == senderJIDUser || trimmed == senderPhone {
			h.logger.Debug("isGroupAdmin: BotAdmins match", "entry", trimmed)
			return true
		}
	}

	// Check native group admin status.
	if info == nil {
		return false
	}
	for _, p := range info.Participants {
		isMatch := p.JID.User == senderJIDUser ||
			p.LID.User == senderJIDUser ||
			p.PhoneNumber.User == senderJIDUser
		if isMatch && (p.IsAdmin || p.IsSuperAdmin) {
			return true
		}
	}
	return false
}


// cachedGetGroupInfo returns GroupInfo from the in-memory cache when the entry
// is still within its TTL, fetching from the WhatsApp API otherwise.
// A sync.Mutex per cache slot is not needed here: the worst case of a
// cache miss race is two concurrent fetches for the same group, which is
// benign (both store the same data and the last write wins in sync.Map).
func (h *Handler) cachedGetGroupInfo(ctx context.Context, chat watypes.JID) *watypes.GroupInfo {
	key := chat.String()

	if v, ok := h.groupInfoCache.Load(key); ok {
		if entry, ok := v.(cachedGroupInfo); ok && time.Now().Before(entry.expiresAt) {
			return entry.info
		}
		// Stale entry — remove it so callers don't see ghost data after eviction.
		h.groupInfoCache.Delete(key)
	}

	info, err := h.whatsappService.GetGroupInfo(ctx, chat)
	if err != nil {
		h.logger.Warn("cachedGetGroupInfo: could not fetch group info",
			"error", err, "chat_id", key)
		return nil
	}

	h.groupInfoCache.Store(key, cachedGroupInfo{
		info:      info,
		expiresAt: time.Now().Add(groupInfoTTL),
	})
	return info
}

// invalidateGroupInfoCache evicts the GroupInfo cache entry for the given chat.
// Call this after any operation that may change participant roles (e.g. promote/demote).
func (h *Handler) invalidateGroupInfoCache(chatKey string) {
	h.groupInfoCache.Delete(chatKey)
}

// isCommand checks if a message is a bot command
func (h *Handler) isCommand(content string) bool {
	return strings.HasPrefix(content, "--") || strings.HasPrefix(content, "-") || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "/")
}
