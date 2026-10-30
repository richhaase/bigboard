package main

import (
	"errors"

	keychain "github.com/keybase/go-keychain"
)

type keychainStore struct {
	query  func(keychain.Item) ([]keychain.QueryResult, error)
	add    func(keychain.Item) error
	update func(keychain.Item, keychain.Item) error
	remove func(keychain.Item) error
}

func newKeychainStore() *keychainStore {
	return &keychainStore{query: keychain.QueryItem, add: keychain.AddItem, update: keychain.UpdateItem, remove: keychain.DeleteItem}
}

func credentialItem() keychain.Item {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService("com.richhaase.bigboard.github")
	item.SetAccount("github.com")
	item.SetSynchronizable(keychain.SynchronizableNo)
	return item
}

func (s *keychainStore) Load() ([]byte, error) {
	item := credentialItem()
	item.SetMatchLimit(keychain.MatchLimitOne)
	item.SetReturnData(true)
	rows, err := s.query(item)
	if errors.Is(err, keychain.ErrorItemNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return append([]byte(nil), rows[0].Data...), nil
}

func (s *keychainStore) Save(data []byte) error {
	item := credentialItem()
	item.SetData(data)
	item.SetLabel("Bigboard GitHub sign-in")
	if err := s.add(item); !errors.Is(err, keychain.ErrorDuplicateItem) {
		return err
	}
	update := keychain.NewItem()
	update.SetData(data)
	return s.update(credentialItem(), update)
}

func (s *keychainStore) Delete() error {
	err := s.remove(credentialItem())
	if errors.Is(err, keychain.ErrorItemNotFound) {
		return nil
	}
	return err
}
