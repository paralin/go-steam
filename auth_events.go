package steam

import (
	"github.com/paralin/go-steam/protocol/protobuf"
	. "github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
)

type LoggedOnEvent struct {
	Result         EResult
	ExtendedResult EResult
	AccountFlags   EAccountFlags
	ClientSteamId  steamid.SteamId `json:",string"`
	Body           *protobuf.CMsgClientLogonResponse
}

// AuthSessionState names the modern authentication stage that failed before CM
// logon could receive an access token.
type AuthSessionState string

const (
	AuthSessionStateNone               AuthSessionState = ""
	AuthSessionStateEmailCode          AuthSessionState = "email_code"
	AuthSessionStateDeviceCode         AuthSessionState = "device_code"
	AuthSessionStateManualConfirmation AuthSessionState = "manual_confirmation"
	AuthSessionStateUnsupportedGuard   AuthSessionState = "unsupported_guard"
	AuthSessionStateDenied             AuthSessionState = "denied"
	AuthSessionStateCanceled           AuthSessionState = "canceled"
	AuthSessionStateTransport          AuthSessionState = "transport"
	AuthSessionStateProtobuf           AuthSessionState = "protobuf"
	AuthSessionStateAgreement          AuthSessionState = "agreement"
	AuthSessionStateTimeout            AuthSessionState = "timeout"
)

// AuthSessionError reports a failed modern authentication session stage.
type AuthSessionError struct {
	State         AuthSessionState
	Confirmations []SteamGuardConfirmation
	Err           error
}

func (e *AuthSessionError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.State != AuthSessionStateNone {
		return string(e.State)
	}
	return "auth session failed"
}

func (e *AuthSessionError) Unwrap() error {
	return e.Err
}

type LogOnFailedEvent struct {
	Result           EResult
	Err              error
	AuthSessionState AuthSessionState
	Confirmations    []SteamGuardConfirmation
}

type LoggedOffEvent struct {
	Result EResult
}

type AccountInfoEvent struct {
	PersonaName          string
	Country              string
	CountAuthedComputers int32
	AccountFlags         EAccountFlags
}

// Returned when Steam is down for some reason.
// A disconnect will follow, probably.
type SteamFailureEvent struct {
	Result EResult
}
