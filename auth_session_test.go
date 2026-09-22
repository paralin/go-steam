package steam

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	protobuf "github.com/aperturerobotics/protobuf-go-lite"
	protocol "github.com/paralin/go-steam/protocol"
	steampb "github.com/paralin/go-steam/protocol/protobuf"
	unified "github.com/paralin/go-steam/protocol/protobuf/unified"
	steamlang "github.com/paralin/go-steam/protocol/steamlang"
)

// TestLogOnWithAccessTokenWritesCMAccessToken keeps passwords out of token-based logon.
func TestLogOnWithAccessTokenWritesCMAccessToken(t *testing.T) {
	// Capture the outgoing CM message without starting transport workers.
	client := &Client{
		events:  make(chan any, 1),
		session: &clientSession{ctx: context.Background(), conn: testConnection{}, writes: make(chan protocol.IMsg, 1), heartbeat: make(chan time.Duration, 1)},
	}
	auth := &Auth{client: client}

	// Authenticate with the reusable token alone.
	if err := auth.LogOn(context.Background(), &LogOnDetails{
		Username:    "alice",
		AccessToken: "access-token",
	}); err != nil {
		t.Fatal(err)
	}

	// Verify the wire message contains the token and omits the password.
	select {
	case msg := <-client.session.writes:
		clientMsg, ok := msg.(*protocol.ClientMsgProtobuf)
		if !ok {
			t.Fatalf("message = %T", msg)
		}
		body, ok := clientMsg.Body.(*steampb.CMsgClientLogon)
		if !ok {
			t.Fatalf("body = %T", clientMsg.Body)
		}
		if body.GetAccountName() != "alice" {
			t.Fatalf("account name = %q", body.GetAccountName())
		}
		if body.GetAccessToken() != "access-token" {
			t.Fatalf("access token = %q", body.GetAccessToken())
		}
		if body.GetPassword() != "" {
			t.Fatalf("password = %q", body.GetPassword())
		}
	default:
		t.Fatal("logon message was not written")
	}
}

// TestLogOnResponseRequestsWebAPINonce requests web authentication after accepted CM logon.
func TestLogOnResponseRequestsWebAPINonce(t *testing.T) {
	// Deliver an accepted logon response to the real authentication handler.
	client := &Client{
		events:  make(chan any, 1),
		session: &clientSession{ctx: context.Background(), conn: testConnection{}, writes: make(chan protocol.IMsg, 1), heartbeat: make(chan time.Duration, 1)},
	}
	auth := &Auth{client: client}
	auth.handleLogOnResponse(clientLogOnResponsePacket(t))

	// Publish authenticated state before requesting the web nonce.
	select {
	case event := <-client.events:
		if _, ok := event.(*LoggedOnEvent); !ok {
			t.Fatalf("event = %T", event)
		}
	default:
		t.Fatal("logged-on event was not emitted")
	}

	// The outgoing request uses the dedicated web-authentication message.
	select {
	case msg := <-client.session.writes:
		clientMsg, ok := msg.(*protocol.ClientMsgProtobuf)
		if !ok {
			t.Fatalf("message = %T", msg)
		}
		if clientMsg.GetMsgType() != steamlang.EMsg_ClientRequestWebAPIAuthenticateUserNonce {
			t.Fatalf("message type = %v", clientMsg.GetMsgType())
		}
		if _, ok := clientMsg.Body.(*steampb.CMsgClientRequestWebAPIAuthenticateUserNonce); !ok {
			t.Fatalf("body = %T", clientMsg.Body)
		}
	default:
		t.Fatal("web api nonce request was not written")
	}
}

