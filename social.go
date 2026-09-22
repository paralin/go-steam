package steam

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"sync"
	"time"

	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
	. "github.com/paralin/go-steam/rwu"
	"github.com/paralin/go-steam/socialcache"
	"github.com/paralin/go-steam/steamid"
)

// Social provides access to social aspects of Steam.
type Social struct {
	// Friends retains observed individual relationships.
	Friends *socialcache.FriendsList
	// Groups retains observed clan relationships.
	Groups *socialcache.GroupsList
	// Chats retains observed room membership.
	Chats *socialcache.ChatsList

	// client owns the Steam transport and event dispatcher.
	client *Client
	// mutex guards the account persona fields below.
	mutex sync.RWMutex
	// name is the current account display name.
	name string
	// avatar is the latest valid account avatar hash.
	avatar string
	// personaState is the current advertised presence.
	personaState EPersonaState
}

// newSocial binds account and relationship caches to the Steam client.
func newSocial(client *Client) *Social {
	return &Social{
		Friends: socialcache.NewFriendsList(),
		Groups:  socialcache.NewGroupsList(),
		Chats:   socialcache.NewChatsList(),
		client:  client,
	}
}

// GetAvatar returns the latest observed account avatar.
func (s *Social) GetAvatar() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.avatar
}

// GetPersonaName returns the latest observed account name.
func (s *Social) GetPersonaName() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.name
}

// SetPersonaName changes and publishes the account name.
func (s *Social) SetPersonaName(name string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.name = name
	s.client.Write(NewClientMsgProtobuf(EMsg_ClientChangeStatus, &CMsgClientChangeStatus{
		PersonaState: new(uint32(s.personaState)),
		PlayerName:   new(name),
	}))
}

// GetPersonaState returns the current advertised presence.
func (s *Social) GetPersonaState() EPersonaState {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.personaState
}

// SetPersonaState changes and publishes the account presence.
func (s *Social) SetPersonaState(state EPersonaState) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.personaState = state
	s.client.Write(NewClientMsgProtobuf(EMsg_ClientChangeStatus, &CMsgClientChangeStatus{
		PersonaState: new(uint32(state)),
	}))
}

// SendMessage sends a direct or room message according to the target identity.
func (s *Social) SendMessage(to steamid.SteamId, entryType EChatEntryType, message string) {
	// Individual accounts receive direct friend messages.
	if to.GetAccountType() == EAccountType_Individual || to.GetAccountType() == EAccountType_ConsoleUser {
		s.client.Write(NewClientMsgProtobuf(EMsg_ClientFriendMsg, &CMsgClientFriendMsg{
			Steamid:       new(to.ToUint64()),
			ChatEntryType: new(int32(entryType)),
			Message:       []byte(message),
		}))
		// Group identities use the legacy chat-room envelope.
	} else if to.GetAccountType() == EAccountType_Clan || to.GetAccountType() == EAccountType_Chat {
		chatID := to.ClanToChat()
		s.client.Write(NewClientMsg(&MsgClientChatMsg{
			ChatMsgType:     entryType,
			SteamIdChatRoom: SteamId(chatID),
			SteamIdChatter:  SteamId(s.client.SteamId()),
		}, []byte(message)))
	}
}

// AddFriend a friend to your friends list or accepts a friend. You'll receive a FriendStateEvent
// for every new/changed friend
func (s *Social) AddFriend(id steamid.SteamId) {
	s.client.Write(NewClientMsgProtobuf(EMsg_ClientAddFriend, &CMsgClientAddFriend{
		SteamidToAdd: new(id.ToUint64()),
	}))
}

// RemoveFriend removes a friend from your friends list
func (s *Social) RemoveFriend(id steamid.SteamId) {
	s.client.Write(NewClientMsgProtobuf(EMsg_ClientRemoveFriend, &CMsgClientRemoveFriend{
		Friendid: new(id.ToUint64()),
	}))
}

// IgnoreFriend ignores or unignores a friend on Steam
func (s *Social) IgnoreFriend(id steamid.SteamId, setIgnore bool) {
	ignore := uint8(1) //True
	if !setIgnore {
		ignore = uint8(0) //False
	}
	s.client.Write(NewClientMsg(&MsgClientSetIgnoreFriend{
		MySteamId:     SteamId(s.client.SteamId()),
		SteamIdFriend: SteamId(id),
		Ignore:        ignore,
	}, make([]byte, 0)))
}

