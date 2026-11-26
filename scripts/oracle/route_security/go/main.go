// Command route-security-oracle serves the gin and chi routers of the
// route-shape-security fixture on 127.0.0.1 for the route security oracle
// (T029): ports from the first three arguments.
package main

import (
	"log"
	"net/http"
	"os"

	"example.com/gvsecurity/chiapp"
	"example.com/gvsecurity/ginapp"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: route-security-oracle GINPORT CHIPORT SERVEDPORT")
	}
	go serve(os.Args[1], ginapp.Engine())
	go serve(os.Args[2], chiapp.Router())
	serve(os.Args[3], chiapp.Served())
}

func serve(port string, h http.Handler) {
	log.Fatal(http.ListenAndServe("127.0.0.1:"+port, h))
}
