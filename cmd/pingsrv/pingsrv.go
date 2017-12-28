package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mami-project/pingme"
)

func main() {

	cachepath := flag.String("cache", "cache", "`directory` to store results in")
	roothtml := flag.String("root", "web/pingme.html", "`file` to serve for root (should contain client-side javascript)")
	bindto := flag.String("bind", "", "`address and port` to bind server to")
	crtfile := flag.String("crt", "", "`file` containing certificate for TLS")
	keyfile := flag.String("key", "", "`file` containing secret key for TLS")

	flag.Parse()

	r := mux.NewRouter()

	pa, err := pingme.NewPingAPI(*cachepath, *roothtml, 10)
	if err != nil {
		log.Fatal(err)
	}
	pa.AddRoutes(r)

	if *crtfile != "" && *keyfile != "" {
		if *bindto == "" {
			*bindto = ":443"
		}
		log.Printf("pingsrv: caching to %s, listening on %s (TLS)", *cachepath, *bindto)
		log.Fatal(http.ListenAndServeTLS(*bindto, *crtfile, *keyfile, r))
	} else {
		if *bindto == "" {
			*bindto = ":80"
		}
		log.Printf("pingsrv: caching to %s, listening on %s", *cachepath, *bindto)
		log.Fatal(http.ListenAndServe(*bindto, r))
	}
}
