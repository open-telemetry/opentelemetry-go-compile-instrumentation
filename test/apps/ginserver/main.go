// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a minimal gin server used for e2e instrumentation testing.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

var port = flag.String("port", "8080", "port to listen on")

func main() {
	flag.Parse()
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	r.GET("/hello/:name", func(c *gin.Context) {
		name := c.Param("name")
		c.JSON(http.StatusOK, gin.H{"message": "Hello " + name})
	})

	r.GET("/status/:code", func(c *gin.Context) {
		code, err := strconv.Atoi(c.Param("code"))
		if err != nil || code < 100 || code > 599 {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(code)
	})

	addr := fmt.Sprintf(":%s", *port)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
