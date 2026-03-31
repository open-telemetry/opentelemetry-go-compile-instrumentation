package segmentio

import (
	"context"
	"sync"
	"testing"
	"time"

	kafka "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type mockHookContext struct {
	params      map[int]interface{}
	returnVals  map[int]interface{}
	data        interface{}
	funcName    string
	packageName string
	skipCall    bool
}

func newMockHookContext() *mockHookContext {
	return &mockHookContext{
		params:      make(map[int]interface{}),
		returnVals:  make(map[int]interface{}),
		funcName:    "mockFunc",
		packageName: "mock",
	}
}

func (m *mockHookContext) SetSkipCall(skip bool) {
	m.skipCall = skip
}

func (m *mockHookContext) IsSkipCall() bool {
	return m.skipCall
}

func (m *mockHookContext) SetParam(index int, value interface{}) {
	m.params[index] = value
}

func (m *mockHookContext) GetParam(index int) interface{} {
	return m.params[index]
}

func (m *mockHookContext) GetParamCount() int {
	return len(m.params)
}

func (m *mockHookContext) SetReturnVal(index int, value interface{}) {
	m.returnVals[index] = value
}

func (m *mockHookContext) GetReturnVal(index int) interface{} {
	return m.returnVals[index]
}

func (m *mockHookContext) GetReturnValCount() int {
	return len(m.returnVals)
}

func (m *mockHookContext) SetData(data interface{}) {
	m.data = data
}

func (m *mockHookContext) GetData() interface{} {
	return m.data
}

func (m *mockHookContext) GetKeyData(key string) interface{} {
	if m.data == nil {
		return nil
	}
	dataMap, ok := m.data.(map[string]interface{})
	if !ok {
		return nil
	}
	return dataMap[key]
}

func (m *mockHookContext) SetKeyData(key string, val interface{}) {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	dataMap, ok := m.data.(map[string]interface{})
	if !ok {
		m.data = make(map[string]interface{})
		dataMap = m.data.(map[string]interface{})
	}
	dataMap[key] = val
}

func (m *mockHookContext) HasKeyData(key string) bool {
	if m.data == nil {
		return false
	}
	dataMap, ok := m.data.(map[string]interface{})
	if !ok {
		return false
	}
	_, exists := dataMap[key]
	return exists
}

func (m *mockHookContext) GetFuncName() string {
	return m.funcName
}

func (m *mockHookContext) GetPackageName() string {
	return m.packageName
}

func setupTestTracer() (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return sr, tp
}

func TestBeforeReadMessage(t *testing.T) {
	tests := []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupReader  func() *kafka.Reader
		expectData   bool
		validateData func(*testing.T, map[string]interface{})
	}{
		{
			name: "stores reader config into hook data",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "kafka")
			},
			setupReader: func() *kafka.Reader {
				return kafka.NewReader(kafka.ReaderConfig{
					Brokers:   []string{"localhost:9092"},
					GroupID:   "test-group",
					Partition: 2,
				})
			},
			expectData: true,
			validateData: func(t *testing.T, data map[string]interface{}) {
				assert.Equal(t, "localhost:9092", data["endpoint"])
				assert.Equal(t, "test-group", data["group_id"])
				assert.Equal(t, "2", data["partition"])
				assert.WithinDuration(t, time.Now(), data["start"].(time.Time), time.Second)
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "kafka")
			},
			setupReader: func() *kafka.Reader {
				return kafka.NewReader(kafka.ReaderConfig{
					Brokers: []string{"localhost:9092"},
					GroupID: "test-group",
				})
			},
			expectData: false,
		},
		{
			name: "empty brokers list gives empty endpoint",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "kafka")
			},
			setupReader: func() *kafka.Reader {
				return kafka.NewReader(kafka.ReaderConfig{
					Brokers: []string{},
					GroupID: "test-group",
				})
			},
			expectData: true,
			validateData: func(t *testing.T, data map[string]interface{}) {
				assert.Equal(t, "", data["endpoint"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = sync.Once{}

			tt.setupEnv(t)
			sr, tp := setupTestTracer()
			defer tp.Shutdown(context.Background())

			r := tt.setupReader()
			defer r.Close()

			ictx := newMockHookContext()
			BeforeReadMessage(ictx, context.Background(), r)
			assert.Equal(t, 0, len(sr.Ended()), "no span should be ended in BeforeReadMessage")

			if tt.expectData {
				data, ok := ictx.GetData().(map[string]interface{})
				require.True(t, ok, "expected data to be set")
				require.NotNil(t, data)

				if tt.validateData != nil {
					tt.validateData(t, data)
				}
			} else {
				assert.Nil(t, ictx.GetData(), "no data should be stored when instrumentation disabled")
			}
		})
	}
}

func TestAfterReadMessage(t *testing.T) {
	tests := []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupData    func() map[string]interface{}
		setupMessage func() kafka.Message
		readErr      error
		expectSpan   bool
		validateData func(*testing.T, map[string]interface{})
		validateSpan func(*testing.T, trace.Span)
	}{
		{
			name: "success stores open span for AfterMessageProcessing",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "kafka")
			},
			setupData: func() map[string]interface{} {
				return map[string]interface{}{
					"endpoint":  "localhost:9092",
					"group_id":  "test-group",
					"partition": "0",
					"start":     time.Now(),
				}
			},
			setupMessage: func() kafka.Message {
				return kafka.Message{Topic: "test-topic", Headers: []kafka.Header{}}
			},
			expectSpan: true,
			validateData: func(t *testing.T, data map[string]interface{}) {
				assert.NotNil(t, data["span"], "span must be stored for AfterMessageProcessing")
				assert.NotNil(t, data["ctx"], "ctx must be stored")

				span, ok := data["span"].(trace.Span)
				require.True(t, ok, "span must implement trace.Span")
				assert.True(t, span.IsRecording(), "span must still be open for AfterMessageProcessing to use")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = sync.Once{}

			tt.setupEnv(t)
			_, tp := setupTestTracer()
			defer tp.Shutdown(context.Background())

			ictx := newMockHookContext()
			if d := tt.setupData(); d != nil {
				ictx.SetData(d)
			}

			assert.NotPanics(t, func() {
				AfterReadMessage(ictx, context.Background(), tt.setupMessage(), tt.readErr)
			})
			if tt.expectSpan {
				data, ok := ictx.GetData().(map[string]interface{})
				require.True(t, ok, "expected data to be set")
				require.NotNil(t, data)

				if tt.validateData != nil {
					tt.validateData(t, data)
				}
			} else {
				assert.Nil(t, ictx.GetData(), "no data should be stored when instrumentation disabled")
			}
		})
	}
}
