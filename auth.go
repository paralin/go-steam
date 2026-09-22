package steam

import (
	"context"
	"errors"
	"net/http"
	"time"

	. "github.com/paralin/go-steam/protocol"
	. "github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
)

// Auth performs Steam authentication and dispatches account events.
type Auth struct {
	// client receives authenticated identity and account events.
	client *Client
	// authServiceBaseURL overrides the official endpoint for protocol tests.
	authServiceBaseURL string
	// authServiceHTTPClient overrides the authentication HTTP transport.
	authServiceHTTPClient *http.Client
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

	// Exchange interactive credentials only when no reusable token was supplied.
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

	// Send the token over the encrypted CM connection.
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

	a.client.steamId.Store(steamid.NewIdAdv(0, 1, int32(EUniverse_Public), EAccountType_Individual).ToUint64())

	a.client.Write(NewClientMsgProtobuf(EMsg_ClientLogon, logon))
	return nil
}

// HandlePacket dispatches Steam authentication and account packets.
func (a *Auth) HandlePacket(packet *Packet) {
	switch packet.EMsg {
	case EMsg_ClientLogOnResponse:
		a.handleLogOnResponse(packet)
	case EMsg_ClientSessionToken:
	case EMsg_ClientLoggedOff:
		a.handleLoggedOff(packet)
	case EMsg_ClientPlayingSessionState:
		body := new(CMsgClientPlayingSessionState)
		packet.ReadProtoMsg(body)
		a.client.Emit(&PlayingSessionStateEvent{
			PlayingBlocked: body.GetPlayingBlocked(),
			PlayingApp:     body.GetPlayingApp(),
		})
	case EMsg_ClientAccountInfo:
		a.handleAccountInfo(packet)
	case EMsg_ClientWalletInfoUpdate:
	case EMsg_ClientRequestWebAPIAuthenticateUserNonceResponse:
	case EMsg_ClientMarketingMessageUpdate:
	}
}

// handleLogOnResponse establishes authenticated identity and heartbeat timing.
func (a *Auth) handleLogOnResponse(packet *Packet) {
	if !packet.IsProto {
		a.client.Fatalf("Got non-proto logon response!")
		return
	}

	body := new(CMsgClientLogonResponse)
	msg := packet.ReadProtoMsg(body)

	result := EResult(body.GetEresult())
	if result == EResult_OK {
		a.client.sessionId.Store(msg.Header.Proto.GetClientSessionid())
		a.client.steamId.Store(msg.Header.Proto.GetSteamid())
		heartbeatSeconds := body.GetHeartbeatSeconds()
		a.client.setHeartbeat(time.Duration(heartbeatSeconds))

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

// handleLoggedOff reports Steam termination of the authenticated session.
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

// handleAccountInfo publishes the signed-in account profile.
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
