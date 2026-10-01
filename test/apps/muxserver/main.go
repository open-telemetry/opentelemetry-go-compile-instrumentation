// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a minimal gorilla/mux server used for e2e instrumentation testing.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

var port = flag.String("port", "8080", "port to listen on")

func main() {
	flag.Parse()

	r := mux.NewRouter()
	// Custom handlers, not mux defaults. Instrumentation must still skip
	// http.route on 404/405 because Match returns true in these cases.
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	r.HandleFunc("/hello/{name}", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "Hello " + mux.Vars(req)["name"],
		})
	}).Methods(http.MethodGet)

	r.HandleFunc("/status/{code}", func(w http.ResponseWriter, req *http.Request) {
		code, err := strconv.Atoi(mux.Vars(req)["code"])
		if err != nil || code < 100 || code > 599 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(code)
	}).Methods(http.MethodGet)

	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/apps/{appId}", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"appId": mux.Vars(req)["appId"],
		})
	}).Methods(http.MethodGet)

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: r,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
