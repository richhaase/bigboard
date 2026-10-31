package main

import (
	"errors"
	"testing"

	keychain "github.com/keybase/go-keychain"
)

func TestKeychainStoreMissingItemsAndAtomicUpdate(t *testing.T) {
	calls := []string{}
	store := &keychainStore{
		query: func(keychain.Item) ([]keychain.QueryResult, error) {
			calls = append(calls, "query")
			return nil, keychain.ErrorItemNotFound
		},
		add:    func(keychain.Item) error { calls = append(calls, "add"); return keychain.ErrorDuplicateItem },
		update: func(keychain.Item, keychain.Item) error { calls = append(calls, "update"); return nil },
		remove: func(keychain.Item) error { calls = append(calls, "remove"); return keychain.ErrorItemNotFound },
	}
	if data, err := store.Load(); err != nil || data != nil {
		t.Fatal("missing credentials were not empty")
	}
	if err := store.Save([]byte("test credential document")); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[1] != "add" || calls[2] != "update" {
		t.Fatal("existing credential was not atomically updated")
	}
	if err := store.Delete(); err != nil {
		t.Fatal("missing credential deletion was not idempotent")
	}
}

func TestKeychainStoreReturnsReadCopyAndPropagatesFailures(t *testing.T) {
	data := []byte("test credential document")
	failure := errors.New("native Keychain failure")
	store := &keychainStore{
		query:  func(keychain.Item) ([]keychain.QueryResult, error) { return []keychain.QueryResult{{Data: data}}, nil },
		add:    func(keychain.Item) error { return failure },
		update: func(keychain.Item, keychain.Item) error { t.Fatal("unexpected update after failure"); return nil },
		remove: func(keychain.Item) error { return failure },
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	loaded[0] = 'X'
	if data[0] == 'X' {
		t.Fatal("credential read was aliased")
	}
	if !errors.Is(store.Save(data), failure) || !errors.Is(store.Delete(), failure) {
		t.Fatal("storage failure was hidden")
	}
}
