package steam

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"crypto/rand"
	"crypto/rsa"

	"github.com/golang/protobuf/proto"
	. "github.com/paralin/go-steam/protocol/protobuf/unified"
)

// authServiceBaseURL is the unauthenticated WebAPI endpoint used for the
// credential authentication flow.
const authServiceBaseURL = "https://api.steampowered.com/IAuthenticationService/"

// maxPollAttempts bounds how long we wait for an auth session to complete.
// With no Steam Guard the refresh token is available on the first poll.
const maxPollAttempts = 10

// getAccessTokenViaCredentials performs the IAuthenticationService handshake
// (the flow modern Steam requires in place of plaintext password logon) over
// the WebAPI and returns a refresh token suitable for use as
// CMsgClientLogon.AccessToken.
func (a *Auth) getAccessTokenViaCredentials(details *LogOnDetails) (string, error) {
	// 1. fetch the RSA public key for the account.
	rsaResp := new(CAuthentication_GetPasswordRSAPublicKey_Response)
	if err := authServiceCall(http.MethodGet, "GetPasswordRSAPublicKey",
		&CAuthentication_GetPasswordRSAPublicKey_Request{
			AccountName: proto.String(details.Username),
		}, rsaResp); err != nil {
		return "", err
	}

	// 2. encrypt the password with the returned key.
	encrypted, err := encryptPassword(details.Password, rsaResp.GetPublickeyMod(), rsaResp.GetPublickeyExp())
	if err != nil {
		return "", err
	}

	// 3. begin an auth session with the encrypted credentials.
	platform := EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient
	persistence := ESessionPersistence_k_ESessionPersistence_Persistent
	beginResp := new(CAuthentication_BeginAuthSessionViaCredentials_Response)
	if err := authServiceCall(http.MethodPost, "BeginAuthSessionViaCredentials",
		&CAuthentication_BeginAuthSessionViaCredentials_Request{
			AccountName:         proto.String(details.Username),
			EncryptedPassword:   proto.String(encrypted),
			EncryptionTimestamp: proto.Uint64(rsaResp.GetTimestamp()),
			PlatformType:        &platform,
			Persistence:         &persistence,
			WebsiteId:           proto.String("Client"),
			DeviceFriendlyName:  proto.String("go-steam"),
			DeviceDetails: &CAuthentication_DeviceDetails{
				DeviceFriendlyName: proto.String("go-steam"),
				PlatformType:       &platform,
			},
		}, beginResp); err != nil {
		return "", err
	}

	if msg := beginResp.GetExtendedErrorMessage(); msg != "" {
		return "", fmt.Errorf("begin auth session failed: %s", msg)
	}

	// 4. ensure no Steam Guard confirmation is required.
	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		pollResp := new(CAuthentication_PollAuthSessionStatus_Response)
		if err := authServiceCall(http.MethodPost, "PollAuthSessionStatus",
			&CAuthentication_PollAuthSessionStatus_Request{
				ClientId:  proto.Uint64(beginResp.GetClientId()),
				RequestId: beginResp.GetRequestId(),
			}, pollResp); err != nil {
			return "", err
		}

		if token := pollResp.GetRefreshToken(); token != "" {
			return token, nil
		}

		// Check if steamguard required
		confTypes := beginResp.GetAllowedConfirmations()
		for _, conf := range confTypes {
			confType := conf.GetConfirmationType()
			if confType == EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode {
				// If code is not sent yet
				if details.TwoFactorCode == "" {
					return "", fmt.Errorf("steam guard required: %v", confType)
				}

				// Sending code
				if err := submitSteamGuardCode(
					beginResp.GetClientId(),
					beginResp.GetSteamid(),
					details.TwoFactorCode,
					confType,
				); err != nil {
					return "", err
				}

				// Code sent
				details.TwoFactorCode = ""
			}
		}

		time.Sleep(time.Second)
	}

	// 5. poll until the session yields a refresh token.
	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		pollResp := new(CAuthentication_PollAuthSessionStatus_Response)
		if err := authServiceCall(http.MethodPost, "PollAuthSessionStatus",
			&CAuthentication_PollAuthSessionStatus_Request{
				ClientId:  proto.Uint64(beginResp.GetClientId()),
				RequestId: beginResp.GetRequestId(),
			}, pollResp); err != nil {
			return "", err
		}
		if token := pollResp.GetRefreshToken(); token != "" {
			return token, nil
		}
		time.Sleep(time.Second)
	}

	return "", fmt.Errorf("auth session did not complete after %d attempts", maxPollAttempts)
}

// submitSteamGuardCode sends Steam Guard code
func submitSteamGuardCode(clientID uint64, steamID uint64,
	code string, codeType EAuthSessionGuardType) error {

	updateResp := new(CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response)

	req := &CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{
		ClientId: proto.Uint64(clientID),
		Steamid:  proto.Uint64(steamID),
		Code:     proto.String(code),
		CodeType: &codeType,
	}

	err := authServiceCall(http.MethodPost, "UpdateAuthSessionWithSteamGuardCode",
		req, updateResp)

	if err != nil {
		return fmt.Errorf("failed to submit Steam Guard code: %v", err)
	}

	if updateResp.GetAgreementSessionUrl() != "" {
		return fmt.Errorf("email confirmation required, please visit: %s", updateResp.GetAgreementSessionUrl())
	}

	return nil
}

// authServiceCall invokes an IAuthenticationService method over the WebAPI,
// passing req as the base64-encoded `input_protobuf_encoded` parameter and
// unmarshalling the protobuf response body into resp. Read-only methods such as
// GetPasswordRSAPublicKey use GET; the rest use POST.
func authServiceCall(httpMethod, method string, req proto.Message, resp proto.Message) error {
	data, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("input_protobuf_encoded", base64.StdEncoding.EncodeToString(data))

	endpoint := authServiceBaseURL + method + "/v1/"

	var httpResp *http.Response
	if httpMethod == http.MethodGet {
		httpResp, err = http.Get(endpoint + "?" + params.Encode())
	} else {
		httpResp, err = http.PostForm(endpoint, params)
	}
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	body, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}

	// The WebAPI reports the Steam result in the x-eresult header; 1 is OK.
	if eresult := httpResp.Header.Get("x-eresult"); eresult != "" && eresult != "1" {
		msg := httpResp.Header.Get("x-error_message")
		if msg == "" {
			msg = httpResp.Status
		}
		return fmt.Errorf("%s failed: eresult %s (%s)", method, eresult, msg)
	}
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s failed: %s", method, httpResp.Status)
	}

	return proto.Unmarshal(body, resp)
}

// encryptPassword RSA-encrypts the password with the modulus and exponent
// (both hex-encoded) returned by GetPasswordRSAPublicKey, using PKCS#1 v1.5
// padding (as required by Steam), and returns the base64-encoded ciphertext.
func encryptPassword(password, modHex, expHex string) (string, error) {
	mod := new(big.Int)
	if _, ok := mod.SetString(modHex, 16); !ok {
		return "", fmt.Errorf("invalid RSA modulus %q", modHex)
	}
	exp := new(big.Int)
	if _, ok := exp.SetString(expHex, 16); !ok {
		return "", fmt.Errorf("invalid RSA exponent %q", expHex)
	}
	pub := &rsa.PublicKey{N: mod, E: int(exp.Int64())}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}
