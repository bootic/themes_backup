package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"runtime"

	"github.com/bootic/themes_backup/server"
	"github.com/gorilla/handlers"
)

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

	app := server.NewApp(dir, nil)

	log.Printf("Serving on %s (PID %d). Writing files to %s", host, os.Getpid(), dir)
	log.Fatal(http.ListenAndServe(host, handlers.LoggingHandler(os.Stdout, app)))
}
