package main

import (
	"context"
	"database/sql"
	"testing"
)

func TestLocalConfigUsesReplicaWhenReplicaSettingsArePresent(t *testing.T) {
	t.Setenv("SITE_DB_USERNAME_NOMAD", "reader")
	t.Setenv("SITE_DB_PASSWORD_NOMAD", "password")
	t.Setenv("SITE_DB_HOST", "replica.example")
	t.Setenv("SITE_DB", "cricket")

	called := false
	config, replica, err := localConfig(context.Background(), "local.db", func(context.Context) (*sql.DB, error) {
		called = true
		return &sql.DB{}, nil
	})
	if err != nil || !called || config.Data == nil || config.Ready == nil || replica == nil {
		t.Fatalf("config=%#v replica=%v called=%v err=%v", config, replica, called, err)
	}
}

func TestLocalConfigDoesNotRequireReplicaSettings(t *testing.T) {
	t.Setenv("SITE_DB_USERNAME_NOMAD", "")
	t.Setenv("SITE_DB_PASSWORD_NOMAD", "")
	t.Setenv("SITE_DB_HOST", "")
	t.Setenv("SITE_DB", "")

	config, replica, err := localConfig(context.Background(), "local.db", func(context.Context) (*sql.DB, error) {
		t.Fatal("local config should not open an unconfigured replica")
		return nil, nil
	})
	if err != nil || config.Data != nil || replica != nil {
		t.Fatalf("config=%#v replica=%v err=%v", config, replica, err)
	}
}
