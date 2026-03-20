//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package baggage

import (
	"runtime"
)

type BaggageContainer struct {
	baggage interface{}
}

//go:norace
func (bc *BaggageContainer) Clone() interface{} {
	return &BaggageContainer{bc.baggage}
}

func GetBaggageFromGLS() *Baggage {
	gls := runtime.GetBaggageContainerFromGLS()
	if gls == nil {
		return nil
	}
	p := gls.(*BaggageContainer).baggage
	if p != nil {
		return p.(*Baggage)
	} else {
		return nil
	}
}

func SetBaggageToGLS(baggage *Baggage) {
	runtime.SetBaggageContainerToGLS(&BaggageContainer{baggage})
}

func ClearBaggageInGLS() {
	SetBaggageToGLS(nil)
}

func DeleteBaggageMemberInGLS(key string) bool {
	if bInternal := GetBaggageFromGLS(); bInternal != nil {
		b := bInternal.DeleteMember(key)
		SetBaggageToGLS(&b)
		return true
	}
	return false
}
