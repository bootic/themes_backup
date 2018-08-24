package main

import (
	"flag"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/antonholmquist/jason"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

func RootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
}

type EventEntity struct {
	Topic         string
	UserId        int64
	UserName      string
	ItemType      string
	ItemId        int64
	CreatedOn     string
	ShopSubdomain string
	data          *jason.Object
}

func NewEvent(reader io.Reader) (*EventEntity, error) {
	obj, err := jason.NewObjectFromReader(reader)
	if err != nil {
		return nil, err
	}
	topic, err := obj.GetString("topic")
	if err != nil {
		return nil, err
	}
	userId, err := obj.GetInt64("user_id")
	if err != nil {
		return nil, err
	}
	userName, err := obj.GetString("user_name")
	if err != nil {
		return nil, err
	}
	createdOn, err := obj.GetString("created_on")
	if err != nil {
		return nil, err
	}
	subdomain, err := obj.GetString("shop_subdomain")
	if err != nil {
		return nil, err
	}
	evt := &EventEntity{
		Topic:         topic,
		UserId:        userId,
		UserName:      userName,
		CreatedOn:     createdOn,
		ShopSubdomain: subdomain,
		data:          obj,
	}

	return evt, nil
}

type App struct {
	dir        string
	eventsChan chan *EventEntity
}

func (app *App) start() {
	for event := range app.eventsChan {
		log.Printf("RECEIVED EVENT %s", event.Topic)
		themeDir, err := app.prepareDir(event)
		if err != nil {
			log.Printf("Could not prepare directory for %s. %s", event.ShopSubdomain, err.Error())
		} else {
			var err error
			switch event.Topic {
			case "themes.updated.templates.updated", "themes.updated.templates.created":
				err = app.processTemplate(themeDir, event)
			case "themes.updated.templates.deleted":
				err = app.processTemplateDeleted(themeDir, event)
			case "themes.updated.assets.updated", "themes.updated.assets.created":
				err = app.processAsset(themeDir, event)
			case "themes.updated.assets.deleted":
				err = app.processAssetDeleted(themeDir, event)
			}

			if err != nil {
				log.Printf("Error processing %s: %s", event.Topic, err.Error())
			}
		}
	}
}

func (app *App) prepareDir(event *EventEntity) (string, error) {
	path := filepath.Join(app.dir, event.ShopSubdomain)
	return path, os.MkdirAll(path, 0700)
}

func (app *App) processTemplate(themeDir string, event *EventEntity) error {
	fileName, err := event.data.GetString("_embedded", "item", "file_name")
	if err != nil {
		return err
	}
	body, err := event.data.GetString("_embedded", "item", "body")
	if err != nil {
		return err
	}
	path := filepath.Join(themeDir, fileName)
	err = ioutil.WriteFile(path, []byte(body), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (app *App) processTemplateDeleted(themeDir string, event *EventEntity) error {
	fileName, err := event.data.GetString("item_slug")
	if err != nil {
		return err
	}
	path := filepath.Join(themeDir, fileName)
	err = os.Remove(path)
	if err != nil {
		return err
	}

	return nil
}

func (app *App) processAsset(themeDir string, event *EventEntity) error {
	fileName, err := event.data.GetString("_embedded", "item", "file_name")
	if err != nil {
		return err
	}

	dir := filepath.Join(themeDir, "assets")
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fileName)
	link, err := event.data.GetString("_embedded", "item", "_links", "file", "href")
	if err != nil {
		return err
	}
	resp, err := http.Get(link)
	defer resp.Body.Close()
	if err != nil {
		return err
	}
	out, err := os.Create(path)
	defer out.Close()
	if err != nil {
		return err
	}
	_, err = io.Copy(out, resp.Body)
	return err
}

func (app *App) processAssetDeleted(themeDir string, event *EventEntity) error {
	fileName, err := event.data.GetString("item_slug")
	if err != nil {
		return err
	}
	path := filepath.Join(themeDir, "assets", fileName)
	err = os.Remove(path)
	if err != nil {
		return err
	}

	return nil
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Please send a request body", http.StatusBadRequest)
		return
	}
	event, err := NewEvent(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch event.Topic {
	case "activation":
		log.Printf("Activated hook for shop %s", event.ShopSubdomain)
		w.Header().Set("X-Hook-Pong", r.Header.Get("X-Hook-Ping"))
		http.Error(w, "", http.StatusNoContent)
	default:
		app.eventsChan <- event
		http.Error(w, event.Topic, http.StatusNoContent)
	}
}

func main() {
	maxProcs := runtime.NumCPU()
	runtime.GOMAXPROCS(maxProcs)

	var (
		host string
		dir  string
	)

	flag.StringVar(&host, "host", "localhost:3004", "host/port to serve HTTP endpoint")
	flag.StringVar(&dir, "dir", "./", "root directory to write Git repos into")
	flag.Parse()

	eventsChan := make(chan *EventEntity, 10)
	app := &App{
		dir:        dir,
		eventsChan: eventsChan,
	}
	go app.start()

	router := mux.NewRouter()
	router.HandleFunc("/", RootHandler).Methods("GET")
	router.HandleFunc("/events", app.ServeHTTP).Methods("POST")
	log.Printf("Serving on %s. Writing files to %s", host, dir)
	log.Fatal(http.ListenAndServe(host, handlers.LoggingHandler(os.Stdout, router)))
}
