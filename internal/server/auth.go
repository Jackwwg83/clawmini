package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const jwtTTL = 24 * time.Hour
const MinAuthSecretLength = 16

type authUserContextKey struct{}

// AuthUser is the authenticated user resolved from JWT.
type AuthUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
}

type jwtClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

// TokenAuth validates JWT user auth and device tokens.
type TokenAuth struct {
	DeviceToken string
	JWTSecret   []byte
	Users       *UserStore
}

func NewTokenAuth(deviceToken string, jwtSecret []byte, users *UserStore) *TokenAuth {
	return &TokenAuth{
		DeviceToken: strings.TrimSpace(deviceToken),
		JWTSecret:   append([]byte(nil), jwtSecret...),
		Users:       users,
	}
}

func NewTokenAuthFromEnv(users ...*UserStore) (*TokenAuth, error) {
	deviceToken := strings.TrimSpace(os.Getenv("CLAWMINI_DEVICE_TOKEN"))
	jwtSecret := strings.TrimSpace(os.Getenv("CLAWMINI_JWT_SECRET"))
	if err := ValidateAuthConfig(deviceToken, []byte(jwtSecret)); err != nil {
		return nil, err
	}
	var userStore *UserStore
	if len(users) > 0 {
		userStore = users[0]
	}
	return NewTokenAuth(deviceToken, []byte(jwtSecret), userStore), nil
}

func validateAuthSecret(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(trimmed) < MinAuthSecretLength {
		return fmt.Errorf("%s must be at least %d characters", name, MinAuthSecretLength)
	}
	return nil
}

func ValidateAuthConfig(deviceToken string, jwtSecret []byte) error {
	if err := validateAuthSecret("CLAWMINI_DEVICE_TOKEN", deviceToken); err != nil {
		return err
	}
	if err := validateAuthSecret("CLAWMINI_JWT_SECRET", string(jwtSecret)); err != nil {
		return err
	}
	return nil
}

func (a *TokenAuth) ValidateDeviceToken(token string) bool {
	return token != "" && strings.TrimSpace(token) == a.DeviceToken
}

func (a *TokenAuth) GenerateToken(user User) (string, error) {
	headerJSON, err := json.Marshal(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(jwtClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Exp:      time.Now().UTC().Add(jwtTTL).Unix(),
	})
	if err != nil {
		return "", err
	}

	head := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := head + "." + payload
	sig := a.jwtSign(signingInput)
	return signingInput + "." + sig, nil
}

func (a *TokenAuth) jwtSign(signingInput string) string {
	mac := hmac.New(sha256.New, a.JWTSecret)
	_, _ = mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *TokenAuth) ParseUserToken(rawToken string) (AuthUser, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return AuthUser{}, fmt.Errorf("missing token")
	}
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return AuthUser{}, fmt.Errorf("invalid token format")
	}
	signingInput := parts[0] + "." + parts[1]
	expectedSig := a.jwtSign(signingInput)
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(expectedSig)) != 1 {
		return AuthUser{}, fmt.Errorf("invalid signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AuthUser{}, fmt.Errorf("invalid token payload")
	}
	var claims jwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return AuthUser{}, fmt.Errorf("invalid token claims")
	}
	if claims.UserID == "" || claims.Username == "" || claims.Role == "" || claims.Exp <= 0 {
		return AuthUser{}, fmt.Errorf("invalid claims")
	}
	if time.Now().UTC().Unix() > claims.Exp {
		return AuthUser{}, fmt.Errorf("token expired")
	}

	authUser := AuthUser{
		ID:       claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
	}
	if a.Users != nil {
		user, err := a.Users.GetUserByID(claims.UserID)
		if err != nil {
			return AuthUser{}, err
		}
		authUser.Role = user.Role
		authUser.Username = user.Username
		authUser.DisplayName = user.DisplayName
	}
	if authUser.DisplayName == "" {
		authUser.DisplayName = authUser.Username
	}
	return authUser, nil
}

func UserFromContext(ctx context.Context) (AuthUser, bool) {
	user, ok := ctx.Value(authUserContextKey{}).(AuthUser)
	return user, ok
}

func UserFromRequest(r *http.Request) (AuthUser, bool) {
	return UserFromContext(r.Context())
}

func WithUserContext(ctx context.Context, user AuthUser) context.Context {
	return context.WithValue(ctx, authUserContextKey{}, user)
}

func ExtractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func (a *TokenAuth) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.ParseUserToken(ExtractToken(r))
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUserContext(r.Context(), user)))
	})
}

func (a *TokenAuth) AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromRequest(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != RoleAdmin {
			WriteError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}
