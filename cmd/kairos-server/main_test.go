package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestHTTPTimeoutConfigurationFromEnvironment(t *testing.T) {
	for _, name := range []string{"KAIROS_HTTP_READ_TIMEOUT", "KAIROS_HTTP_WRITE_TIMEOUT", "KAIROS_HTTP_IDLE_TIMEOUT"} {
		t.Setenv(name, "")
	}

	configured, err := httpTimeoutConfigurationFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if configured.read != time.Minute || configured.write != 2*time.Minute || configured.idle != 2*time.Minute {
		t.Fatalf("default HTTP timeouts = %+v", configured)
	}

	t.Setenv("KAIROS_HTTP_READ_TIMEOUT", "15s")
	t.Setenv("KAIROS_HTTP_WRITE_TIMEOUT", "45s")
	t.Setenv("KAIROS_HTTP_IDLE_TIMEOUT", "3m")
	configured, err = httpTimeoutConfigurationFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if configured.read != 15*time.Second || configured.write != 45*time.Second || configured.idle != 3*time.Minute {
		t.Fatalf("configured HTTP timeouts = %+v", configured)
	}
}

func TestHTTPServerReadTimeoutStopsSlowRequestBody(t *testing.T) {
	const readTimeout = 500 * time.Millisecond
	handlerStarted := make(chan struct{})
	bodyRead := make(chan error, 1)
	serverURL := startHTTPTimeoutTestServer(t, &http.Server{
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      2 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			close(handlerStarted)
			_, err := io.ReadAll(request.Body)
			bodyRead <- err
			writer.WriteHeader(http.StatusNoContent)
		}),
	})

	bodyReader, bodyWriter := io.Pipe()
	request, err := http.NewRequest(http.MethodPost, serverURL, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = 1
	clientResult := make(chan error, 1)
	go func() {
		response, requestErr := (&http.Client{Timeout: 3 * time.Second}).Do(request)
		if response != nil {
			response.Body.Close()
		}
		clientResult <- requestErr
	}()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	select {
	case readErr := <-bodyRead:
		var timeout net.Error
		if !errors.As(readErr, &timeout) || !timeout.Timeout() {
			t.Fatalf("slow body read error = %v, want timeout", readErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow body was not interrupted")
	}
	bodyWriter.Close()
	<-clientResult
}

func TestHTTPServerWriteTimeoutStopsSlowResponse(t *testing.T) {
	const writeTimeout = 500 * time.Millisecond
	responseWrite := make(chan error, 1)
	serverURL := startHTTPTimeoutTestServer(t, &http.Server{
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      writeTimeout,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			controller := http.NewResponseController(writer)
			if _, err := io.WriteString(writer, "started"); err != nil {
				responseWrite <- err
				return
			}
			if err := controller.Flush(); err != nil {
				responseWrite <- err
				return
			}
			time.Sleep(2 * writeTimeout)
			_, writeErr := io.WriteString(writer, "finished")
			if flushErr := controller.Flush(); flushErr != nil {
				responseWrite <- flushErr
				return
			}
			responseWrite <- writeErr
		}),
	})

	response, err := (&http.Client{Timeout: 3 * time.Second}).Get(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	select {
	case writeErr := <-responseWrite:
		var timeout net.Error
		if !errors.As(writeErr, &timeout) || !timeout.Timeout() {
			t.Fatalf("slow response write error = %v, want timeout", writeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow response was not interrupted")
	}
}

func startHTTPTimeoutTestServer(t *testing.T, server *http.Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close test server: %v", err)
		}
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve test HTTP: %v", err)
		}
	})
	return "http://" + listener.Addr().String()
}

func TestHTTPTimeoutConfigurationRejectsInvalidValues(t *testing.T) {
	for _, name := range []string{"KAIROS_HTTP_READ_TIMEOUT", "KAIROS_HTTP_WRITE_TIMEOUT", "KAIROS_HTTP_IDLE_TIMEOUT"} {
		t.Run(name, func(t *testing.T) {
			for _, configuredName := range []string{"KAIROS_HTTP_READ_TIMEOUT", "KAIROS_HTTP_WRITE_TIMEOUT", "KAIROS_HTTP_IDLE_TIMEOUT"} {
				t.Setenv(configuredName, "1m")
			}
			t.Setenv(name, "0s")
			if _, err := httpTimeoutConfigurationFromEnvironment(); err == nil || !strings.Contains(err.Error(), name+" must be a positive duration") {
				t.Fatalf("configuration error = %v", err)
			}
		})
	}

	t.Setenv("KAIROS_HTTP_READ_TIMEOUT", "not-a-duration")
	if _, err := httpTimeoutConfigurationFromEnvironment(); err == nil || !strings.Contains(err.Error(), "parse KAIROS_HTTP_READ_TIMEOUT") {
		t.Fatalf("parse error = %v", err)
	}

	t.Setenv("KAIROS_HTTP_READ_TIMEOUT", "1m")
	t.Setenv("KAIROS_HTTP_WRITE_TIMEOUT", "-1s")
	if _, err := httpTimeoutConfigurationFromEnvironment(); err == nil || !strings.Contains(err.Error(), "KAIROS_HTTP_WRITE_TIMEOUT must be a positive duration") {
		t.Fatalf("negative duration error = %v", err)
	}
}
