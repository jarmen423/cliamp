package spotify

import (
	"context"
	"fmt"
	"time"

	"github.com/bjarneo/cliamp/provider"
)

// Account returns the Spotify account cliamp is signed in as, read from
// /v1/me at most once per session. Implements provider.AccountSwitcher.
func (p *SpotifyProvider) Account(ctx context.Context) (provider.Account, error) {
	if err := p.ensureSession(); err != nil {
		return provider.Account{}, err
	}
	p.mu.Lock()
	if p.accountSet {
		account := p.account
		p.mu.Unlock()
		return account, nil
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := p.webAPI(ctx, "GET", "/v1/me", nil)
	if err != nil {
		return provider.Account{}, fmt.Errorf("spotify: read account: %w", err)
	}
	var me struct {
		ID          string         `json:"id"`
		DisplayName string         `json:"display_name"`
		Product     string         `json:"product"`
		Images      []spotifyImage `json:"images"`
	}
	if err := decodeBody(resp, &me); err != nil {
		return provider.Account{}, fmt.Errorf("spotify: parse account: %w", err)
	}
	name := me.DisplayName
	if name == "" {
		name = me.ID
	}
	account := provider.Account{ID: me.ID, Name: name, Detail: me.Product, ImageURL: pickCoverImage(me.Images)}
	p.mu.Lock()
	p.account = account
	p.accountSet = true
	// Reuse the same answer for ownership checks.
	p.userID = me.ID
	p.meFetched = true
	p.mu.Unlock()
	return account, nil
}

// SwitchAccount signs the current account out and signs in again from
// scratch. The interactive flow forces Spotify's consent screen
// (show_dialog in session.go), so the browser offers a different account
// instead of silently re-authorizing the same one. It blocks for as long
// as the browser sign-in takes, up to Authenticate's five minutes. The
// whole of ctx governs only the account read at the end: cancelling the
// caller does not abort a sign-in the user is mid-way through.
// Implements provider.AccountSwitcher.
func (p *SpotifyProvider) SwitchAccount(ctx context.Context) (provider.Account, error) {
	p.Close()
	if _, err := DeleteCreds(); err != nil {
		return provider.Account{}, fmt.Errorf("spotify: switch account: %w", err)
	}
	if err := p.Authenticate(); err != nil {
		return provider.Account{}, fmt.Errorf("spotify: switch account: %w", err)
	}
	return p.Account(ctx)
}