// RequestFriendListInfo requests persona state for a list of specified SteamIds
func (s *Social) RequestFriendListInfo(ids []steamid.SteamId, requestedInfo EClientPersonaStateFlag) {
	var friends []uint64
	for _, id := range ids {
		friends = append(friends, id.ToUint64())
	}
	s.client.Write(NewClientMsgProtobuf(EMsg_ClientRequestFriendData, &CMsgClientRequestFriendData{
		PersonaStateRequested: new(uint32(requestedInfo)),
		Friends:               friends,
	}))
}

// RequestFriendInfo requests persona state for a specified SteamId
func (s *Social) RequestFriendInfo(id steamid.SteamId, requestedInfo EClientPersonaStateFlag) {
	s.RequestFriendListInfo([]steamid.SteamId{id}, requestedInfo)
}

// RequestProfileInfo requests profile information for a specified SteamId
func (s *Social) RequestProfileInfo(id steamid.SteamId) {
	s.client.Write(NewClientMsgProtobuf(EMsg_ClientFriendProfileInfo, &CMsgClientFriendProfileInfo{
		SteamidFriend: new(id.ToUint64()),
	}))
}

// JoinChat attempts to join a chat room
func (s *Social) JoinChat(id steamid.SteamId) {
	chatID := id.ClanToChat()
	s.client.Write(NewClientMsg(&MsgClientJoinChat{
		SteamIdChat: SteamId(chatID),
	}, make([]byte, 0)))
}

// LeaveChat attempts to leave a chat room
func (s *Social) LeaveChat(id steamid.SteamId) {
	chatID := id.ClanToChat()
	payload := new(bytes.Buffer)
	_ = binary.Write(payload, binary.LittleEndian, s.client.SteamId().ToUint64())       // ChatterActedOn
	_ = binary.Write(payload, binary.LittleEndian, uint32(EChatMemberStateChange_Left)) // StateChange
	_ = binary.Write(payload, binary.LittleEndian, s.client.SteamId().ToUint64())       // ChatterActedBy
	s.client.Write(NewClientMsg(&MsgClientChatMemberInfo{
		SteamIdChat: SteamId(chatID),
		Type:        EChatInfoType_StateChange,
	}, payload.Bytes()))
}

// KickChatMember requests removal of a member from the room.
func (s *Social) KickChatMember(room steamid.SteamId, user SteamId) {
	chatID := room.ClanToChat()
	s.client.Write(NewClientMsg(&MsgClientChatAction{
		SteamIdChat:        SteamId(chatID),
		SteamIdUserToActOn: user,
		ChatAction:         EChatAction_Kick,
	}, make([]byte, 0)))
}

// BanChatMember requests a room ban for the member.
func (s *Social) BanChatMember(room steamid.SteamId, user SteamId) {
	chatID := room.ClanToChat()
	s.client.Write(NewClientMsg(&MsgClientChatAction{
		SteamIdChat:        SteamId(chatID),
		SteamIdUserToActOn: user,
		ChatAction:         EChatAction_Ban,
	}, make([]byte, 0)))
}

// UnbanChatMember requests removal of the member’s room ban.
func (s *Social) UnbanChatMember(room steamid.SteamId, user SteamId) {
	chatID := room.ClanToChat()
	s.client.Write(NewClientMsg(&MsgClientChatAction{
		SteamIdChat:        SteamId(chatID),
		SteamIdUserToActOn: user,
		ChatAction:         EChatAction_UnBan,
	}, make([]byte, 0)))
}

