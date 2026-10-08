// demo-service is only the HTTP target for the local tunnel acceptance test.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	name := flag.String("name", "app", "response name")
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "meerkat:"+*name) })
	log.Fatal((&http.Server{Addr: ":3000", Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
