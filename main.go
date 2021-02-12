package main

import (
	"net/http"
	"os"

	log "github.com/sirupsen/logrus"
)

func init() {
	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	//log.SetLevel(log.WarnLevel)
}

func main() {
	// Inspired by
	// https://thenewstack.io/make-a-restful-json-api-go/
	r := NewRouter()

	// Bind to a port
	var port string
	port = "8000"

	log.Info("Listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