// HandlePacket handles a Steam packet.
func (s *Social) HandlePacket(packet *Packet) {
	switch packet.EMsg {
	case EMsg_ClientPersonaState:
		s.handlePersonaState(packet)
	case EMsg_ClientClanState:
		s.handleClanState(packet)
	case EMsg_ClientFriendsList:
		s.handleFriendsList(packet)
	case EMsg_ClientFriendMsgIncoming:
		s.handleFriendMsg(packet)
	case EMsg_ClientAccountInfo:
		s.handleAccountInfo(packet)
	case EMsg_ClientAddFriendResponse:
		s.handleFriendResponse(packet)
	case EMsg_ClientChatEnter:
		s.handleChatEnter(packet)
	case EMsg_ClientChatMsg:
		s.handleChatMsg(packet)
	case EMsg_ClientChatMemberInfo:
		s.handleChatMemberInfo(packet)
	case EMsg_ClientChatActionResult:
		s.handleChatActionResult(packet)
	case EMsg_ClientChatInvite:
		s.handleChatInvite(packet)
	case EMsg_ClientSetIgnoreFriendResponse:
		s.handleIgnoreFriendResponse(packet)
	case EMsg_ClientFriendProfileInfoResponse:
		s.handleProfileInfoResponse(packet)
	}
}

// handleAccountInfo requests the persona after account login.
func (s *Social) handleAccountInfo(packet *Packet) {
	// Auth owns account login; request the current persona through the social stream.
	flags := EClientPersonaStateFlag_PlayerName | EClientPersonaStateFlag_Presence | EClientPersonaStateFlag_SourceID
	s.RequestFriendInfo(s.client.SteamId(), EClientPersonaStateFlag(flags))
}

// handleFriendsList updates relationships before publishing the initial roster.
func (s *Social) handleFriendsList(packet *Packet) {
	list := new(CMsgClientFriendsList)
	packet.ReadProtoMsg(list)
	var friends []steamid.SteamId
	for _, friend := range list.GetFriends() {
		steamID := steamid.SteamId(friend.GetUlfriendid())
		isClan := steamID.GetAccountType() == EAccountType_Clan

		if isClan {
			rel := EClanRelationship(friend.GetEfriendrelationship())
			if rel == EClanRelationship_None {
				s.Groups.Remove(steamID)
			} else {
				s.Groups.Add(socialcache.Group{
					SteamId:      steamID,
					Relationship: rel,
				})

			}
			if list.GetBincremental() {
				s.client.Emit(&GroupStateEvent{steamid.SteamId(steamID), rel})
			}
		} else {
			rel := EFriendRelationship(friend.GetEfriendrelationship())
			if rel == EFriendRelationship_None {
				s.Friends.Remove(steamID)
			} else {
				s.Friends.Add(socialcache.Friend{
					SteamId:      steamID,
					Relationship: rel,
				})

			}
			if list.GetBincremental() {
				s.client.Emit(&FriendStateEvent{steamID, rel})
			}
		}
		if !list.GetBincremental() {
			friends = append(friends, steamID)
		}
	}
	if !list.GetBincremental() {
		s.RequestFriendListInfo(friends, EClientPersonaStateFlag_DefaultInfoRequest)
		s.client.Emit(&FriendsListEvent{})
	}
}

