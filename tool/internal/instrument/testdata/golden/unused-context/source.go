// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

func BeforeOnly(n int) int { return n + 1 }
func AfterOnly(n int) int { return n + 1 }
func BothUnused(n int) int { return n + 1 }
func BeforeUses(n int) int { return n + 1 }
func AfterUses(n int) int { return n + 1 }
