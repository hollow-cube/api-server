package main

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v56/github"
	"github.com/hollow-cube/hc-services/services/session-service/config"
)

const (
	GithubAppID         = 1941434
	HollowCubeInstallID = 85558049
)

func newGithubClient(conf *config.Config) (*github.Client, error) {
	var itr http.RoundTripper
	if conf.Github.PrivateKey != "" {
		privateKey, err := base64.StdEncoding.DecodeString(conf.Github.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private key (base64): %w", err)
		}

		itr, err = ghinstallation.New(http.DefaultTransport, GithubAppID, HollowCubeInstallID, privateKey)
		if err != nil {
			return nil, err
		}
	}

	client := github.NewClient(&http.Client{Transport: itr})
	return client, nil
}
