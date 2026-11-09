// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a minimal Echo server used for e2e instrumentation testing.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"
)

var port = flag.String("port", "8080", "port to listen on")

func main() {
	flag.Parse()

	e := echo.New()

	e.GET("/hello/:name", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"message": "Hello " + c.Param("name"),
		})
	})

	e.GET("/error", func(*echo.Context) error {
		return fmt.Errorf("echo context error")
	})

	e.GET("/returned-error", func(*echo.Context) error {
		return fmt.Errorf("handler returned error")
	})

	e.GET("/status/:code", func(c *echo.Context) error {
		code, err := strconv.Atoi(c.Param("code"))
		if err != nil || code < 100 || code > 599 {
			return c.NoContent(http.StatusBadRequest)
		}
		return c.NoContent(code)
	})

	if err := e.Start(":" + *port); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