// TestGetAccessTokenViaCredentialsReturnsRefreshToken preserves the reusable auth result.
func TestGetAccessTokenViaCredentialsReturnsRefreshToken(t *testing.T) {
	// Serve the RSA, authentication and polling methods through a real HTTP boundary.
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			req := new(unified.CAuthentication_GetPasswordRSAPublicKey_Request)
			readAuthRequest(t, r, req)
			if req.GetAccountName() != "alice" {
				t.Fatalf("account name = %q", req.GetAccountName())
			}
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			req := new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request)
			readAuthRequest(t, r, req)
			if req.GetAccountName() != "alice" {
				t.Fatalf("begin account name = %q", req.GetAccountName())
			}
			if req.GetEncryptedPassword() == "" {
				t.Fatal("encrypted password is empty")
			}
			if req.GetEncryptionTimestamp() != 123 {
				t.Fatalf("encryption timestamp = %d", req.GetEncryptionTimestamp())
			}
			if !req.GetRememberLogin() {
				t.Fatal("remember login is false")
			}
			if req.GetDeviceFriendlyName() != "go-steam" {
				t.Fatalf("device friendly name = %q", req.GetDeviceFriendlyName())
			}
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
			}
		case "PollAuthSessionStatus":
			req := new(unified.CAuthentication_PollAuthSessionStatus_Request)
			readAuthRequest(t, r, req)
			if req.GetClientId() != 42 {
				t.Fatalf("poll client id = %d", req.GetClientId())
			}
			if string(req.GetRequestId()) != "request" {
				t.Fatalf("poll request id = %q", req.GetRequestId())
			}
			return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	// The complete exchange yields a refresh token for the supplied credentials.
	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	token, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username:               "alice",
		Password:               "password",
		ShouldRememberPassword: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "refresh-token" {
		t.Fatalf("token = %q", token)
	}
}

// TestAuthServiceCallReportsEresultAsDenied preserves denial carried in successful HTTP responses.
func TestAuthServiceCallReportsEresultAsDenied(t *testing.T) {
	// Steam can encode an authentication failure in headers on an HTTP 200 response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-eresult", "5")
		w.Header().Set("x-error_message", "invalid password")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Expose the protocol denial instead of accepting the HTTP status alone.
	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	err := auth.authServiceCall(
		context.Background(),
		http.MethodPost,
		"PollAuthSessionStatus",
		&unified.CAuthentication_PollAuthSessionStatus_Request{},
		&unified.CAuthentication_PollAuthSessionStatus_Response{},
	)
	var authErr *AuthSessionError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T", err)
	}
	if authErr.State != AuthSessionStateDenied {
		t.Fatalf("state = %q", authErr.State)
	}
}

// TestGetAccessTokenViaCredentialsReportsSteamGuard retains the required confirmation method.
func TestGetAccessTokenViaCredentialsReportsSteamGuard(t *testing.T) {
	// Return the email-code challenge from Steam's initial authentication response.
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
				AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
					{
						ConfirmationType:  guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode),
						AssociatedMessage: stringPtr("a***@example.com"),
					},
				},
			}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	// A caller without a code receives the same challenge and account hint.
	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username: "alice",
		Password: "password",
	})
	var authErr *AuthSessionError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if authErr.State != AuthSessionStateEmailCode {
		t.Fatalf("state = %s", authErr.State)
	}
	if len(authErr.Confirmations) != 1 {
		t.Fatalf("confirmations = %d", len(authErr.Confirmations))
	}
	if authErr.Confirmations[0].Type != unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode {
		t.Fatalf("confirmation type = %v", authErr.Confirmations[0].Type)
	}
}

// TestGetAccessTokenViaCredentialsSubmitsSteamGuardCode submits a device code in the same session.
func TestGetAccessTokenViaCredentialsSubmitsSteamGuardCode(t *testing.T) {
	// Validate the device challenge response before allowing polling to complete.
	updated := false
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
				AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
					{ConfirmationType: guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode)},
				},
			}
		case "UpdateAuthSessionWithSteamGuardCode":
			req := new(unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request)
			readAuthRequest(t, r, req)
			if req.GetCode() != "12345" {
				t.Fatalf("guard code = %q", req.GetCode())
			}
			if req.GetCodeType() != unified.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode {
				t.Fatalf("guard code type = %v", req.GetCodeType())
			}
			updated = true
			return &unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response{}
		case "PollAuthSessionStatus":
			readAuthRequest(t, r, new(unified.CAuthentication_PollAuthSessionStatus_Request))
			return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	// Supply the device code with the credentials and complete authentication.
	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username:      "alice",
		Password:      "password",
		TwoFactorCode: "12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("steam guard update was not submitted")
	}
}

