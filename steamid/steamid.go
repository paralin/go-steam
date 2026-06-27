package steamid

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/paralin/go-steam/protocol/steamlang"
)

// SteamId is a steam identifier.
type SteamId uint64

func fromAccountId(id uint32) SteamId {
	return NewIdAdv(id, 1, 1, 1)
}

func fromAccountIdStr(id string) (SteamId, error) {
	accountId, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return SteamId(0), err
	}
	return fromAccountId(uint32(accountId)), nil
}

// NewId attempts to parse the steam ID.
func NewId(id string) (SteamId, error) {
	valid, err := regexp.MatchString(`[U:1:[0-9]+]`, id)
	if err != nil {
		return SteamId(0), err
	}
	if valid {
		id = id[5 : len(id)-1]
		return fromAccountIdStr(id)
	}

	valid, err = regexp.MatchString(`STEAM_[0-5]:[01]:\d+`, id)
	if err != nil {
		return SteamId(0), err
	}
	if valid {
		id = strings.Replace(id, "STEAM_", "", -1) // remove STEAM_
		splitid := strings.Split(id, ":")          // split 0:1:00000000 into 0 1 00000000
		universe, _ := strconv.ParseInt(splitid[0], 10, 32)
		if universe == 0 { //EUniverse_Invalid
			universe = int64(steamlang.EUniverse_Public)
		}
		authServer, _ := strconv.ParseUint(splitid[1], 10, 32)
		accId, _ := strconv.ParseUint(splitid[2], 10, 32)
		accountType := steamlang.EAccountType_Individual
		accountId := (uint32(accId) << 1) | uint32(authServer)
		return NewIdAdv(uint32(accountId), 1, int32(universe), accountType), nil
	}

	valid, err = regexp.MatchString(`^[0-9]{7,9}$`, id)
	if err != nil {
		return SteamId(0), err
	}
	if valid {
		return fromAccountIdStr(id)
	}

	newid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return SteamId(0), err
	}
	return SteamId(newid), nil
}

func NewIdAdv(accountId, instance uint32, universe int32, accountType steamlang.EAccountType) SteamId {
	s := SteamId(0)
	s = s.SetAccountId(accountId)
	s = s.SetAccountInstance(instance)
	s = s.SetAccountUniverse(universe)
	s = s.SetAccountType(accountType)
	return s
}

func (s SteamId) ToUint64() uint64 {
	return uint64(s)
}

func (s SteamId) ToString() string {
	return strconv.FormatUint(uint64(s), 10)
}

func (s SteamId) String() string {
	switch s.GetAccountType() {
	case 0: // EAccountType_Invalid
		fallthrough
	case 1: // EAccountType_Individual
		if s.GetAccountUniverse() <= 1 { // EUniverse_Public
			return fmt.Sprintf("STEAM_0:%d:%d", s.GetAccountId()&1, s.GetAccountId()>>1)
		} else {
			return fmt.Sprintf("STEAM_%d:%d:%d", s.GetAccountUniverse(), s.GetAccountId()&1, s.GetAccountId()>>1)
		}
	default:
		return strconv.FormatUint(uint64(s), 10)
	}
}

func (s SteamId) get(offset uint, mask uint64) uint64 {
	return (uint64(s) >> offset) & mask
}

func (s SteamId) set(offset uint, mask, value uint64) SteamId {
	return SteamId((uint64(s) & ^(mask << offset)) | (value&mask)<<offset)
}

func (s SteamId) GetAccountId() uint32 {
	return uint32(s.get(0, 0xFFFFFFFF))
}

func (s SteamId) SetAccountId(id uint32) SteamId {
	return s.set(0, 0xFFFFFFFF, uint64(id))
}

func (s SteamId) GetAccountInstance() AccountInstance {
	return AccountInstance(s.get(32, 0xFFFFF))
}

func (s SteamId) SetAccountInstance(value uint32) SteamId {
	return s.set(32, 0xFFFFF, uint64(value))
}

func (s SteamId) GetAccountType() steamlang.EAccountType {
	accType := steamlang.EAccountType(s.get(52, 0xF))
	if accType <= steamlang.EAccountType_Invalid || accType >= steamlang.EAccountType_Max {
		return steamlang.EAccountType_Invalid
	}
	return accType
}

func (s SteamId) SetAccountType(t steamlang.EAccountType) SteamId {
	return s.set(52, 0xF, uint64(t))
}

func (s SteamId) GetAccountUniverse() int32 {
	return int32(s.get(56, 0xF))
}

func (s SteamId) SetAccountUniverse(universe int32) SteamId {
	return s.set(56, 0xF, uint64(universe))
}

// used to fix the Clan SteamId to a Chat SteamId
func (s SteamId) ClanToChat() SteamId {
	if s.GetAccountType() == steamlang.EAccountType(7) { //EAccountType_Clan
		s = s.SetAccountInstance(uint32(ChatInstanceFlagClan))
		s = s.SetAccountType(steamlang.EAccountType_Chat) //EAccountType_Chat
	}
	return s
}

// used to fix the Chat SteamId to a Clan SteamId
func (s SteamId) ChatToClan() SteamId {
	if s.GetAccountType() == steamlang.EAccountType_Chat { //EAccountType_Chat
		s = s.SetAccountInstance(0)
		s = s.SetAccountType(steamlang.EAccountType_Clan) //EAccountType_Clan
	}
	return s
}

// ToSteam2 converts to the steam2 ID representation.
func (s SteamId) ToSteam2() string {
	return s.String()
}

// ToSteam3 converts to the steam3 ID representation.
func (s SteamId) ToSteam3() string {
	accType := s.GetAccountType()
	accInstance := s.GetAccountInstance()

	accTypeChr, ok := accountTypeChars[accType]
	if !ok {
		accTypeChr = 'i'
	}

	if accType == steamlang.EAccountType_Chat {
		if accInstance.HasFlag(uint32(ChatInstanceFlagClan)) {
			accTypeChr = 'c'
		} else if accInstance.HasFlag(uint32(ChatInstanceFlagLobby)) {
			accTypeChr = 'L'
		}
	}

	var renderInstance bool
	switch accType {
	case steamlang.EAccountType_AnonGameServer:
		fallthrough
	case steamlang.EAccountType_Multiseat:
		renderInstance = true
		break
	case steamlang.EAccountType_Individual:
		renderInstance = uint32(accInstance) != DesktopInstance
	}

	if renderInstance {
		return fmt.Sprintf("[%s:%d:%d:%d]", string(accTypeChr), s.GetAccountUniverse(), s.GetAccountId(), accInstance)
	}

	return fmt.Sprintf("[%s:%d:%d]", string(accTypeChr), s.GetAccountUniverse(), s.GetAccountId())
}
