// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

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

	addr := fmt.Sprintf(":%s", *port)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