// handlePersonaState merges fields identified by Steam presence flags.
func (s *Social) handlePersonaState(packet *Packet) {
	list := new(CMsgClientPersonaState)
	packet.ReadProtoMsg(list)
	flags := EClientPersonaStateFlag(list.GetStatusFlags())
	for _, friend := range list.GetFriends() {
		id := steamid.SteamId(friend.GetFriendid())
		if id == s.client.SteamId() { //this is our client id
			s.mutex.Lock()
			if friend.GetPlayerName() != "" {
				s.name = friend.GetPlayerName()
			}
			avatar := hex.EncodeToString(friend.GetAvatarHash())
			if ValidAvatar(avatar) {
				s.avatar = avatar
			}
			s.mutex.Unlock()
		} else if id.GetAccountType() == EAccountType_Individual {
			if (flags & EClientPersonaStateFlag_PlayerName) == EClientPersonaStateFlag_PlayerName {
				if friend.GetPlayerName() != "" {
					s.Friends.SetName(id, friend.GetPlayerName())
				}
			}
			if (flags & EClientPersonaStateFlag_Presence) == EClientPersonaStateFlag_Presence {
				avatar := hex.EncodeToString(friend.GetAvatarHash())
				if ValidAvatar(avatar) {
					s.Friends.SetAvatar(id, avatar)
				}
				s.Friends.SetPersonaState(id, EPersonaState(friend.GetPersonaState()))
				s.Friends.SetPersonaStateFlags(id, EPersonaStateFlag(friend.GetPersonaStateFlags()))
			}
			if (flags & EClientPersonaStateFlag_GameDataBlob) == EClientPersonaStateFlag_GameDataBlob {
				s.Friends.SetGameAppId(id, friend.GetGamePlayedAppId())
				s.Friends.SetGameId(id, friend.GetGameid())
				s.Friends.SetGameName(id, friend.GetGameName())
			}
		} else if id.GetAccountType() == EAccountType_Clan {
			if (flags & EClientPersonaStateFlag_PlayerName) == EClientPersonaStateFlag_PlayerName {
				if friend.GetPlayerName() != "" {
					s.Groups.SetName(id, friend.GetPlayerName())
				}
			}
			if (flags & EClientPersonaStateFlag_Presence) == EClientPersonaStateFlag_Presence {
				avatar := hex.EncodeToString(friend.GetAvatarHash())
				if ValidAvatar(avatar) {
					s.Groups.SetAvatar(id, avatar)
				}
			}
		}
		s.client.Emit(&PersonaStateEvent{
			StatusFlags:            flags,
			FriendId:               id,
			State:                  EPersonaState(friend.GetPersonaState()),
			StateFlags:             EPersonaStateFlag(friend.GetPersonaStateFlags()),
			GameAppId:              friend.GetGamePlayedAppId(),
			GameId:                 friend.GetGameid(),
			GameName:               friend.GetGameName(),
			GameServerIp:           friend.GetGameServerIp(),
			GameServerPort:         friend.GetGameServerPort(),
			QueryPort:              friend.GetQueryPort(),
			SourceSteamId:          steamid.SteamId(friend.GetSteamidSource()),
			GameDataBlob:           friend.GetGameDataBlob(),
			Name:                   friend.GetPlayerName(),
			Avatar:                 hex.EncodeToString(friend.GetAvatarHash()),
			LastLogOff:             friend.GetLastLogoff(),
			LastLogOn:              friend.GetLastLogon(),
			ClanRank:               friend.GetClanRank(),
			ClanTag:                friend.GetClanTag(),
			OnlineSessionInstances: friend.GetOnlineSessionInstances(),
		})
	}
}

// handleClanState updates cached group state before publishing its observation.
func (s *Social) handleClanState(packet *Packet) {
	body := new(CMsgClientClanState)
	packet.ReadProtoMsg(body)
	var name string
	var avatar string
	if body.GetNameInfo() != nil {
		name = body.GetNameInfo().GetClanName()
		avatar = hex.EncodeToString(body.GetNameInfo().GetShaAvatar())
	}
	var totalCount, onlineCount, chattingCount, ingameCount uint32
	if body.GetUserCounts() != nil {
		usercounts := body.GetUserCounts()
		totalCount = usercounts.GetMembers()
		onlineCount = usercounts.GetOnline()
		chattingCount = usercounts.GetChatting()
		ingameCount = usercounts.GetInGame()
	}
	var events, announcements []ClanEventDetails
	for _, event := range body.GetEvents() {
		events = append(events, ClanEventDetails{
			Id:         event.GetGid(),
			EventTime:  event.GetEventTime(),
			Headline:   event.GetHeadline(),
			GameId:     event.GetGameId(),
			JustPosted: event.GetJustPosted(),
		})
	}
	for _, announce := range body.GetAnnouncements() {
		announcements = append(announcements, ClanEventDetails{
			Id:         announce.GetGid(),
			EventTime:  announce.GetEventTime(),
			Headline:   announce.GetHeadline(),
			GameId:     announce.GetGameId(),
			JustPosted: announce.GetJustPosted(),
		})
	}
	clanid := steamid.SteamId(body.GetSteamidClan())
	if body.GetNameInfo() != nil {
		if name != "" {
			s.Groups.SetName(clanid, name)
		}
		if ValidAvatar(avatar) {
			s.Groups.SetAvatar(clanid, avatar)
		}
	}
	if body.GetUserCounts() != nil {
		s.Groups.SetMemberTotalCount(clanid, totalCount)
		s.Groups.SetMemberOnlineCount(clanid, onlineCount)
		s.Groups.SetMemberChattingCount(clanid, chattingCount)
		s.Groups.SetMemberInGameCount(clanid, ingameCount)
	}
	s.client.Emit(&ClanStateEvent{
		ClandId:             clanid,
		ClanName:            name,
		Avatar:              avatar,
		MemberTotalCount:    totalCount,
		MemberOnlineCount:   onlineCount,
		MemberChattingCount: chattingCount,
		MemberInGameCount:   ingameCount,
		Events:              events,
		Announcements:       announcements,
	})
}

