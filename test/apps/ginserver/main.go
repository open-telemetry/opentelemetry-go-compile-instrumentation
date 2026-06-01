// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal Gin HTTP server for integration testing.
// This server is designed to be instrumented with the otelc compile-time tool.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

var port = flag.String("port", "8084", "The server port")

func main() {
	flag.Parse()
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	r.GET("/hello", func(c *gin.Context) {
		name := c.Query("name")
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Hello %s", name)})
	})

	r.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	})

	addr := fmt.Sprintf(":%s", *port)
	if err := r.Run(addr); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
