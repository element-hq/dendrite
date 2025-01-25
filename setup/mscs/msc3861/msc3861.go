package msc3861

import (
	"github.com/element-hq/dendrite/setup"
	"github.com/matrix-org/gomatrixserverlib/fclient"
)

func Enable(m *setup.Monolith) error {
	client := fclient.NewClient()
	userVerifier, err := newMSC3861UserVerifier(
		m.UserAPI, m.Config.Global.ServerName,
		m.Config.MSCs.MSC3861, !m.Config.ClientAPI.GuestsDisabled,
		client,
	)
	if err != nil {
		return err
	}
	m.UserVerifierProvider.UserVerifier = userVerifier
	return nil
}
