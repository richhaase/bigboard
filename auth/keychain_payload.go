package auth

import "encoding/json"

func (m *Manager) save(next credentials) error {
	data, err := json.Marshal(next)
	if err != nil || !validCredentials(next) {
		return ErrSignIn
	}
	if err := m.store.Save(data); err != nil {
		return ErrStorage
	}
	m.credentials, m.loaded, m.unsaved = next, true, nil
	return nil
}
