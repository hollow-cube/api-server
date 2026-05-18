package v1Public

import (
	"context"
	"errors"
	"time"

	"github.com/hollow-cube/api-server/api/auth"
	"github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/pkg/ox"
)

type (
	RedeemRequest struct {
		DPoP string `header:"DPoP"`
		Body RedeemBody
	}
	RedeemBody struct {
		LaunchCode string  `json:"launchCode"`
		ClientKind string  `json:"clientKind"` // "web" | "desktop"
		Label      *string `json:"label"`
	}
	RedeemResponse struct {
		AccessToken     string    `json:"accessToken"`
		AccessExpiresAt time.Time `json:"accessExpiresAt"`
		SessionID       string    `json:"sessionId"`
		MapID           *string   `json:"mapId,omitempty"`
	}
)

// POST /auth/redeem
func (s *Server) RedeemLaunchGrant(ctx context.Context, request RedeemRequest) (*RedeemResponse, error) {
	// THIS ENDPOINT IS UNAUTHENTICATED INTENTIONALLY via envoy. We do a verification
	// inside but be very careful about what comes before.

	kind := request.Body.ClientKind
	if kind != string(db.ApiClientKindWeb) && kind != string(db.ApiClientKindDesktop) {
		return nil, ox.BadRequest{}
	}

	// No ExpectKeyID: this is first contact, so we register whatever key the
	// proof carries and pin the session to its thumbprint.
	keyID, derSPKI, err := auth.VerifyDPoP(ctx, s.redis, s.conf.Auth.ExternalURL, auth.DPoPParams{
		Proof:  request.DPoP,
		Method: "POST",
		Path:   "/v1/auth/redeem",
	})
	if err != nil {
		return nil, ox.Unauthorized{}
	}

	lt := s.conf.Auth.Lifetime(kind)
	now := time.Now()

	type redeemed struct {
		sessionID string
		playerID  string
		mapID     *string
	}
	out, err := db.Tx(ctx, s.sessionStore, func(ctx context.Context, tx *db.Queries) (*redeemed, error) {
		// Row-locked + redeemed_at-null predicate makes the burn atomic against
		// a concurrent redeem of the same code.
		grant, err := tx.GetLaunchGrantForRedeem(ctx, util.Sha256b([]byte(request.Body.LaunchCode)))
		if errors.Is(err, db.ErrNoRows) {
			return nil, ox.Unauthorized{}
		} else if err != nil {
			return nil, err
		}

		clientID, err := tx.UpsertApiClient(ctx, db.UpsertApiClientParams{
			Kind:      db.ApiClientKind(kind),
			PublicKey: derSPKI,
			KeyID:     keyID,
			Label:     request.Body.Label,
		})
		if err != nil {
			return nil, err
		}

		// Revoke-and-replace: a fresh game vouch supersedes any prior session
		// for this (client, account).
		if err = tx.RevokeSessionsForClientAccount(ctx, clientID, grant.PlayerID); err != nil {
			return nil, err
		}

		sessionID, err := tx.CreateSession(ctx, db.CreateSessionParams{
			ClientID:          clientID,
			PlayerID:          grant.PlayerID,
			IdleExpiresAt:     now.Add(lt.IdleTTL),
			AbsoluteExpiresAt: now.Add(lt.AbsoluteTTL),
		})
		if err != nil {
			return nil, err
		}

		if err = tx.MarkLaunchGrantRedeemed(ctx, grant.ID, &sessionID); err != nil {
			return nil, err
		}

		return &redeemed{sessionID: sessionID, playerID: grant.PlayerID, mapID: grant.MapID}, nil
	})
	if err != nil {
		return nil, err
	}

	ttl := s.conf.Auth.AccessTokenTTL
	return &RedeemResponse{
		AccessToken:     s.keyring.Mint(out.sessionID, ttl),
		AccessExpiresAt: time.Now().Add(ttl),
		SessionID:       out.sessionID,
		MapID:           out.mapID,
	}, nil
}

type (
	RefreshAccessTokenRequest struct {
		DPoP string `header:"DPoP"`
		Body RefreshAccessTokenBody
	}
	RefreshAccessTokenBody struct {
		SessionID string `json:"sessionId"`
	}
	RefreshAccessTokenResponse struct {
		AccessToken     string    `json:"accessToken"`
		AccessExpiresAt time.Time `json:"accessExpiresAt"`
	}
)

// POST /auth/token
func (s *Server) RefreshAccessToken(ctx context.Context, request RefreshAccessTokenRequest) (*RefreshAccessTokenResponse, error) {
	// THIS ENDPOINT IS UNAUTHENTICATED INTENTIONALLY via envoy. We do a verification
	// inside but be very careful about what comes before.

	session, err := s.sessionStore.GetActiveSession(ctx, request.Body.SessionID)
	if errors.Is(err, db.ErrNoRows) {
		return nil, ox.Unauthorized{}
	} else if err != nil {
		return nil, err
	}

	if _, _, err = auth.VerifyDPoP(ctx, s.redis, s.conf.Auth.ExternalURL, auth.DPoPParams{
		Proof:       request.DPoP,
		Method:      "POST",
		Path:        "/v1/auth/token",
		ExpectKeyID: session.ClientKeyID,
	}); err != nil {
		return nil, ox.Unauthorized{}
	}

	// Slide the idle window, clamped to the absolute bound.
	lt := s.conf.Auth.Lifetime(string(session.ClientKind))
	newIdle := time.Now().Add(lt.IdleTTL)
	if newIdle.After(session.AbsoluteExpiresAt) {
		newIdle = session.AbsoluteExpiresAt
	}
	if err = s.sessionStore.TouchSession(ctx, request.Body.SessionID, newIdle); err != nil {
		return nil, err
	}

	ttl := s.conf.Auth.AccessTokenTTL
	return &RefreshAccessTokenResponse{
		AccessToken:     s.keyring.Mint(request.Body.SessionID, ttl),
		AccessExpiresAt: time.Now().Add(ttl),
	}, nil
}
