package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ScienJus/kairos/internal/identity"
)

func TestOpenAPIExecutorTokenPattern(t *testing.T) {
	document, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)    ExecutorToken:\n.*?pattern: '([^']+)'`).FindSubmatch(document)
	if len(match) != 2 {
		t.Fatal("ExecutorToken pattern missing")
	}
	pattern, err := regexp.Compile(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	lengths := regexp.MustCompile(`(?s)    ExecutorToken:\n.*?const: ''\n.*?minLength: (\d+)\n.*?maxLength: (\d+)`).FindSubmatch(document)
	if len(lengths) != 3 {
		t.Fatal("ExecutorToken empty alternative or length bounds missing")
	}
	minLength, _ := strconv.Atoi(string(lengths[1]))
	maxLength, _ := strconv.Atoi(string(lengths[2]))
	values := []string{"", "identity", identity.ExecutorTokenPrefix + strings.Repeat("A", 33)}
	for _, size := range []int{31, 32, 33} {
		raw := bytes.Repeat([]byte{7}, size)
		values = append(values, identity.ExecutorTokenPrefix+base64.RawURLEncoding.EncodeToString(raw))
	}
	// Two unused bits in the final character must be zero; test every alphabet value.
	for _, last := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_" {
		values = append(values, identity.ExecutorTokenPrefix+strings.Repeat("A", 42)+string(last))
	}
	canonical := identity.ExecutorTokenPrefix + strings.Repeat("A", 43)
	values = append(values, canonical+"=", canonical+"\n", " "+canonical)
	for _, value := range values {
		_, err := identity.ExecutorTokenHash(value)
		// Empty is accepted only as an omitted optional Claim request credential.
		want := value == "" || err == nil
		length := utf8.RuneCountInString(value)
		got := value == "" || (length >= minLength && length <= maxLength && pattern.MatchString(value))
		if got != want {
			t.Errorf("Schema/server token mismatch for %q: schema=%v want=%v", value, got, want)
		}
	}
}

func TestOpenAPIBusinessOperationsDeclareForbidden(t *testing.T) {
	document, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var path, operation string
	var forbidden bool
	checked := 0
	operationHeader := regexp.MustCompile(`^    (get|post|put|patch|delete):$`)
	check := func() {
		if operation == "" || path == "/healthz" || path == "/api/v1/auth/config" || strings.HasPrefix(path, "/api/v1/identities") {
			return
		}
		checked++
		if !forbidden {
			t.Errorf("%s %s has no 403 response", operation, path)
		}
	}
	// Inspect operation blocks without rewriting the hand-maintained YAML document.
	for _, line := range strings.Split(string(document), "\n") {
		if line == "components:" {
			check()
			break
		}
		if strings.HasPrefix(line, "  /") {
			check()
			path, operation, forbidden = strings.TrimSuffix(strings.TrimSpace(line), ":"), "", false
			continue
		}
		if operationHeader.MatchString(line) {
			check()
			operation, forbidden = strings.TrimSuffix(strings.TrimSpace(line), ":"), false
		}
		if strings.HasPrefix(line, "        '403':") {
			forbidden = true
		}
	}
	if checked < 30 {
		t.Fatalf("only checked %d business operations", checked)
	}
}
