// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

type testKey string

type testListener struct {
	startTime       time.Time
	endTime         time.Time
	startAttributes []attribute.KeyValue
	endAttributes   []attribute.KeyValue
}

func (t *testListener) OnBeforeStart(parentContext context.Context, startTimestamp time.Time) context.Context {
	t.startTime = startTimestamp
	return context.WithValue(parentContext, testKey("test1"), "a")
}

func (t *testListener) OnBeforeEnd(
	ctx context.Context,
	startAttributes []attribute.KeyValue,
	_ time.Time,
) context.Context {
	t.startAttributes = startAttributes
	return context.WithValue(ctx, testKey("test2"), "a")
}

func (t *testListener) OnAfterStart(_ context.Context, endTimestamp time.Time) {
	t.endTime = endTimestamp
}

func (t *testListener) OnAfterEnd(_ context.Context, endAttributes []attribute.KeyValue, _ time.Time) {
	t.endAttributes = endAttributes
}

func TestShadower(t *testing.T) {
	originAttrs := []attribute.KeyValue{
		attribute.String("a", "b"),
		attribute.String("a1", "a1"),
		attribute.String("a2", "a2"),
		attribute.String("a3", "a3"),
	}

	n := NoopAttrsShadower{}
	num, newAttrs := n.Shadow(originAttrs)
	if num != len(originAttrs) {
		t.Fatal("origin attrs length is not equal to new attrs length")
	}
	for i := range num {
		if newAttrs[i].Value != originAttrs[i].Value {
			t.Fatal("origin attrs value is not equal to new attrs value")
		}
	}
}

func TestOnBeforeStart(t *testing.T) {
	w := &testListener{}
	newCtx := w.OnBeforeStart(context.Background(), time.UnixMilli(123412341234))
	if w.startTime.UnixMilli() != 123412341234 {
		t.Fatal("start time is not equal to new start time")
	}
	if newCtx.Value(testKey("test1")) != "a" {
		t.Fatal("key test1 is not equal to new key value")
	}
}

func TestOnBeforeEnd(t *testing.T) {
	w := &testListener{}
	w.OnBeforeEnd(context.Background(), []attribute.KeyValue{{
		Key:   "123",
		Value: attribute.StringValue("abcde"),
	}}, time.UnixMilli(123412341234))
	if w.startAttributes[0].Key != "123" {
		t.Fatal("start attribute key is not equal to new start attribute key")
	}
	if w.startAttributes[0].Value.AsString() != "abcde" {
		t.Fatal("start attribute value is not equal to new start attribute value")
	}
}

func TestOnAfterStart(t *testing.T) {
	w := &testListener{}
	w.OnAfterStart(context.Background(), time.UnixMilli(123412341234))
	if w.endTime.UnixMilli() != 123412341234 {
		t.Fatal("start time is not equal to new start time")
	}
}

func TestOnAfterEnd(t *testing.T) {
	w := &testListener{}
	w.OnAfterEnd(context.Background(), []attribute.KeyValue{{
		Key:   "123",
		Value: attribute.StringValue("abcde"),
	}}, time.UnixMilli(123412341234))
	if w.endAttributes[0].Key != "123" {
		t.Fatal("start attribute key is not equal to new start attribute key")
	}
	if w.endAttributes[0].Value.AsString() != "abcde" {
		t.Fatal("start attribute value is not equal to new start attribute value")
	}
}
