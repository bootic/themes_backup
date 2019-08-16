package server

import (
	"errors"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antonholmquist/jason"
	"github.com/gorilla/mux"
)

func RootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
}

type EventEntity struct {
	EventId       int64
	Topic         string
	UserId        int64
	UserName      string
	ItemType      string
	ItemId        int64
	Item          *jason.Object
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
	userId, _ := obj.GetInt64("user_id")
	userName, _ := obj.GetString("user_name")
	createdOn, _ := obj.GetString("created_on")
	item, _ := obj.GetObject("_embedded", "item")
	subdomain, _ := obj.GetString("shop_subdomain")
	eventId, _ := obj.GetInt64("sequence")
	evt := &EventEntity{
		EventId:       eventId,
		Topic:         topic,
		UserId:        userId,
		UserName:      userName,
		CreatedOn:     createdOn,
		ShopSubdomain: subdomain,
		Item:          item,
		data:          obj,
	}

	return evt, nil
}

type fileGetter func(string) (io.ReadCloser, error)

func httpGet(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return resp.Body, err
	}
	return resp.Body, nil
}

type App struct {
	router     *mux.Router
	dir        string
	eventsChan chan *EventEntity
	GetFile    fileGetter
}

func (app *App) start() {
	for event := range app.eventsChan {
		log.Printf("RECEIVED EVENT %s", event.Topic)
		themeDir, err := app.prepareDir(event, event.Topic == "themes.updated")
		if err != nil {
			log.Printf("Could not prepare directory for %s. %s", event.ShopSubdomain, err.Error())
		} else {
			var err error
			var fileName string
			commit := true
			switch event.Topic {
			case "themes.updated":
				_, err = app.processTheme(themeDir, event.Item)
				fileName = "theme"
			case "themes.updated.templates.updated", "themes.updated.templates.created":
				fileName, err = app.processTemplate(themeDir, event.Item)
			case "themes.updated.templates.deleted":
				fileName, err = app.processTemplateDeleted(themeDir, event.data)
			case "themes.updated.assets.updated", "themes.updated.assets.created":
				fileName, err = app.processAsset(themeDir, event.Item)
			case "themes.updated.assets.deleted":
				fileName, err = app.processAssetDeleted(themeDir, event.data)
			default:
				commit = false
			}

			if err != nil {
				log.Printf("Error processing %s: %s", event.Topic, err.Error())
			} else if commit {
				err = app.commit(event.ShopSubdomain, fileName, event)
				if err != nil {
					log.Printf("Error committing %s: %s", event.Topic, err.Error())
				}
			}
		}
	}
}

func (app *App) commit(subdomain, fileName string, event *EventEntity) error {
	dir := filepath.Join(app.dir, subdomain)
	segments := strings.Split(event.Topic, ".")
	action := segments[len(segments)-1]
	logLine := event.UserName + ": " + action + " " + fileName + " - evt:" + strconv.FormatInt(event.EventId, 10)
	cmdStr := "cd " + dir + " && git init . && git add --all . && git commit -m '" + logLine + "'"
	cmd := exec.Command("bash", "-c", cmdStr)
	return cmd.Run()
}

func (app *App) prepareDir(event *EventEntity, isTheme bool) (string, error) {
	if event.ShopSubdomain == "" {
		return "", errors.New("Missing shop subdomain")
	}

	isDevTheme := false

	// if item is a theme, it should have a 'production' property
	if isTheme {
		isProd, err := event.Item.GetBoolean("production")
		if err != nil {
			isDevTheme = !isProd
		}
	} else { // ok, looks like the item is an asset or template
		theme, err := event.Item.GetObject("_embedded", "theme")
		if err != nil {
			isProd, err := theme.GetBoolean("production")
			if err != nil {
				isDevTheme = !isProd
			}
		}
	}

	path := filepath.Join(app.dir, event.ShopSubdomain)
	if isDevTheme {
		path += "-dev"
	}

	return path, os.MkdirAll(path, 0755)
}

func (app *App) processTheme(themeDir string, data *jason.Object) (string, error) {
	// remove all previous files
	matches, _ := filepath.Glob(themeDir + "/*.*")
	for _, f := range matches {
		if path.Base(f) != ".git" {
			os.RemoveAll(f)
		}
	}

	templates, err := data.GetObjectArray("_embedded", "templates")
	if err != nil {
		return "", err
	}
	for _, tpl := range templates {
		_, err = app.processTemplate(themeDir, tpl)
		if err != nil {
			return "", err
		}
	}

	assets, err := data.GetObjectArray("_embedded", "assets")
	if err != nil {
		// assets are optional, do not error out
		return "", nil
	}
	for _, asset := range assets {
		_, err = app.processAsset(themeDir, asset)
		if err != nil {
			return "", err
		}
	}
	return "", nil
}

func (app *App) processTemplate(themeDir string, data *jason.Object) (string, error) {
	fileName, err := data.GetString("file_name")
	if err != nil {
		return fileName, err
	}
	body, err := data.GetString("body")
	if err != nil {
		return fileName, err
	}
	path := filepath.Join(themeDir, fileName)
	err = ioutil.WriteFile(path, []byte(body), 0644)
	if err != nil {
		return fileName, err
	}

	return fileName, nil
}

func (app *App) processTemplateDeleted(themeDir string, data *jason.Object) (string, error) {
	fileName, err := data.GetString("item_slug")
	if err != nil {
		return fileName, err
	}
	path := filepath.Join(themeDir, fileName)
	err = os.Remove(path)
	if err != nil {
		return fileName, err
	}

	return fileName, nil
}

func (app *App) processAsset(themeDir string, data *jason.Object) (string, error) {
	fileName, err := data.GetString("file_name")
	if err != nil {
		return fileName, err
	}

	dir := filepath.Join(themeDir, "assets")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return fileName, err
	}
	path := filepath.Join(dir, fileName)
	link, err := data.GetString("_links", "file", "href")
	if err != nil {
		return fileName, err
	}
	fileData, err := app.GetFile(link)
	defer fileData.Close()
	if err != nil {
		return fileName, err
	}
	out, err := os.Create(path)
	defer out.Close()
	if err != nil {
		return fileName, err
	}
	_, err = io.Copy(out, fileData)
	return fileName, err
}

func (app *App) processAssetDeleted(themeDir string, data *jason.Object) (string, error) {
	fileName, err := data.GetString("item_slug")
	if err != nil {
		return fileName, err
	}
	path := filepath.Join(themeDir, "assets", fileName)
	err = os.Remove(path)
	if err != nil {
		return fileName, err
	}

	return fileName, nil
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app.router.ServeHTTP(w, r)
}

func (app *App) HandleEvents(w http.ResponseWriter, r *http.Request) {
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
		log.Println("Activated hook")
		w.Header().Set("X-Hook-Pong", r.Header.Get("X-Hook-Ping"))
		http.Error(w, "", http.StatusNoContent)
	default:
		app.eventsChan <- event
		http.Error(w, event.Topic, http.StatusNoContent)
	}
}
func NewApp(dir string, getFunc fileGetter) http.Handler {
	eventsChan := make(chan *EventEntity, 10)
	router := mux.NewRouter()
	if getFunc == nil {
		getFunc = httpGet
	}
	app := &App{
		router:     router,
		dir:        dir,
		eventsChan: eventsChan,
		GetFile:    getFunc,
	}
	router.HandleFunc("/", RootHandler).Methods("GET")
	router.HandleFunc("/events", app.HandleEvents).Methods("POST")

	go app.start()

	return app
}
