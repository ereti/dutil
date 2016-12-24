package dstate

import (
	"github.com/jonas747/discordgo"
	"sync"
	"time"
)

type GuildState struct {
	sync.RWMutex

	// The underlying guild, the members and channels fields shouldnt be used
	Guild *discordgo.Guild

	Members  map[string]*MemberState
	Channels map[string]*ChannelState

	maxMessages           int           // Absolute max number of messages cached in a channel
	maxMessageDuration    time.Duration // Max age of messages, if 0 ignored. (Only checks age whena new message is received on the channel)
	removeDeletedMessages bool
}

func NewGuildState(guild *discordgo.Guild, maxMessages int, maxMessageDuration time.Duration, removeDeletedMessages bool) *GuildState {
	guildState := &GuildState{
		Guild:                 guild,
		Members:               make(map[string]*MemberState),
		Channels:              make(map[string]*ChannelState),
		maxMessages:           maxMessages,
		maxMessageDuration:    maxMessageDuration,
		removeDeletedMessages: removeDeletedMessages,
	}

	for _, channel := range guild.Channels {
		guildState.ChannelAddUpdate(false, channel)
	}

	for _, member := range guild.Members {
		guildState.MemberAddUpdate(false, member)
	}

	for _, presence := range guild.Presences {
		guildState.PresenceAddUpdate(false, presence)
	}

	return guildState
}

func (g *GuildState) GuildUpdate(lock bool, newGuild *discordgo.Guild) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	if newGuild.Roles == nil {
		newGuild.Roles = g.Guild.Roles
	}
	if newGuild.Emojis == nil {
		newGuild.Emojis = g.Guild.Emojis
	}
	if newGuild.Members == nil {
		newGuild.Members = g.Guild.Members
	}
	if newGuild.Presences == nil {
		newGuild.Presences = g.Guild.Presences
	}
	if newGuild.Channels == nil {
		newGuild.Channels = g.Guild.Channels
	}
	if newGuild.VoiceStates == nil {
		newGuild.VoiceStates = g.Guild.VoiceStates
	}

	*g.Guild = *newGuild
}

func (g *GuildState) MemberAddUpdate(lock bool, newMember *discordgo.Member) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	existing, ok := g.Members[newMember.User.ID]
	if ok {
		if existing.Member == nil {
			existing.Member = newMember
		} else {
			// Patch
			if newMember.JoinedAt != "" {
				existing.Member.JoinedAt = newMember.JoinedAt
			}
			if newMember.Roles != nil {
				existing.Member.Roles = newMember.Roles
			}

			// Seems to always be provided
			existing.Member.Nick = newMember.Nick
			existing.Member.User = newMember.User
		}
	} else {
		g.Members[newMember.User.ID] = &MemberState{
			Member: newMember,
		}
	}
}

func (g *GuildState) MemberRemove(lock bool, id string) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}
	delete(g.Members, id)
}

func (g *GuildState) PresenceAddUpdate(lock bool, newPresence *discordgo.Presence) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	existing, ok := g.Members[newPresence.User.ID]
	if ok {
		if existing.Presence == nil {
			existing.Presence = newPresence
		} else {
			// Patch

			// Nil games indicates them not playing anything, so this had to always be provided?
			// IDK the docs dosen't seem to correspond to the actual results very well
			existing.Presence.Game = newPresence.Game

			if newPresence.Status != "" {
				existing.Presence.Status = newPresence.Status
			}
		}
	} else {
		g.Members[newPresence.User.ID] = &MemberState{
			Presence: newPresence,
		}
	}
}

func (g *GuildState) Channel(lock bool, id string) *ChannelState {
	if lock {
		g.RLock()
		defer g.RUnlock()
	}

	return g.Channels[id]
}

func (g *GuildState) ChannelAddUpdate(lock bool, newChannel *discordgo.Channel) *ChannelState {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	existing, ok := g.Channels[newChannel.ID]
	if ok {
		// Patch
		if newChannel.PermissionOverwrites == nil {
			newChannel.PermissionOverwrites = existing.Channel.PermissionOverwrites
		}
		if newChannel.IsPrivate && newChannel.Recipient == nil {
			newChannel.Recipient = existing.Channel.Recipient
		}
		*existing.Channel = *newChannel
		return existing
	}

	state := &ChannelState{
		Channel:  newChannel,
		Messages: make([]*MessageState, 0),
		Guild:    g,
	}

	g.Channels[newChannel.ID] = state
	return state
}

