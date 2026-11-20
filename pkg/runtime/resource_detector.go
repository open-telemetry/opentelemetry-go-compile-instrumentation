// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/sdk/resource"
)

var resourceDetectors detectorRegistry

type detectorRegistry struct {
	sync.Mutex
	// sealed is set once newResource reads the registry; later registrations would never run.
	sealed    bool
	detectors []guardedDetector
}

// RegisterResourceDetector adds d to the detectors that build the resource of
// the auto-configured SDK providers. Call it from the init function of a hook
// package, which runs before SetupOTelSDK builds the resource.
//
// The resource merges in this order, and a later source overrides an earlier
// one for the same attribute key:
//  1. The built-in detectors (telemetry SDK, service, process, OS, container, host).
//  2. The registered detectors, in registration order.
//  3. The OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME environment variables.
//
// A detector that does not apply to the current environment must return an
// empty resource and a nil error. A detector that returns an error or panics
// is logged and skipped. Detection runs synchronously before the SDK starts
// and has no timeout, so a detector must not block.
//
// The name must be unique and identifies the detector in logs.
// RegisterResourceDetector returns false and does not register d when name is
// empty or already registered, d is nil, or the resource is already built.
func RegisterResourceDetector(name string, d resource.Detector) bool {
	if name == "" || d == nil {
		return false
	}

	resourceDetectors.Lock()
	defer resourceDetectors.Unlock()

	if resourceDetectors.sealed {
		Logger().Warn("resource detector registered after the resource was built, ignoring it", "detector", name)
		return false
	}
	for _, registered := range resourceDetectors.detectors {
		if registered.name == name {
			Logger().Warn("resource detector name already registered, ignoring it", "detector", name)
			return false
		}
	}
	resourceDetectors.detectors = append(resourceDetectors.detectors, guardedDetector{name: name, detector: d})
	return true
}

// seal returns the registered detectors in registration order and closes the
// registry for further registrations.
func (r *detectorRegistry) seal() []resource.Detector {
	r.Lock()
	defer r.Unlock()

	r.sealed = true
	detectors := make([]resource.Detector, 0, len(r.detectors))
	for _, d := range r.detectors {
		detectors = append(detectors, d)
	}
	return detectors
}

// guardedDetector runs a registered detector so that a panic in it cannot
// stop the SDK setup. It adds the detector name to every error.
type guardedDetector struct {
	name     string
	detector resource.Detector
}

func (g guardedDetector) Detect(ctx context.Context) (res *resource.Resource, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			res, err = nil, fmt.Errorf("resource detector %q: panic: %v", g.name, rec)
		}
	}()

	res, err = g.detector.Detect(ctx)
	if err != nil {
		// %w keeps resource.ErrPartialResource visible, so the partial
		// resource of the detector is still merged.
		return res, fmt.Errorf("resource detector %q: %w", g.name, err)
	}
	return res, nil
}
