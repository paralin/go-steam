package steam

import (
	. "github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
	"time"
)

type FriendsListEvent struct{}

type FriendStateEvent struct {
	SteamId      steamid.SteamId `json:",string"`
	Relationship EFriendRelationship
}

func (f *FriendStateEvent) IsFriend() bool {
	return f.Relationship == EFriendRelationship_Friend
}

type GroupStateEvent struct {
	SteamId      steamid.SteamId `json:",string"`
	Relationship EClanRelationship
}

func (g *GroupStateEvent) IsMember() bool {
	return g.Relationship == EClanRelationship_Member
}

// Fired when someone changing their friend details
type PersonaStateEvent struct {
	StatusFlags            EClientPersonaStateFlag
	FriendId               steamid.SteamId `json:",string"`
	State                  EPersonaState
	StateFlags             EPersonaStateFlag
	GameAppId              uint32
	GameId                 uint64 `json:",string"`
	GameName               string
	GameServerIp           uint32
	GameServerPort         uint32
	QueryPort              uint32
	SourceSteamId          steamid.SteamId `json:",string"`
	GameDataBlob           []byte
	Name                   string
	Avatar                 string
	LastLogOff             uint32
	LastLogOn              uint32
	ClanRank               uint32
	ClanTag                string
	OnlineSessionInstances uint32
}

// Fired when a clan's state has been changed
type ClanStateEvent struct {
	ClandId             steamid.SteamId `json:",string"`
	ClanName            string
	Avatar              string
	MemberTotalCount    uint32
	MemberOnlineCount   uint32
	MemberChattingCount uint32
	MemberInGameCount   uint32
	Events              []ClanEventDetails
	Announcements       []ClanEventDetails
}

type ClanEventDetails struct {
	Id         uint64 `json:",string"`
	EventTime  uint32
	Headline   string
	GameId     uint64 `json:",string"`
	JustPosted bool
}

// Fired in response to adding a friend to your friends list
type FriendAddedEvent struct {
	Result      EResult
	SteamId     steamid.SteamId `json:",string"`
	PersonaName string
}

// Fired when the client receives a message from either a friend or a chat room
type ChatMsgEvent struct {
	ChatRoomId SteamId `json:",string"` // not set for friend messages
	ChatterId  SteamId `json:",string"`
	Message    string
	EntryType  EChatEntryType
	Timestamp  time.Time
	Offline    bool
}

// Whether the type is ChatMsg
func (c *ChatMsgEvent) IsMessage() bool {
	return c.EntryType == EChatEntryType_ChatMsg
}

// Fired in response to joining a chat
type ChatEnterEvent struct {
	ChatRoomId    steamid.SteamId `json:",string"`
	FriendId      steamid.SteamId `json:",string"`
	ChatRoomType  EChatRoomType
	OwnerId       steamid.SteamId `json:",string"`
	ClanId        steamid.SteamId `json:",string"`
	ChatFlags     byte
	EnterResponse EChatRoomEnterResponse
	Name          string
}

// Fired in response to a chat member's info being received
type ChatMemberInfoEvent struct {
	ChatRoomId      steamid.SteamId `json:",string"`
	Type            EChatInfoType
	StateChangeInfo StateChangeDetails
}

type StateChangeDetails struct {
	ChatterActedOn SteamId `json:",string"`
	StateChange    EChatMemberStateChange
	ChatterActedBy SteamId `json:",string"`
}

// Fired when a chat action has completed
type ChatActionResultEvent struct {
	ChatRoomId SteamId `json:",string"`
	ChatterId  SteamId `json:",string"`
	Action     EChatAction
	Result     EChatActionResult
}

// Fired when a chat invite is received
type ChatInviteEvent struct {
	InvitedId    steamid.SteamId `json:",string"`
	ChatRoomId   steamid.SteamId `json:",string"`
	PatronId     steamid.SteamId `json:",string"`
	ChatRoomType EChatRoomType
	FriendChatId steamid.SteamId `json:",string"`
	ChatRoomName string
	GameId       uint64 `json:",string"`
}

// Fired in response to ignoring a friend
type IgnoreFriendEvent struct {
	Result EResult
}

// Fired in response to requesting profile info for a user
type ProfileInfoEvent struct {
	Result      EResult
	SteamId     steamid.SteamId `json:",string"`
	TimeCreated uint32
	RealName    string
	CityName    string
	StateName   string
	CountryName string
	Headline    string
	Summary     string
}
