// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

type Config struct {
	Host string
	Port int
}

func WrapConfig(c Config) Config {
	fmt.Println("wrapped config")
	return c
}

func main() {
	cfg := Config{Host: "localhost", Port: 8080}
	_ = cfg
}
