// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestMongoClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "mongoclient", "go", "build", "-a")

	testCases := []struct {
		name string
	}{
		{
			name: "basic",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			addr := StartMockMongoServer(t)

			output := f.Run("mongoclient", "-uri=mongodb://"+addr)
			require.Contains(t, output, "MongoDB operations completed successfully")

			spans := testutil.AllSpans(f.Traces())
			require.GreaterOrEqual(t, len(spans), 1, "expected at least 1 span (insert)")

			// Verify insert span matching the actual attributes from otelmongo
			insertSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("db.operation", "insert"),
			)

			// Assert MongoDB specific semantic conventions attributes
			testutil.RequireAttribute(t, insertSpan, "db.system", "mongodb")
			testutil.RequireAttribute(t, insertSpan, "db.operation", "insert")
			testutil.RequireAttribute(t, insertSpan, "db.name", "testdb")
			testutil.RequireAttribute(t, insertSpan, "db.mongodb.collection", "users")
			testutil.RequireAttribute(t, insertSpan, "net.peer.name", "127.0.0.1")
			testutil.RequireAttribute(t, insertSpan, "net.transport", "ip_tcp")
		})
	}
}

// StartMockMongoServer starts a minimal mock MongoDB wire protocol server.
func StartMockMongoServer(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleMockConnection(t, conn)
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
	})

	return listener.Addr().String()
}

func handleMockConnection(t *testing.T, conn net.Conn) {
	defer conn.Close()
	for {
		header := make([]byte, 16)
		_, err := io.ReadFull(conn, header)
		if err != nil {
			return
		}

		msgLen := binary.LittleEndian.Uint32(header[0:4])
		reqID := binary.LittleEndian.Uint32(header[4:8])
		opCode := binary.LittleEndian.Uint32(header[12:16])

		body := make([]byte, msgLen-16)
		_, err = io.ReadFull(conn, body)
		if err != nil {
			return
		}

		var bsonBytes []byte
		if opCode == 2004 { // OP_QUERY
			idx := 4
			for idx < len(body) && body[idx] != 0 {
				idx++
			}
			idx++    // skip null
			idx += 8 // skip skip/return
			if idx < len(body) {
				bsonBytes = body[idx:]
			}
		} else if opCode == 2013 { // OP_MSG
			if len(body) > 5 && body[4] == 0 {
				bsonBytes = body[5:]
			}
		}

		var doc bson.M
		if len(bsonBytes) > 0 {
			_ = bson.Unmarshal(bsonBytes, &doc)
		}

		// Prepare response BSON document
		var respDoc bson.M
		if doc != nil && (doc["isMaster"] != nil || doc["ismaster"] != nil || doc["hello"] != nil) {
			respDoc = bson.M{
				"ismaster":                     true,
				"maxBsonObjectSize":            16777216,
				"maxMessageSizeBytes":          48000000,
				"maxWriteBatchSize":            100000,
				"logicalSessionTimeoutMinutes": 30,
				"connectionId":                 1,
				"minWireVersion":               0,
				"maxWireVersion":               17,
				"ok":                           1.0,
			}
		} else {
			// Assume it is the insert command
			respDoc = bson.M{
				"n":  1,
				"ok": 1.0,
			}
		}

		var respErr error
		if opCode == 2004 {
			respErr = sendOpReply(conn, reqID, respDoc)
		} else {
			respErr = sendOpMsg(conn, reqID, respDoc)
		}
		if respErr != nil {
			return
		}
	}
}

func sendOpReply(conn net.Conn, responseTo uint32, responseDoc bson.M) error {
	docBytes, err := bson.Marshal(responseDoc)
	if err != nil {
		return err
	}

	msgLen := 16 + 20 + len(docBytes)
	resp := make([]byte, msgLen)
	binary.LittleEndian.PutUint32(resp[0:4], uint32(msgLen))
	binary.LittleEndian.PutUint32(resp[4:8], 0)
	binary.LittleEndian.PutUint32(resp[8:12], responseTo)
	binary.LittleEndian.PutUint32(resp[12:16], 1) // OP_REPLY

	binary.LittleEndian.PutUint32(resp[16:20], 8)
	binary.LittleEndian.PutUint64(resp[20:28], 0)
	binary.LittleEndian.PutUint32(resp[32:36], 1)
	copy(resp[36:], docBytes)

	_, err = conn.Write(resp)
	return err
}

func sendOpMsg(conn net.Conn, responseTo uint32, responseDoc bson.M) error {
	docBytes, err := bson.Marshal(responseDoc)
	if err != nil {
		return err
	}

	msgLen := 16 + 4 + 1 + len(docBytes)
	resp := make([]byte, msgLen)
	binary.LittleEndian.PutUint32(resp[0:4], uint32(msgLen))
	binary.LittleEndian.PutUint32(resp[4:8], 0)
	binary.LittleEndian.PutUint32(resp[8:12], responseTo)
	binary.LittleEndian.PutUint32(resp[12:16], 2013) // OP_MSG

	binary.LittleEndian.PutUint32(resp[16:20], 0)
	resp[20] = 0 // Section type 0
	copy(resp[21:], docBytes)

	_, err = conn.Write(resp)
	return err
}