// TestGetAccessTokenViaCredentialsSubmitsEmailSteamGuardCode preserves the email code type.
func TestGetAccessTokenViaCredentialsSubmitsEmailSteamGuardCode(t *testing.T) {
	// Validate the email challenge response before allowing polling to complete.
	updated := false
	server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
		switch name {
		case "GetPasswordRSAPublicKey":
			readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
			return authRSAPublicKeyResponse(t)
		case "BeginAuthSessionViaCredentials":
			readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
			return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
				ClientId:  uint64Ptr(42),
				RequestId: []byte("request"),
				Steamid:   uint64Ptr(76561198000000000),
				AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
					{ConfirmationType: guardTypePtr(unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode)},
				},
			}
		case "UpdateAuthSessionWithSteamGuardCode":
			req := new(unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request)
			readAuthRequest(t, r, req)
			if req.GetCode() != "ABC123" {
				t.Fatalf("guard code = %q", req.GetCode())
			}
			if req.GetCodeType() != unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode {
				t.Fatalf("guard code type = %v", req.GetCodeType())
			}
			updated = true
			return &unified.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Response{}
		case "PollAuthSessionStatus":
			readAuthRequest(t, r, new(unified.CAuthentication_PollAuthSessionStatus_Request))
			return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
		default:
			t.Fatalf("unexpected auth method %q", name)
			return nil
		}
	})
	defer server.Close()

	// Supply the email code with the credentials and complete authentication.
	auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
	_, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
		Username: "alice",
		Password: "password",
		AuthCode: "ABC123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("steam guard update was not submitted")
	}
}

// TestGetAccessTokenViaCredentialsPollsManualSteamGuardConfirmation accepts out-of-band approval.
func TestGetAccessTokenViaCredentialsPollsManualSteamGuardConfirmation(t *testing.T) {
	for _, confirmationType := range []unified.EAuthSessionGuardType{
		unified.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation,
		unified.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailConfirmation,
	} {
		t.Run(confirmationType.String(), func(t *testing.T) {
			// Poll the original session without submitting an unrelated one-time code.
			server := newAuthServiceTestServer(t, func(t *testing.T, name string, r *http.Request) protobuf.Message {
				switch name {
				case "GetPasswordRSAPublicKey":
					readAuthRequest(t, r, new(unified.CAuthentication_GetPasswordRSAPublicKey_Request))
					return authRSAPublicKeyResponse(t)
				case "BeginAuthSessionViaCredentials":
					readAuthRequest(t, r, new(unified.CAuthentication_BeginAuthSessionViaCredentials_Request))
					return &unified.CAuthentication_BeginAuthSessionViaCredentials_Response{
						ClientId:  uint64Ptr(42),
						RequestId: []byte("request"),
						Steamid:   uint64Ptr(76561198000000000),
						AllowedConfirmations: []*unified.CAuthentication_AllowedConfirmation{
							{ConfirmationType: guardTypePtr(confirmationType)},
						},
					}
				case "PollAuthSessionStatus":
					req := new(unified.CAuthentication_PollAuthSessionStatus_Request)
					readAuthRequest(t, r, req)
					if req.GetClientId() != 42 {
						t.Fatalf("poll client id = %d", req.GetClientId())
					}
					if string(req.GetRequestId()) != "request" {
						t.Fatalf("poll request id = %q", req.GetRequestId())
					}
					return &unified.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: stringPtr("refresh-token")}
				case "UpdateAuthSessionWithSteamGuardCode":
					t.Fatal("manual confirmation should not submit a Steam Guard code")
					return nil
				default:
					t.Fatalf("unexpected auth method %q", name)
					return nil
				}
			})
			defer server.Close()

			// Complete the manual confirmation exchange using its retained request identity.
			auth := &Auth{authServiceBaseURL: server.URL + "/", authServiceHTTPClient: server.Client()}
			token, err := auth.getAccessTokenViaCredentials(context.Background(), &LogOnDetails{
				Username: "alice",
				Password: "password",
			})
			if err != nil {
				t.Fatal(err)
			}
			if token != "refresh-token" {
				t.Fatalf("token = %q", token)
			}
		})
	}
}