// handleFriendResponse publishes the result of a friend request.
func (s *Social) handleFriendResponse(packet *Packet) {
	body := new(CMsgClientAddFriendResponse)
	packet.ReadProtoMsg(body)
	s.client.Emit(&FriendAddedEvent{
		Result:      EResult(body.GetEresult()),
		SteamId:     steamid.SteamId(body.GetSteamIdAdded()),
		PersonaName: body.GetPersonaNameAdded(),
	})
}

// handleFriendMsg publishes a timestamped direct message.
func (s *Social) handleFriendMsg(packet *Packet) {
	body := new(CMsgClientFriendMsgIncoming)
	packet.ReadProtoMsg(body)
	message := string(bytes.Split(body.GetMessage(), []byte{0x0})[0])
	s.client.Emit(&ChatMsgEvent{
		ChatterId: SteamId(body.GetSteamidFrom()),
		Message:   message,
		EntryType: EChatEntryType(body.GetChatEntryType()),
		Timestamp: time.Unix(int64(body.GetRtime32ServerTimestamp()), 0),
	})
}

// handleChatMsg publishes a legacy chat-room message.
func (s *Social) handleChatMsg(packet *Packet) {
	body := new(MsgClientChatMsg)
	payload := packet.ReadClientMsg(body).Payload
	message := string(bytes.Split(payload, []byte{0x0})[0])
	s.client.Emit(&ChatMsgEvent{
		ChatRoomId: SteamId(body.SteamIdChatRoom),
		ChatterId:  SteamId(body.SteamIdChatter),
		Message:    message,
		EntryType:  EChatEntryType(body.ChatMsgType),
	})
}

// handleChatEnter retains the room roster before announcing entry.
func (s *Social) handleChatEnter(packet *Packet) {
	body := new(MsgClientChatEnter)
	payload := packet.ReadClientMsg(body).Payload
	reader := bytes.NewBuffer(payload)
	name, _ := ReadString(reader)
	_, _ = ReadByte(reader) //0
	count := body.NumMembers
	chatID := steamid.SteamId(body.SteamIdChat)
	clanID := steamid.SteamId(body.SteamIdClan)
	s.Chats.Add(socialcache.Chat{SteamId: chatID, GroupId: clanID})
	for i := 0; i < int(count); i++ {
		id, chatPerm, clanPerm := readChatMember(reader)
		_, _ = ReadBytes(reader, 6) //No idea what this is
		s.Chats.AddChatMember(chatID, socialcache.ChatMember{
			SteamId:         steamid.SteamId(id),
			ChatPermissions: chatPerm,
			ClanPermissions: clanPerm,
		})
	}
	s.client.Emit(&ChatEnterEvent{
		ChatRoomId:    steamid.SteamId(body.SteamIdChat),
		FriendId:      steamid.SteamId(body.SteamIdFriend),
		ChatRoomType:  EChatRoomType(body.ChatRoomType),
		OwnerId:       steamid.SteamId(body.SteamIdOwner),
		ClanId:        steamid.SteamId(body.SteamIdClan),
		ChatFlags:     byte(body.ChatFlags),
		EnterResponse: EChatRoomEnterResponse(body.EnterResponse),
		Name:          name,
	})
}

