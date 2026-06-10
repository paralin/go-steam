package steam

import (
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
)

type Auth struct {
	client  *Client
	details *LogOnDetails
}

type SentryHash []byte

type LogOnDetails struct {
	Username string

	// If logging into an account without a login key, the account's password.
	Password string

	// If you have a Steam Guard email code, you can provide it here.
	AuthCode string

	// If you have a Steam Guard mobile two-factor authentication code, you can provide it here.
	TwoFactorCode  string
	SentryFileHash SentryHash
	LoginKey       string

	// A refresh token obtained from the Authentication service. If set, it is
	// used to log on directly and Password is ignored. When only a Password is
	// set, one is obtained automatically before logging on.
	AccessToken string

	// true if you want to get a login key which can be used in lieu of
	// a password for subsequent logins. false or omitted otherwise.
	ShouldRememberPassword bool
}

// Log on with the given details. You must always specify username and
// password OR username and loginkey. For the first login, don't set an authcode or a hash and you'll
//
//	receive an error (EResult_AccountLogonDenied)
//
// and Steam will send you an authcode. Then you have to login again, this time with the authcode.
// Shortly after logging in, you'll receive a MachineAuthUpdateEvent with a hash which allows
// you to login without using an authcode in the future.
//
// If you don't use Steam Guard, username and password are enough.
//
// After the event EMsg_ClientNewLoginKey is received you can use the LoginKey
// to login instead of using the password.
func (a *Auth) LogOn(details *LogOnDetails) {
	if details.Username == "" {
		panic("Username must be set!")
	}
	if details.Password == "" && details.LoginKey == "" && details.AccessToken == "" {
		panic("Password, LoginKey or AccessToken must be set!")
	}

	// Steam no longer accepts a plaintext password in the logon message. When a
	// password is supplied (and no access token), exchange it for a refresh
	// token via the Authentication service first, then log on with that token.
	if details.AccessToken == "" && details.Password != "" {
		go func() {
			token, err := a.getAccessTokenViaCredentials(details)
			if err != nil {
				a.client.Errorf("auth: failed to obtain access token: %v", err)
				a.client.Emit(&LogOnFailedEvent{Result: EResult_InvalidPassword})
				a.client.Disconnect()
				return
			}
			a.sendLogon(details, token)
		}()
		return
	}

	a.sendLogon(details, details.AccessToken)
}

func (a *Auth) sendLogon(details *LogOnDetails, accessToken string) {
	logon := new(CMsgClientLogon)
	logon.AccountName = &details.Username
	if accessToken != "" {
		logon.AccessToken = proto.String(accessToken)
	} else if details.Password != "" {
		logon.Password = proto.String(details.Password)
	}
	if details.AuthCode != "" {
		logon.AuthCode = proto.String(details.AuthCode)
	}
	if details.TwoFactorCode != "" {
		logon.TwoFactorCode = proto.String(details.TwoFactorCode)
	}
	logon.ClientLanguage = proto.String("english")
	logon.ProtocolVersion = proto.Uint32(MsgClientLogon_CurrentProtocol)
	logon.ShaSentryfile = details.SentryFileHash
	if details.LoginKey != "" {
		logon.LoginKey = proto.String(details.LoginKey)
	}
	if details.ShouldRememberPassword {
		logon.ShouldRememberPassword = proto.Bool(details.ShouldRememberPassword)
	}

	atomic.StoreUint64(&a.client.steamId, steamid.NewIdAdv(0, 1, int32(EUniverse_Public), EAccountType_Individual).ToUint64())

	a.client.Write(NewClientMsgProtobuf(EMsg_ClientLogon, logon))
}

func (a *Auth) HandlePacket(packet *Packet) {
	switch packet.EMsg {
	case EMsg_ClientLogOnResponse:
		a.handleLogOnResponse(packet)
	case EMsg_ClientNewLoginKey:
		a.handleLoginKey(packet)
	case EMsg_ClientSessionToken:
	case EMsg_ClientLoggedOff:
		a.handleLoggedOff(packet)
	case EMsg_ClientAccountInfo:
		a.handleAccountInfo(packet)
	case EMsg_ClientWalletInfoUpdate:
	case EMsg_ClientRequestWebAPIAuthenticateUserNonceResponse:
	case EMsg_ClientMarketingMessageUpdate:
	}
}

func (a *Auth) handleLogOnResponse(packet *Packet) {
	if !packet.IsProto {
		a.client.Fatalf("Got non-proto logon response!")
		return
	}

	body := new(CMsgClientLogonResponse)
	msg := packet.ReadProtoMsg(body)

	result := EResult(body.GetEresult())
	if result == EResult_OK {
		atomic.StoreInt32(&a.client.sessionId, msg.Header.Proto.GetClientSessionid())
		atomic.StoreUint64(&a.client.steamId, msg.Header.Proto.GetSteamid())

		// The webapi nonce is no longer included in the logon response, so
		// request it explicitly. The Web module stores it when the response
		// arrives, enabling Web.LogOn().
		a.client.Write(NewClientMsgProtobuf(EMsg_ClientRequestWebAPIAuthenticateUserNonce, new(CMsgClientRequestWebAPIAuthenticateUserNonce)))

		go a.client.heartbeatLoop(time.Duration(body.GetHeartbeatSeconds()))

		a.client.Emit(&LoggedOnEvent{
			Result:         EResult(body.GetEresult()),
			ExtendedResult: EResult(body.GetEresultExtended()),
			AccountFlags:   EAccountFlags(body.GetAccountFlags()),
			ClientSteamId:  steamid.SteamId(body.GetClientSuppliedSteamid()),
			Body:           body,
		})
	} else if result == EResult_Fail || result == EResult_ServiceUnavailable || result == EResult_TryAnotherCM {
		// some error on Steam's side, we'll get an EOF later
		a.client.Emit(&SteamFailureEvent{
			Result: EResult(body.GetEresult()),
		})
	} else {
		a.client.Emit(&LogOnFailedEvent{
			Result: EResult(body.GetEresult()),
		})
		a.client.Disconnect()
	}
}

func (a *Auth) handleLoginKey(packet *Packet) {
	body := new(CMsgClientNewLoginKey)
	packet.ReadProtoMsg(body)
	a.client.Write(NewClientMsgProtobuf(EMsg_ClientNewLoginKeyAccepted, &CMsgClientNewLoginKeyAccepted{
		UniqueId: proto.Uint32(body.GetUniqueId()),
	}))
	a.client.Emit(&LoginKeyEvent{
		UniqueId: body.GetUniqueId(),
		LoginKey: body.GetLoginKey(),
	})
}

func (a *Auth) handleLoggedOff(packet *Packet) {
	result := EResult_Invalid
	if packet.IsProto {
		body := new(CMsgClientLoggedOff)
		packet.ReadProtoMsg(body)
		result = EResult(body.GetEresult())
	} else {
		body := new(MsgClientLoggedOff)
		packet.ReadClientMsg(body)
		result = body.Result
	}
	a.client.Emit(&LoggedOffEvent{Result: result})
}

func (a *Auth) handleAccountInfo(packet *Packet) {
	body := new(CMsgClientAccountInfo)
	packet.ReadProtoMsg(body)
	a.client.Emit(&AccountInfoEvent{
		PersonaName:          body.GetPersonaName(),
		Country:              body.GetIpCountry(),
		CountAuthedComputers: body.GetCountAuthedComputers(),
		AccountFlags:         EAccountFlags(body.GetAccountFlags()),
	})
}
