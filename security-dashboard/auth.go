package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AccessAuth verifies the JWT Cloudflare Access attaches to every request
// that passed its login page. With it on, a request that reaches the pod any
// other way (tunnel misconfiguration, in-cluster curl) gets a 401 instead of
// the data. Leave CF_ACCESS_TEAM empty to run open, for example on a LAN.
type AccessAuth struct {
	team string
	aud  string
	log  *slog.Logger

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func NewAccessAuth(team, aud string, log *slog.Logger) *AccessAuth {
	a := &AccessAuth{team: strings.TrimSpace(team), aud: strings.TrimSpace(aud), log: log}
	if a.team == "" || a.aud == "" {
		log.Warn("access auth OFF: set CF_ACCESS_TEAM and CF_ACCESS_AUD to require a Cloudflare Access login")
	} else {
		log.Info("access auth on", "team", a.team)
	}
	return a
}

func (a *AccessAuth) Enabled() bool { return a.team != "" && a.aud != "" }

func (a *AccessAuth) Wrap(next http.Handler) http.Handler {
	if !a.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Cf-Access-Jwt-Assertion")
		if tok == "" {
			if c, err := r.Cookie("CF_Authorization"); err == nil {
				tok = c.Value
			}
		}
		if tok == "" {
			http.Error(w, "login required", http.StatusUnauthorized)
			return
		}
		email, err := a.verify(tok)
		if err != nil {
			a.log.Warn("access token rejected", "err", err, "ip", clientIP(r))
			http.Error(w, "login required", http.StatusUnauthorized)
			return
		}
		r.Header.Set("X-Dashboard-User", email)
		next.ServeHTTP(w, r)
	})
}

func (a *AccessAuth) issuer() string { return "https://" + a.team + ".cloudflareaccess.com" }

func (a *AccessAuth) verify(tok string) (string, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed token")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(hb, &hdr); err != nil {
		return "", err
	}
	if hdr.Alg != "RS256" {
		return "", fmt.Errorf("alg %q", hdr.Alg)
	}
	key, err := a.key(hdr.Kid)
	if err != nil {
		return "", err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return "", errors.New("bad signature")
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Aud   json.RawMessage `json:"aud"`
		Exp   int64           `json:"exp"`
		Nbf   int64           `json:"nbf"`
		Iss   string          `json:"iss"`
		Email string          `json:"email"`
		Sub   string          `json:"sub"`
	}
	if err := json.Unmarshal(pb, &claims); err != nil {
		return "", err
	}
	now := time.Now().Unix()
	if claims.Exp == 0 || now > claims.Exp+30 {
		return "", errors.New("expired")
	}
	if claims.Nbf != 0 && now < claims.Nbf-30 {
		return "", errors.New("not yet valid")
	}
	if claims.Iss != a.issuer() {
		return "", fmt.Errorf("issuer %q", claims.Iss)
	}
	if !audMatch(claims.Aud, a.aud) {
		return "", errors.New("audience mismatch")
	}
	if claims.Email == "" {
		return claims.Sub, nil
	}
	return claims.Email, nil
}

func audMatch(raw json.RawMessage, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, s := range many {
			if s == want {
				return true
			}
		}
	}
	return false
}

// key returns the signing key for kid, refreshing the JWKS at most once a
// minute when it is missing and at least once an hour otherwise.
func (a *AccessAuth) key(kid string) (*rsa.PublicKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if k, ok := a.keys[kid]; ok && time.Since(a.fetched) < time.Hour {
		return k, nil
	}
	if time.Since(a.fetched) < time.Minute && a.keys != nil {
		if k, ok := a.keys[kid]; ok {
			return k, nil
		}
		return nil, errors.New("unknown key id")
	}
	cl := &http.Client{Timeout: 10 * time.Second}
	res, err := cl.Get(a.issuer() + "/cdn-cgi/access/certs")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("certs: %s", res.Status)
	}
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(res.Body).Decode(&jwks); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
		eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
	}
	a.keys, a.fetched = keys, time.Now()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("unknown key id")
}
