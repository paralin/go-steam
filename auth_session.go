package steam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	. "github.com/paralin/go-steam/protocol/protobuf/unified"
)

const (
	defaultAuthServiceBaseURL = "https://api.steampowered.com/IAuthenticationService/"
	maxPollAttempts           = 10
)

// SteamGuardConfirmation describes a Steam Guard confirmation required by a
// modern authentication session.
type SteamGuardConfirmation struct {
	Type    EAuthSessionGuardType
	Message string
}

func newAuthSessionError(state AuthSessionState, confirmations []SteamGuardConfirmation, err error) *AuthSessionError {
	return &AuthSessionError{State: state, Confirmations: confirmations, Err: err}
}

func (a *Auth) getAccessTokenViaCredentials(ctx context.Context, details *LogOnDetails) (string, error) {
	rsaResp := new(CAuthentication_GetPasswordRSAPublicKey_Response)
	if err := a.authServiceCall(ctx, http.MethodGet, "GetPasswordRSAPublicKey", &CAuthentication_GetPasswordRSAPublicKey_Request{
		AccountName: stringPtr(details.Username),
	}, rsaResp); err != nil {
		return "", err
	}

	encrypted, err := encryptPassword(details.Password, rsaResp.GetPublickeyMod(), rsaResp.GetPublickeyExp())
	if err != nil {
		return "", newAuthSessionError(AuthSessionStateProtobuf, nil, err)
	}

	platform := EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient
	persistence := ESessionPersistence_k_ESessionPersistence_Persistent
	deviceName := details.DeviceFriendlyName
	if deviceName == "" {
		deviceName = "go-steam"
	}
	beginResp := new(CAuthentication_BeginAuthSessionViaCredentials_Response)
	if err := a.authServiceCall(ctx, http.MethodPost, "BeginAuthSessionViaCredentials", &CAuthentication_BeginAuthSessionViaCredentials_Request{
		DeviceFriendlyName:  stringPtr(deviceName),
		AccountName:         stringPtr(details.Username),
		EncryptedPassword:   stringPtr(encrypted),
		EncryptionTimestamp: uint64Ptr(rsaResp.GetTimestamp()),
		RememberLogin:       boolPtr(details.ShouldRememberPassword),
		PlatformType:        &platform,
		Persistence:         &persistence,
		WebsiteId:           stringPtr("Client"),
		DeviceDetails: &CAuthentication_DeviceDetails{
			DeviceFriendlyName: stringPtr(deviceName),
			PlatformType:       &platform,
		},
	}, beginResp); err != nil {
		return "", err
	}
	if msg := beginResp.GetExtendedErrorMessage(); msg != "" {
		return "", newAuthSessionError(AuthSessionStateDenied, nil, fmt.Errorf("begin auth session failed: %s", msg))
	}
	confirmations, err := a.submitAvailableSteamGuardCode(ctx, beginResp, details)
	if err != nil {
		return "", err
	}

	interval := time.Duration(beginResp.GetInterval() * float32(time.Second))
	if interval <= 0 {
		interval = time.Second
	}
	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		pollResp := new(CAuthentication_PollAuthSessionStatus_Response)
		if err := a.authServiceCall(ctx, http.MethodPost, "PollAuthSessionStatus", &CAuthentication_PollAuthSessionStatus_Request{
			ClientId:  uint64Ptr(beginResp.GetClientId()),
			RequestId: beginResp.GetRequestId(),
		}, pollResp); err != nil {
			return "", err
		}
		if token := pollResp.GetRefreshToken(); token != "" {
			return token, nil
		}
		if token := pollResp.GetAccessToken(); token != "" {
			return token, nil
		}
		if url := pollResp.GetAgreementSessionUrl(); url != "" {
			return "", newAuthSessionError(AuthSessionStateAgreement, nil, fmt.Errorf("auth session requires agreement: %s", url))
		}
		if attempt == maxPollAttempts-1 {
			break
		}
		if err := waitAuthPoll(ctx, interval); err != nil {
			return "", err
		}
	}
	return "", newAuthSessionError(authSessionTimeoutState(confirmations), confirmations, fmt.Errorf("auth session did not complete after %d attempts", maxPollAttempts))
}

func (a *Auth) submitAvailableSteamGuardCode(ctx context.Context, resp *CAuthentication_BeginAuthSessionViaCredentials_Response, details *LogOnDetails) ([]SteamGuardConfirmation, error) {
	confirmations := make([]SteamGuardConfirmation, 0, len(resp.GetAllowedConfirmations()))
	manualConfirmationAvailable := false
	for _, conf := range resp.GetAllowedConfirmations() {
		confType := conf.GetConfirmationType()
		confirmations = append(confirmations, SteamGuardConfirmation{Type: confType, Message: conf.GetAssociatedMessage()})
		switch confType {
		case EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode:
			if details.AuthCode != "" {
				return confirmations, a.submitSteamGuardCode(ctx, resp.GetClientId(), resp.GetSteamid(), details.AuthCode, confType)
			}
		case EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode:
			if details.TwoFactorCode != "" {
				return confirmations, a.submitSteamGuardCode(ctx, resp.GetClientId(), resp.GetSteamid(), details.TwoFactorCode, confType)
			}
		case EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation,
			EAuthSessionGuardType_k_EAuthSessionGuardType_EmailConfirmation:
			manualConfirmationAvailable = true
		case EAuthSessionGuardType_k_EAuthSessionGuardType_None:
			return confirmations, nil
		}
	}
	if len(confirmations) == 0 || manualConfirmationAvailable {
		return confirmations, nil
	}
	return confirmations, newAuthSessionError(steamGuardState(confirmations), confirmations, fmt.Errorf("steam guard confirmation required"))
}

