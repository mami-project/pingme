package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mami-project/pingme"
)

func main() {

	cachepath := flag.String("cache", ".", "directory to store results in")
	bindto := flag.String("bind", ":8176", "address and port to bind server to")

	flag.Parse()

	r := mux.NewRouter()

	pa := pingme.NewPingAPI(*cachepath, 10)
	pa.AddRoutes(r)

	log.Printf("pingsrv: caching to %s, listening on %s", *cachepath, *bindto)

	log.Fatal(http.ListenAndServe(*bindto, r))
}
