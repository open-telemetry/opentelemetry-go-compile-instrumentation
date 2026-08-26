// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is a minimal Fiber server used for e2e instrumentation testing.
package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

var port = flag.String("port", "8080", "port to listen on")

func main() {
	flag.Parse()

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Get("/hello/:name", func(c *fiber.Ctx) error {
		name := c.Params("name")
		return c.JSON(fiber.Map{"message": "Hello " + name})
	})

	app.Get("/status/:code", func(c *fiber.Ctx) error {
		code, err := strconv.Atoi(c.Params("code"))
		if err != nil || code < 100 || code > 599 {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		return c.SendStatus(code)
	})

	addr := fmt.Sprintf(":%s", *port)
	if err := app.Listen(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
