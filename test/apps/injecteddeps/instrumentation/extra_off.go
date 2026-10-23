// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !injectedtag

package hook

func extraStatus() string { return "absent" }
