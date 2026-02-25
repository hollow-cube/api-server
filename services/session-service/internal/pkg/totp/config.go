package totp

import "fmt"

type Config struct {
	PlayerID      string
	Active        bool
	Key           []byte
	RecoveryCodes []string
}

func NewConfigForPlayer(playerID string) (*Config, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	recoveryCodes, err := GenerateRecoveryCodes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate recovery codes: %w", err)
	}

	return &Config{
		PlayerID:      playerID,
		Active:        false,
		Key:           key,
		RecoveryCodes: recoveryCodes,
	}, nil
}