func steamGuardState(confirmations []SteamGuardConfirmation) AuthSessionState {
	for _, confirmation := range confirmations {
		switch confirmation.Type {
		case EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode:
			return AuthSessionStateEmailCode
		case EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode:
			return AuthSessionStateDeviceCode
		case EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation,
			EAuthSessionGuardType_k_EAuthSessionGuardType_EmailConfirmation:
			return AuthSessionStateManualConfirmation
		case EAuthSessionGuardType_k_EAuthSessionGuardType_None:
			return AuthSessionStateNone
		}
	}
	return AuthSessionStateUnsupportedGuard
}

func authSessionTimeoutState(confirmations []SteamGuardConfirmation) AuthSessionState {
	if steamGuardState(confirmations) == AuthSessionStateManualConfirmation {
		return AuthSessionStateManualConfirmation
	}
	return AuthSessionStateTimeout
}

func (a *Auth) submitSteamGuardCode(ctx context.Context, clientID, steamID uint64, code string, codeType EAuthSessionGuardType) error {
	updateResp := new(CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response)
	if err := a.authServiceCall(ctx, http.MethodPost, "UpdateAuthSessionWithSteamGuardCode", &CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{
		ClientId: uint64Ptr(clientID),
		Steamid:  uint64Ptr(steamID),
		Code:     stringPtr(code),
		CodeType: &codeType,
	}, updateResp); err != nil {
		return err
	}
	if url := updateResp.GetAgreementSessionUrl(); url != "" {
		return newAuthSessionError(AuthSessionStateAgreement, nil, fmt.Errorf("steam guard agreement required: %s", url))
	}
	return nil
}

func (a *Auth) authServiceCall(ctx context.Context, method, name string, req protobuf.Message, resp protobuf.Message) error {
	encoded, err := req.MarshalVT()
	if err != nil {
		return newAuthSessionError(AuthSessionStateProtobuf, nil, err)
	}
	params := url.Values{}
	params.Set("input_protobuf_encoded", base64.StdEncoding.EncodeToString(encoded))

	endpoint := a.authServiceEndpoint() + name + "/v1/"
	var httpReq *http.Request
	if method == http.MethodGet {
		httpReq, err = http.NewRequestWithContext(ctx, method, endpoint+"?"+params.Encode(), nil)
	} else {
		httpReq, err = http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(params.Encode()))
		if err == nil {
			httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return newAuthSessionError(AuthSessionStateTransport, nil, err)
	}

	httpResp, err := a.authServiceClient().Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return newAuthSessionError(AuthSessionStateCanceled, nil, err)
		}
		return newAuthSessionError(AuthSessionStateTransport, nil, err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return newAuthSessionError(AuthSessionStateTransport, nil, err)
	}
	if eresult := httpResp.Header.Get("x-eresult"); eresult != "" && eresult != "1" {
		msg := httpResp.Header.Get("x-error_message")
		if msg == "" {
			msg = httpResp.Status
		}
		return newAuthSessionError(AuthSessionStateDenied, nil, fmt.Errorf("%s failed: eresult %s (%s)", name, eresult, msg))
	}
	if httpResp.StatusCode != http.StatusOK {
		return newAuthSessionError(AuthSessionStateTransport, nil, fmt.Errorf("%s failed: %s: %s", name, httpResp.Status, string(body)))
	}
	if err := resp.UnmarshalVT(body); err != nil {
		return newAuthSessionError(AuthSessionStateProtobuf, nil, err)
	}
	return nil
}

func (a *Auth) authServiceEndpoint() string {
	if a.authServiceBaseURL != "" {
		return a.authServiceBaseURL
	}
	return defaultAuthServiceBaseURL
}

func (a *Auth) authServiceClient() *http.Client {
	if a.authServiceHTTPClient != nil {
		return a.authServiceHTTPClient
	}
	return http.DefaultClient
}

func encryptPassword(password, modHex, expHex string) (string, error) {
	mod := new(big.Int)
	if _, ok := mod.SetString(modHex, 16); !ok {
		return "", fmt.Errorf("invalid RSA modulus %q", modHex)
	}
	exp := new(big.Int)
	if _, ok := exp.SetString(expHex, 16); !ok {
		return "", fmt.Errorf("invalid RSA exponent %q", expHex)
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &rsa.PublicKey{N: mod, E: int(exp.Int64())}, []byte(password))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}

func waitAuthPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return newAuthSessionError(AuthSessionStateCanceled, nil, ctx.Err())
	}
}