func (g *GuildState) ChannelRemove(lock bool, id string) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}
	delete(g.Channels, id)
}

func (g *GuildState) Role(lock bool, id string) *discordgo.Role {
	if lock {
		g.RLock()
		defer g.RUnlock()
	}

	for _, role := range g.Guild.Roles {
		if role.ID == id {
			return role
		}
	}

	return nil
}

func (g *GuildState) RoleAddUpdate(lock bool, newRole *discordgo.Role) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	existing := g.Role(false, newRole.ID)
	if existing != nil {
		*existing = *newRole
	} else {
		g.Guild.Roles = append(g.Guild.Roles, newRole)
	}
}

func (g *GuildState) RoleRemove(lock bool, id string) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	for i, v := range g.Guild.Roles {
		if v.ID == id {
			g.Guild.Roles = append(g.Guild.Roles[:i], g.Guild.Roles[i+1:]...)
			return
		}
	}
}

func (g *GuildState) VoiceState(lock bool, userID string) *discordgo.VoiceState {
	if lock {
		g.RLock()
		defer g.RUnlock()
	}

	for _, v := range g.Guild.VoiceStates {
		if v.UserID == userID {
			return v
		}
	}

	return nil
}

func (g *GuildState) VoiceStateUpdate(lock bool, update *discordgo.VoiceStateUpdate) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	// Handle Leaving Channel
	if update.ChannelID == "" {
		for i, state := range g.Guild.VoiceStates {
			if state.UserID == update.UserID {
				g.Guild.VoiceStates = append(g.Guild.VoiceStates[:i], g.Guild.VoiceStates[i+1:]...)
				return
			}
		}
	}

	existing := g.VoiceState(false, update.UserID)
	if existing != nil {
		*existing = *update.VoiceState
		return
	}

	g.Guild.VoiceStates = append(g.Guild.VoiceStates, update.VoiceState)

	return
}

func (g *GuildState) MessageAddUpdate(lock bool, msg *discordgo.Message) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	channel := g.Channels[msg.ChannelID]
	if channel == nil {
		// TODO: Log this somewhere
		return
	}

	existing := channel.Message(msg.ID)
	if existing != nil {
		// Patch the existing message
		if msg.Content != "" {
			existing.Message.Content = msg.Content
		}
		if msg.EditedTimestamp != "" {
			existing.Message.EditedTimestamp = msg.EditedTimestamp
		}
		if msg.Mentions != nil {
			existing.Message.Mentions = msg.Mentions
		}
		if msg.Embeds != nil {
			existing.Message.Embeds = msg.Embeds
		}
		if msg.Attachments != nil {
			existing.Message.Attachments = msg.Attachments
		}
		if msg.Timestamp != "" {
			existing.Message.Timestamp = msg.Timestamp
		}
		if msg.Author != nil {
			existing.Message.Author = msg.Author
		}
		existing.ParseTimes()
	} else {
		// Add the new one
		ms := &MessageState{
			Message: msg,
		}
		ms.ParseTimes()
		channel.Messages = append(channel.Messages, ms)
		if len(channel.Messages) > g.maxMessages {
			channel.Messages = channel.Messages[len(channel.Messages)-g.maxMessages:]
		}
	}

	// Check age
	if g.maxMessageDuration == 0 {
		return
	}

	now := time.Now()
	for i := len(channel.Messages) - 1; i >= 0; i-- {
		m := channel.Messages[i]

		ts := m.ParsedCreated
		if ts.IsZero() {
			continue
		}

		if now.Sub(ts) > g.maxMessageDuration {
			// All messages before this is old aswell
			// TODO: remove by edited timestamp if set
			channel.Messages = channel.Messages[i:]
			break
		}
	}
}

func (g *GuildState) MessageRemove(lock bool, channelID, messageID string) {
	if lock {
		g.Lock()
		defer g.Unlock()
	}

	channel := g.Channel(lock, channelID)
	if channel == nil {
		return
	}

	for i, ms := range channel.Messages {
		if ms.Message.ID == messageID {
			if g.removeDeletedMessages {
				channel.Messages = append(channel.Messages[:i], channel.Messages[i+1:]...)
			} else {
				ms.Deleted = true
			}
			return
		}
	}
}
