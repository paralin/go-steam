package steam

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
)

type Auth struct {
	client                *Client
	details               *LogOnDetails
	authServiceBaseURL    string
	authServiceHTTPClient *http.Client
}
type LogOnDetails struct {
	Username string

	// Password starts a modern Steam auth session and exchanges credentials for
	// a token before CM logon.
	Password string

	// AccessToken logs on to CM directly when the caller already owns a modern
	// Steam authentication token.
	AccessToken string

	// DeviceFriendlyName labels the device in the modern Steam auth session.
	DeviceFriendlyName string

	// AuthCode supplies a Steam Guard email code when Steam requests one.
	AuthCode string

	// TwoFactorCode supplies a Steam Guard mobile authenticator code when Steam
	// requests one.
	TwoFactorCode string

	ShouldRememberPassword bool
}

// LogOn exchanges password credentials for a modern Steam access token when
// needed, then logs on to CM with CMsgClientLogon.AccessToken.
func (a *Auth) LogOn(ctx context.Context, details *LogOnDetails) error {
	if details == nil {
		return errors.New("logon details must be set")
	}
	if details.Username == "" {
		return errors.New("username must be set")
	}
	if details.AccessToken == "" && details.Password == "" {
		return errors.New("password or access token must be set")
	}

	accessToken := details.AccessToken
	if accessToken == "" {
		var err error
		accessToken, err = a.getAccessTokenViaCredentials(ctx, details)
		if err != nil {
			event := &LogOnFailedEvent{Err: err}
			var authErr *AuthSessionError
			if errors.As(err, &authErr) {
				event.AuthSessionState = authErr.State
				event.Confirmations = authErr.Confirmations
			}
			a.client.Emit(event)
			return err
		}
	}

	language := "english"
	protocolVersion := uint32(MsgClientLogon_CurrentProtocol)
	rememberPassword := details.ShouldRememberPassword
	logon := &CMsgClientLogon{
		AccountName:     &details.Username,
		AccessToken:     &accessToken,
		ClientLanguage:  &language,
		ProtocolVersion: &protocolVersion,
	}
	if rememberPassword {
		logon.ShouldRememberPassword = &rememberPassword
	}

	atomic.StoreUint64(&a.client.steamId, steamid.NewIdAdv(0, 1, int32(EUniverse_Public), EAccountType_Individual).ToUint64())

	a.client.Write(NewClientMsgProtobuf(EMsg_ClientLogon, logon))
	return nil
}

func (a *Auth) HandlePacket(packet *Packet) {
	switch packet.EMsg {
	case EMsg_ClientLogOnResponse:
		a.handleLogOnResponse(packet)
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
		heartbeatSeconds := body.GetHeartbeatSeconds()
		go a.client.heartbeatLoop(time.Duration(heartbeatSeconds))

		a.client.Emit(&LoggedOnEvent{
			Result:         EResult(body.GetEresult()),
			ExtendedResult: EResult(body.GetEresultExtended()),
			AccountFlags:   EAccountFlags(body.GetAccountFlags()),
			ClientSteamId:  steamid.SteamId(body.GetClientSuppliedSteamid()),
			Body:           body,
		})
		a.client.Write(NewClientMsgProtobuf(EMsg_ClientRequestWebAPIAuthenticateUserNonce, new(CMsgClientRequestWebAPIAuthenticateUserNonce)))
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
