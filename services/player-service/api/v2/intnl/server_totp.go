package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/totp"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/util"
	"github.com/jackc/pgx/v5"
	"github.com/skip2/go-qrcode"
)

var (
	errInvalidCode   = errors.New("invalid code")
	errNotConfigured = errors.New("not configured")
)

func (s *server) CheckTotp(ctx context.Context, request CheckTotpRequestObject) (CheckTotpResponseObject, error) {
	var code string
	if request.Params.Code != nil {
		code = *request.Params.Code
	}

	config, err := s.testTotpCode(ctx, request.PlayerId, code, false)
	if errors.Is(err, errInvalidCode) {
		return CheckTotp400Response{}, nil
	} else if errors.Is(err, errNotConfigured) {
		return CheckTotp404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to test totp code: %w", err)
	} else if config == nil {
		return CheckTotp401Response{}, nil
	}

	return CheckTotp200Response{}, nil
}

func (s *server) BeginTotpSetup(ctx context.Context, request BeginTotpSetupRequestObject) (BeginTotpSetupResponseObject, error) {
	pd, err := s.store.GetPlayerData(ctx, request.PlayerId)
	if errors.Is(err, pgx.ErrNoRows) {
		return BeginTotpSetup404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read player data: %w", err)
	}

	config, err := totp.NewConfigForPlayer(request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// This will insert the new config into the database only if there is not an *active* record currently.
	// If there is, false will be returned from the first argument and we can return an error.
	inserted, err := s.storageClient.AddTOTP(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to insert totp record: %w", err)
	} else if !inserted {
		return BeginTotpSetup409Response{}, nil // They already have TOTP configured
	}

	// The record has been inserted and may be validated (unless replaced by a future record)
	uri := totp.MakeURI("Hollow Cube", pd.Username, config.Key)
	qr, err := qrcode.New(uri, qrcode.Low)
	if err != nil {
		return nil, fmt.Errorf("failed to generate qr code: %w", err)
	}

	// Convert the QR code to a bitset containing the white pixels
	const qrCodeSize = 41
	qr.DisableBorder = true
	qrImage := qr.Image(qrCodeSize)
	image := util.NewBitSet(qrImage.Bounds().Dx() * qrImage.Bounds().Dy())
	for y := 0; y < qrImage.Bounds().Dy(); y++ {
		for x := 0; x < qrImage.Bounds().Dx(); x++ {
			r, _, _, _ := qrImage.At(x, y).RGBA()
			if r != 0 {
				image.Set(y*qrImage.Bounds().Dx() + x)
			}
		}
	}

	return BeginTotpSetup201JSONResponse{TotpSetupResponseJSONResponse{
		Uri:           uri,
		QrCodeSize:    qrCodeSize,
		QrCode:        image.String(),
		RecoveryCodes: config.RecoveryCodes,
	}}, nil
}

func (s *server) CompleteTotpSetup(ctx context.Context, request CompleteTotpSetupRequestObject) (CompleteTotpSetupResponseObject, error) {
	code := request.Body.Code

	config, err := s.testTotpCode(ctx, request.PlayerId, code, true)
	if code == "" || errors.Is(err, errInvalidCode) {
		return CompleteTotpSetup400Response{}, nil
	} else if errors.Is(err, errNotConfigured) {
		return CompleteTotpSetup404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to test totp code: %w", err)
	} else if config == nil {
		return CompleteTotpSetup401Response{}, nil
	}

	// All good, activate it :)
	err = s.storageClient.ActivateTOTP(ctx, request.PlayerId, config.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to activate totp: %w", err)
	}
	return CompleteTotpSetup200Response{}, nil
}

func (s *server) RemoveTotp(ctx context.Context, request RemoveTotpRequestObject) (RemoveTotpResponseObject, error) {
	if err := s.storageClient.DeleteTOTP(ctx, request.PlayerId); err != nil {
		return nil, fmt.Errorf("failed to delete totp record: %w", err)
	}
	return RemoveTotp204Response{}, nil
}

// testTotpCode fetches and tests a TOTP code. The return types are as follows:
// Valid Code      -> !nil, nil
// Incorrect Code  -> nil, nil
// Invalid Code    -> nil, errInvalidCode
// Not set up      -> nil, errNotConfigured
// Any other error -> nil, error
//
// It is valid to test an empty string and check for errNotConfigured to determine if TOTP is setup or not
// unsafeAllowInactive will still test an inactive totp entry. Should not be set unless you know why its set.
func (s *server) testTotpCode(ctx context.Context, playerId, code string, unsafeAllowInactive bool) (*totp.Config, error) {
	if code != "" && len(code) != 6 {
		return nil, errInvalidCode
	}

	config, err := s.storageClient.GetTOTP(ctx, playerId)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, errNotConfigured
	} else if err != nil {
		return nil, fmt.Errorf("failed to read totp record: %w", err)
	} else if !unsafeAllowInactive && !config.Active {
		return nil, errNotConfigured
	}

	// Check last, current, and next codes
	match, err := totp.TestTriplet(config.Key, code)
	if err != nil {
		return nil, fmt.Errorf("failed to test totp code: %w", err)
	} else if !match {
		return nil, nil
	}

	return config, nil
}
