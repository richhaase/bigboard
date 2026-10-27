package tui

import (
	"context"

	"github.com/richhaase/bigboard/git"
)

type SettingsConfig struct {
	PersonalAccount string
	Organizations   []string
	Exclude         []string
}

type SettingsAccount struct {
	Login         string
	Organizations []string
}

type SettingsOptions struct {
	ConfigPath   string
	Initial      SettingsConfig
	Required     bool
	InitialError string
	Group        string
	Account      func(context.Context) (SettingsAccount, error)
	Discover     func(context.Context, SettingsConfig) ([]git.Repository, error)
	Save         func(context.Context, SettingsConfig, []git.Repository) ([]git.Repository, error)
}