// clientLogOnResponsePacket creates an accepted Steam session with a two-second heartbeat.
func clientLogOnResponsePacket(t *testing.T) *protocol.Packet {
	t.Helper()
	return protoBodyPacket(t, steamlang.EMsg_ClientLogOnResponse, &steampb.CMsgClientLogonResponse{
		Eresult:               int32Ptr(int32(steamlang.EResult_OK)),
		HeartbeatSeconds:      int32Ptr(2),
		ClientSuppliedSteamid: uint64Ptr(76561198000000000),
	})
}

// protoBodyPacket frames a generated protobuf without optional header fields.
func protoBodyPacket(t *testing.T, eMsg steamlang.EMsg, msg protobuf.Message) *protocol.Packet {
	t.Helper()
	buf := new(bytes.Buffer)
	body, err := msg.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(eMsg)|steamlang.ProtoMask); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(buf, binary.LittleEndian, int32(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := buf.Write(body); err != nil {
		t.Fatal(err)
	}
	return &protocol.Packet{
		EMsg:    eMsg,
		IsProto: true,
		Data:    buf.Bytes(),
	}
}

// newAuthServiceTestServer serves versioned Steam authentication methods over local HTTP.
func newAuthServiceTestServer(t *testing.T, handle func(*testing.T, string, *http.Request) protobuf.Message) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 2 || parts[1] != "v1" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		writeAuthResponse(t, w, handle(t, parts[0], r))
	}))
}

// readAuthRequest decodes the generated request from either GET or POST encoding.
func readAuthRequest(t *testing.T, r *http.Request, msg protobuf.Message) {
	t.Helper()
	encoded := r.URL.Query().Get("input_protobuf_encoded")
	if encoded == "" {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		encoded = r.Form.Get("input_protobuf_encoded")
	}
	if encoded == "" {
		t.Fatal("missing input_protobuf_encoded")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := msg.UnmarshalVT(data); err != nil {
		t.Fatal(err)
	}
}

// writeAuthResponse sends the generated binary body consumed by the auth client.
func writeAuthResponse(t *testing.T, w http.ResponseWriter, msg protobuf.Message) {
	t.Helper()
	data, err := msg.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write(data)
	if err != nil {
		t.Fatal(err)
	}
}

// authRSAPublicKeyResponse provides a disposable RSA key for password encryption tests.
func authRSAPublicKeyResponse(t *testing.T) *unified.CAuthentication_GetPasswordRSAPublicKey_Response {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return &unified.CAuthentication_GetPasswordRSAPublicKey_Response{
		PublickeyMod: stringPtr(key.N.Text(16)),
		PublickeyExp: stringPtr(strconv.FormatInt(int64(key.E), 16)),
		Timestamp:    uint64Ptr(123),
	}
}

// uint32Ptr preserves optional integer presence in test messages.
func uint32Ptr(value uint32) *uint32 {
	return &value
}

// int32Ptr preserves optional signed integer presence in test messages.
func int32Ptr(value int32) *int32 {
	return &value
}

// guardTypePtr preserves optional confirmation-method presence in test messages.
func guardTypePtr(value unified.EAuthSessionGuardType) *unified.EAuthSessionGuardType {
	return &value
}

// testConnection accepts authentication messages without opening a socket.
type testConnection struct{}

// Read leaves packet delivery to the authentication test.
func (testConnection) Read() (*protocol.Packet, error) {
	return nil, nil
}

// Write accepts an authentication frame.
func (testConnection) Write([]byte) error {
	return nil
}

// Close has no transport resources to release.
func (testConnection) Close() error {
	return nil
}

// SetReadTimeout accepts the server's authenticated liveness deadline.
func (testConnection) SetReadTimeout(time.Duration) error { return nil }

// SetEncryptionKey leaves test authentication messages unencrypted.
func (testConnection) SetEncryptionKey([]byte) {}

// IsEncrypted reports the test transport's plaintext contract.
func (testConnection) IsEncrypted() bool {
	return false
}
