package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	DIR = "./test"
)

func TestActivation(t *testing.T) {
	srv := setup()
	defer teardown()

	evt := `{
		"topic": "activation"
	}`

	rec := srv("POST", "/events", evt)
	if rec.Code != 204 {
		t.Errorf("Expected status 204, got %d - %s", rec.Code, rec.Body)
	}
}

func TestHandleTemplateEvent(t *testing.T) {
	srv := setup()
	defer teardown()

	evt := `{
		"sequence": 1,
		"shop_subdomain": "acme",
		"topic": "themes.updated.templates.created",
		"user_name": "Joe Bloggs",
		"user_id": 123,
		"created_on": "2018-08-10T20:00:00",
		"_embedded": {
			"item": {
				"file_name": "foo.html",
				"body": "Some HTML code here"
			}
		}
	}`

	rec := srv("POST", "/events", evt)
	if rec.Code != 204 {
		t.Errorf("Expected status 204, got %d - %s", rec.Code, rec.Body)
	}

	path := filepath.Join(DIR, "acme", "foo.html")
	if !fileExists(path) {
		t.Errorf("Expected file %s to exist, but it didn't", path)
	}

	if msg, ok := commitExists("acme", "Joe Bloggs: created foo.html - evt:1"); !ok {
		t.Errorf("Expected Git commit, but was '%s'", msg)
	}
}

func TestHandleThemeEvent(t *testing.T) {
	srv := setup()
	defer teardown()

	evt := `{
		"sequence": 1,
		"shop_subdomain": "acme",
		"topic": "themes.updated",
		"user_name": "Joe Bloggs",
		"user_id": 123,
		"created_on": "2018-08-10T20:00:00",
		"_embedded": {
			"item": {
				"_embedded": {
					"templates": [
						{
							"file_name": "foo.html",
							"body": "Some HTML code here"
						}
					]
				}
			}
		}
	}`

	rec := srv("POST", "/events", evt)
	if rec.Code != 204 {
		t.Errorf("Expected status 204, got %d - %s", rec.Code, rec.Body)
	}

	path := filepath.Join(DIR, "acme", "foo.html")
	if !fileExists(path) {
		t.Errorf("Expected file %s to exist, but it didn't", path)
	}

	if msg, ok := commitExists("acme", "Joe Bloggs: updated theme - evt:1"); !ok {
		t.Errorf("Expected Git commit, but was '%s'", msg)
	}
}

func setup() func(string, string, string) *httptest.ResponseRecorder {
	teardown()
	err := os.MkdirAll(DIR, 0700)
	if err != nil {
		log.Fatal(err)
	}

	app := NewApp(DIR)

	return func(method, path, json string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		reader := strings.NewReader(json)
		req, err := http.NewRequest(method, "http://example.com"+path, reader)
		if err != nil {
			log.Fatal(err)
		}

		app.ServeHTTP(rec, req)

		// sleep to give async goroutine to write to file
		time.Sleep(50 * time.Millisecond)

		return rec
	}
}

func teardown() {
	err := os.RemoveAll(DIR)
	if err != nil {
		log.Fatal(err)
	}
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	} else {
		return true
	}
}

func commitExists(subdomain, line string) (string, bool) {
	cmdStr := "cd " + filepath.Join(DIR, subdomain) + " && git log --pretty=format:%s"
	var out bytes.Buffer
	cmd := exec.Command("bash", "-c", cmdStr)
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	return out.String(), (out.String() == line)
}
