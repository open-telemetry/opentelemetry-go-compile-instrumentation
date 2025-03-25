// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"fmt"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api/instrumenter"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api/utils"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"strconv"
)

// TODO: remove server.address and put it into NetworkAttributesExtractor

type DbExperimentalAttributesEnabler interface {
	Enable() bool
}

type defaultDbExperimentalAttributesEnabler struct {
}

func (d defaultDbExperimentalAttributesEnabler) Enable() bool {
	return false
}

var experimentalAttributesEnabler = instrumenter.NewDefaultInstrumentEnabler()

type DbClientCommonAttrsExtractor[REQUEST any, RESPONSE any, GETTER DbClientCommonAttrsGetter[REQUEST]] struct {
	Getter           GETTER
	AttributesFilter func(attrs []attribute.KeyValue) []attribute.KeyValue
}

func (d *DbClientCommonAttrsExtractor[REQUEST, RESPONSE, GETTER]) GetSpanKey() attribute.Key {
	return utils.DB_CLIENT_KEY
}

func (d *DbClientCommonAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnStart(attributes []attribute.KeyValue, parentContext context.Context, request REQUEST) ([]attribute.KeyValue, context.Context) {
	return attributes, parentContext
}

func (d *DbClientCommonAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnEnd(attrs []attribute.KeyValue, context context.Context, request REQUEST, response RESPONSE, err error) ([]attribute.KeyValue, context.Context) {
	attrs = append(attrs, attribute.KeyValue{
		Key:   semconv.DBSystemNameKey,
		Value: attribute.StringValue(d.Getter.GetSystem(request)),
	})
	if d.AttributesFilter != nil {
		attrs = d.AttributesFilter(attrs)
	}
	return attrs, context
}

type DbClientAttrsExtractor[REQUEST any, RESPONSE any, GETTER DbClientAttrsGetter[REQUEST]] struct {
	Base DbClientCommonAttrsExtractor[REQUEST, RESPONSE, GETTER]
}

func (d *DbClientAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnStart(attrs []attribute.KeyValue, parentContext context.Context, request REQUEST) ([]attribute.KeyValue, context.Context) {
	attrs, parentContext = d.Base.OnStart(attrs, parentContext, request)
	if d.Base.AttributesFilter != nil {
		attrs = d.Base.AttributesFilter(attrs)
	}
	return attrs, parentContext
}

func (d *DbClientAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnEnd(attrs []attribute.KeyValue, context context.Context, request REQUEST, response RESPONSE, err error) ([]attribute.KeyValue, context.Context) {
	attrs, context = d.Base.OnEnd(attrs, context, request, response, err)
	attrs = append(attrs, attribute.KeyValue{
		Key:   semconv.DBQueryTextKey,
		Value: attribute.StringValue(d.Base.Getter.GetStatement(request)),
	}, attribute.KeyValue{
		Key:   semconv.DBOperationNameKey,
		Value: attribute.StringValue(d.Base.Getter.GetOperation(request)),
	}, attribute.KeyValue{
		Key:   semconv.ServerAddressKey,
		Value: attribute.StringValue(d.Base.Getter.GetServerAddress(request)),
	})
	batchSize := d.Base.Getter.GetBatchSize(request)
	if batchSize > 0 {
		attrs = append(attrs, attribute.KeyValue{Key: semconv.DBOperationBatchSizeKey, Value: attribute.IntValue(batchSize)})
	}
	dbNameSpace := d.Base.Getter.GetDbNamespace(request)
	if dbNameSpace != "" {
		attrs = append(attrs, attribute.KeyValue{Key: semconv.DBNamespaceKey, Value: attribute.StringValue(dbNameSpace)})
	}
	// TODO: add db.collection.name after doing sanitizing
	if d.Base.AttributesFilter != nil {
		attrs = d.Base.AttributesFilter(attrs)
	}
	if experimentalAttributesEnabler.Enable() {
		params := d.Base.Getter.GetParameters(request)
		if params != nil && len(params) > 0 {
			for i, param := range params {
				attrs = append(attrs, attribute.String("db.query.parameter."+strconv.Itoa(i), fmt.Sprintf("%v", param)))
			}
		}
	}
	return attrs, context
}

func (d *DbClientAttrsExtractor[REQUEST, RESPONSE, GETTER]) GetSpanKey() attribute.Key {
	return utils.DB_CLIENT_KEY
}

// TODO: sanitize sql
// TODO: batch sql
