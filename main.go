package main

import (
	"flag"
	"log"

	"github.com/kozwoj/gobbler/server"
)

func main() {
	port := flag.Int("port", 8080, "HTTP port to listen on")
	flag.Parse()

	s := server.New()
	if err := s.ListenAndServe(*port); err != nil {
		log.Fatal(err)
	}
}
