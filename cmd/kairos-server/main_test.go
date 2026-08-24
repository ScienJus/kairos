package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHandleCommand(t *testing.T) {
	originalVersion := version
	version = "v0.1.0-test"
	t.Cleanup(func() { version = originalVersion })

	t.Run("server mode", func(t *testing.T) {
		handled, err := handleCommand(nil, &bytes.Buffer{})
		if err != nil || handled {
			t.Fatalf("handleCommand() = (%v, %v), want (false, nil)", handled, err)
		}
	})

	t.Run("version", func(t *testing.T) {
		var output bytes.Buffer
		handled, err := handleCommand([]string{"--version"}, &output)
		if err != nil || !handled {
			t.Fatalf("handleCommand() = (%v, %v), want (true, nil)", handled, err)
		}
		if got, want := output.String(), "kairos-server v0.1.0-test\n"; got != want {
			t.Fatalf("version output = %q, want %q", got, want)
		}
	})

	t.Run("unsupported argument", func(t *testing.T) {
		handled, err := handleCommand([]string{"--unknown"}, &bytes.Buffer{})
		if !handled || err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("handleCommand() = (%v, %v), want handled usage error", handled, err)
		}
	})
}

func TestDatabaseConfigurationFromEnvironment(t *testing.T) {
	t.Run("default sqlite", func(t *testing.T) {
		t.Setenv("KAIROS_POSTGRES_DSN", "")
		t.Setenv("KAIROS_SQLITE_PATH", "")

		configuration := databaseConfigurationFromEnvironment()
		if configuration.backend != databaseBackendSQLite || configuration.location != "kairos.db" {
			t.Fatalf("database configuration = %+v, want default sqlite", configuration)
		}
	})

	t.Run("configured sqlite", func(t *testing.T) {
		t.Setenv("KAIROS_POSTGRES_DSN", "")
		t.Setenv("KAIROS_SQLITE_PATH", "/data/kairos.db")

		configuration := databaseConfigurationFromEnvironment()
		if configuration.backend != databaseBackendSQLite || configuration.location != "/data/kairos.db" {
			t.Fatalf("database configuration = %+v, want configured sqlite", configuration)
		}
	})

	t.Run("postgres takes precedence", func(t *testing.T) {
		t.Setenv("KAIROS_POSTGRES_DSN", "postgres://kairos:secret@database/kairos")
		t.Setenv("KAIROS_SQLITE_PATH", "/data/ignored.db")

		configuration := databaseConfigurationFromEnvironment()
		if configuration.backend != databaseBackendPostgres || configuration.location != "postgres://kairos:secret@database/kairos" {
			t.Fatalf("database configuration = %+v, want configured postgres", configuration)
		}
	})
}
