// Package oidc implements minimal OIDC authorization-code login.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes default openid email profile
	Scopes string
}

func (c Config) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.RedirectURL != ""
}

type Provider struct {
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	UserURL  string `json:"userinfo_endpoint"`
}

func Discover(ctx context.Context, issuer string) (Provider, error) {
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Provider{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Provider{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Provider{}, fmt.Errorf("oidc discovery %s", resp.Status)
	}
	var p Provider
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return Provider{}, err
	}
	if p.AuthURL == "" || p.TokenURL == "" {
		return Provider{}, fmt.Errorf("oidc discovery incomplete")
	}
	return p, nil
}

func (c Config) AuthCodeURL(p Provider, state string) string {
	scopes := c.Scopes
	if scopes == "" {
		scopes = "openid email profile"
	}
	v := url.Values{}
	v.Set("client_id", c.ClientID)
	v.Set("redirect_uri", c.RedirectURL)
	v.Set("response_type", "code")
	v.Set("scope", scopes)
	v.Set("state", state)
	return p.AuthURL + "?" + v.Encode()
}

type TokenResp struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

type UserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (c Config) Exchange(ctx context.Context, p Provider, code string) (TokenResp, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURL)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResp{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TokenResp{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return TokenResp{}, fmt.Errorf("token exchange: %s %s", resp.Status, string(body))
	}
	var tr TokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return TokenResp{}, err
	}
	return tr, nil
}

func (c Config) UserInfo(ctx context.Context, p Provider, accessToken string) (UserInfo, error) {
	if p.UserURL == "" {
		return UserInfo{}, fmt.Errorf("no userinfo endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.UserURL, nil)
	if err != nil {
		return UserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return UserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return UserInfo{}, fmt.Errorf("userinfo: %s %s", resp.Status, string(b))
	}
	var u UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return UserInfo{}, err
	}
	if u.Email == "" {
		u.Email = u.Sub + "@oidc.local"
	}
	if u.Name == "" {
		u.Name = u.Email
	}
	return u, nil
}