// handleChatMemberInfo updates room membership before publishing the change.
func (s *Social) handleChatMemberInfo(packet *Packet) {
	body := new(MsgClientChatMemberInfo)
	payload := packet.ReadClientMsg(body).Payload
	reader := bytes.NewBuffer(payload)
	chatID := steamid.SteamId(body.SteamIdChat)
	if body.Type == EChatInfoType_StateChange {
		actedOn, _ := ReadUint64(reader)
		state, _ := ReadInt32(reader)
		actedBy, _ := ReadUint64(reader)
		_, _ = ReadByte(reader) //0
		stateChange := EChatMemberStateChange(state)
		if stateChange == EChatMemberStateChange_Entered {
			_, chatPerm, clanPerm := readChatMember(reader)
			s.Chats.AddChatMember(chatID, socialcache.ChatMember{
				SteamId:         steamid.SteamId(actedOn),
				ChatPermissions: chatPerm,
				ClanPermissions: clanPerm,
			})
		} else if stateChange == EChatMemberStateChange_Banned || stateChange == EChatMemberStateChange_Kicked ||
			stateChange == EChatMemberStateChange_Disconnected || stateChange == EChatMemberStateChange_Left {
			s.Chats.RemoveChatMember(chatID, steamid.SteamId(actedOn))
		}
		stateInfo := StateChangeDetails{
			ChatterActedOn: SteamId(actedOn),
			StateChange:    EChatMemberStateChange(stateChange),
			ChatterActedBy: SteamId(actedBy),
		}
		s.client.Emit(&ChatMemberInfoEvent{
			ChatRoomId:      steamid.SteamId(body.SteamIdChat),
			Type:            EChatInfoType(body.Type),
			StateChangeInfo: stateInfo,
		})
	}
}

// readChatMember decodes the retained binary chat membership layout.
func readChatMember(r io.Reader) (SteamId, EChatPermission, EClanPermission) {
	_, _ = ReadString(r) // MessageObject
	_, _ = ReadByte(r)   // 7
	_, _ = ReadString(r) //steamid
	id, _ := ReadUint64(r)
	_, _ = ReadByte(r)   // 2
	_, _ = ReadString(r) //Permissions
	chat, _ := ReadInt32(r)
	_, _ = ReadByte(r)   // 2
	_, _ = ReadString(r) //Details
	clan, _ := ReadInt32(r)
	return SteamId(id), EChatPermission(chat), EClanPermission(clan)
}

// handleChatActionResult publishes a moderation action result.
func (s *Social) handleChatActionResult(packet *Packet) {
	body := new(MsgClientChatActionResult)
	packet.ReadClientMsg(body)
	s.client.Emit(&ChatActionResultEvent{
		ChatRoomId: SteamId(body.SteamIdChat),
		ChatterId:  SteamId(body.SteamIdUserActedOn),
		Action:     EChatAction(body.ChatAction),
		Result:     EChatActionResult(body.ActionResult),
	})
}

// handleChatInvite publishes the invitation supplied by Steam.
func (s *Social) handleChatInvite(packet *Packet) {
	body := new(CMsgClientChatInvite)
	packet.ReadProtoMsg(body)
	s.client.Emit(&ChatInviteEvent{
		InvitedId:    steamid.SteamId(body.GetSteamIdInvited()),
		ChatRoomId:   steamid.SteamId(body.GetSteamIdChat()),
		PatronId:     steamid.SteamId(body.GetSteamIdPatron()),
		ChatRoomType: EChatRoomType(body.GetChatroomType()),
		FriendChatId: steamid.SteamId(body.GetSteamIdFriendChat()),
		ChatRoomName: body.GetChatName(),
		GameId:       body.GetGameId(),
	})
}

// handleIgnoreFriendResponse publishes Steam’s ignore-list result.
func (s *Social) handleIgnoreFriendResponse(packet *Packet) {
	body := new(MsgClientSetIgnoreFriendResponse)
	packet.ReadClientMsg(body)
	s.client.Emit(&IgnoreFriendEvent{
		Result: EResult(body.Result),
	})
}

// handleProfileInfoResponse publishes the requested profile observation.
func (s *Social) handleProfileInfoResponse(packet *Packet) {
	body := new(CMsgClientFriendProfileInfoResponse)
	packet.ReadProtoMsg(body)
	s.client.Emit(&ProfileInfoEvent{
		Result:      EResult(body.GetEresult()),
		SteamId:     steamid.SteamId(body.GetSteamidFriend()),
		TimeCreated: body.GetTimeCreated(),
		RealName:    body.GetRealName(),
		CityName:    body.GetCityName(),
		StateName:   body.GetStateName(),
		CountryName: body.GetCountryName(),
		Headline:    body.GetHeadline(),
		Summary:     body.GetSummary(),
	})
}
