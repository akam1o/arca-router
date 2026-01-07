package netconf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestChunkedFramingRoundtrip(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "small message",
			message: `<rpc message-id="1"><get-config><source><running/></source></get-config></rpc>`,
		},
		{
			name:    "large message",
			message: strings.Repeat("x", 10000),
		},
		{
			name:    "empty message",
			message: "",
		},
		{
			name:    "message with special chars",
			message: `<rpc>]]>]]><hello>##\n</hello></rpc>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write chunked message
			var buf bytes.Buffer
			writer := NewFramingWriter(&buf, "1.1")
			if err := writer.WriteMessage([]byte(tt.message)); err != nil {
				t.Fatalf("WriteMessage failed: %v", err)
			}

			// Read chunked message
			reader := NewFramingReader(&buf, "1.1")
			got, err := reader.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage failed: %v", err)
			}

			if string(got) != tt.message {
				t.Errorf("ReadMessage mismatch:\nwant: %q\ngot:  %q", tt.message, string(got))
			}
		})
	}
}

func TestEOMFramingRoundtrip(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "small message",
			message: `<rpc message-id="1"><get-config><source><running/></source></get-config></rpc>`,
		},
		{
			name:    "large message",
			message: strings.Repeat("x", 10000),
		},
		{
			name:    "empty message",
			message: "",
		},
		{
			name:    "message with chunked framing chars",
			message: `<rpc>##\n#123\n</rpc>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write EOM message
			var buf bytes.Buffer
			writer := NewFramingWriter(&buf, "1.0")
			if err := writer.WriteMessage([]byte(tt.message)); err != nil {
				t.Fatalf("WriteMessage failed: %v", err)
			}

			// Read EOM message
			reader := NewFramingReader(&buf, "1.0")
			got, err := reader.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage failed: %v", err)
			}

			if string(got) != tt.message {
				t.Errorf("ReadMessage mismatch:\nwant: %q\ngot:  %q", tt.message, string(got))
			}
		})
	}
}

func TestChunkedFramingMultipleChunks(t *testing.T) {
	// Create a message larger than MaxChunkSize to force multiple chunks
	message := strings.Repeat("x", MaxChunkSize*2+100)

	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.1")
	if err := writer.WriteMessage([]byte(message)); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	// Verify multiple chunk headers are present
	output := buf.String()
	chunkCount := strings.Count(output, "#") - strings.Count(output, "##") // Count chunk headers, excluding end marker
	if chunkCount < 3 {                                                    // At least 3 chunks for MaxChunkSize*2+100
		t.Errorf("Expected at least 3 chunks, but got %d", chunkCount)
	}

	// Verify end marker
	if !strings.HasSuffix(output, ChunkEnd) {
		t.Errorf("Message does not end with %q", ChunkEnd)
	}

	// Read back and verify
	reader := NewFramingReader(&buf, "1.1")
	got, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if string(got) != message {
		t.Errorf("Message length mismatch: want %d, got %d", len(message), len(got))
	}
}

func TestChunkedFramingInvalidHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing hash",
			input: "123\ndata##\n",
		},
		{
			name:  "missing newline",
			input: "#123data##\n",
		},
		{
			name:  "invalid size",
			input: "#abc\ndata##\n",
		},
		{
			name:  "negative size",
			input: "#-1\ndata##\n",
		},
		{
			name:  "oversized chunk",
			input: "#99999\ndata##\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewFramingReader(strings.NewReader(tt.input), "1.1")
			_, err := reader.ReadMessage()
			if err == nil {
				t.Errorf("Expected error for invalid input, but got nil")
			}
		})
	}
}

func TestChunkedFramingMaxMessageSize(t *testing.T) {
	// Create a message that exceeds MaxMessageSize
	message := strings.Repeat("x", MaxMessageSize+1)

	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.1")
	if err := writer.WriteMessage([]byte(message)); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	// Reading should fail due to size limit
	reader := NewFramingReader(&buf, "1.1")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for oversized message, but got nil")
	}
}

func TestEOMFramingMaxMessageSize(t *testing.T) {
	// Create a message that exceeds MaxMessageSize
	message := strings.Repeat("x", MaxMessageSize+1) + EOMMarker

	reader := NewFramingReader(strings.NewReader(message), "1.0")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for oversized message, but got nil")
	}
}

func TestEOMFramingWithEOMInContent(t *testing.T) {
	// Message contains ]]> but not the full ]]>]]> marker
	message := `<rpc><data>This has ]]> in it</data></rpc>`

	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.0")
	if err := writer.WriteMessage([]byte(message)); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	reader := NewFramingReader(&buf, "1.0")
	got, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if string(got) != message {
		t.Errorf("ReadMessage mismatch:\nwant: %q\ngot:  %q", message, string(got))
	}
}

func BenchmarkChunkedFramingWrite(b *testing.B) {
	message := []byte(strings.Repeat("x", 1000))
	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writer.WriteMessage(message)
	}
}

func BenchmarkChunkedFramingRead(b *testing.B) {
	message := []byte(strings.Repeat("x", 1000))
	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.1")
	writer.WriteMessage(message)
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewFramingReader(bytes.NewReader(data), "1.1")
		reader.ReadMessage()
	}
}

func BenchmarkEOMFramingWrite(b *testing.B) {
	message := []byte(strings.Repeat("x", 1000))
	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.0")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		writer.WriteMessage(message)
	}
}

func BenchmarkEOMFramingRead(b *testing.B) {
	message := []byte(strings.Repeat("x", 1000))
	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.0")
	writer.WriteMessage(message)
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewFramingReader(bytes.NewReader(data), "1.0")
		reader.ReadMessage()
	}
}

func TestChunkedFramingOversizedHeader(t *testing.T) {
	// Create a header that exceeds MaxChunkHeaderLength
	oversizedHeader := "#" + strings.Repeat("9", MaxChunkHeaderLength) + "\n"
	input := oversizedHeader + "data##\n"

	reader := NewFramingReader(strings.NewReader(input), "1.1")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for oversized header, but got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum length") {
		t.Errorf("Expected 'exceeds maximum length' error, got: %v", err)
	}
}

func TestChunkedFramingTruncatedChunkData(t *testing.T) {
	// Chunk header says 10 bytes, but only 5 bytes provided
	input := "#10\nabcde"

	reader := NewFramingReader(strings.NewReader(input), "1.1")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for truncated chunk data, but got nil")
	}
}

func TestEOMFramingWithEOMMarkerInPayload(t *testing.T) {
	// Message contains the EOM marker itself
	message := `<rpc><data>` + EOMMarker + `</data></rpc>`

	var buf bytes.Buffer
	writer := NewFramingWriter(&buf, "1.0")
	err := writer.WriteMessage([]byte(message))

	// Should fail because payload contains EOM marker
	if err == nil {
		t.Errorf("Expected error when writing message with EOM marker, but got nil")
	}
	if !strings.Contains(err.Error(), "contains EOM marker") {
		t.Errorf("Expected 'contains EOM marker' error, got: %v", err)
	}
}

func TestChunkedFramingMissingNewline(t *testing.T) {
	// Chunk header without newline (will hit max length)
	input := "#123456789"

	reader := NewFramingReader(strings.NewReader(input), "1.1")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for missing newline, but got nil")
	}
}

func TestChunkedFramingHugeChunkSize(t *testing.T) {
	// Chunk size exceeds MaxMessageSize
	hugeSize := int64(MaxMessageSize) + 1
	input := fmt.Sprintf("#%d\n", hugeSize)

	reader := NewFramingReader(strings.NewReader(input), "1.1")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for chunk size exceeding message limit, but got nil")
	}
	if !strings.Contains(err.Error(), "exceeds message limit") {
		t.Errorf("Expected 'exceeds message limit' error, got: %v", err)
	}
}

func TestChunkedFramingOverflowProtection(t *testing.T) {
	// Two chunks that together exceed MaxMessageSize
	chunkSize := MaxMessageSize/2 + 1
	input := fmt.Sprintf("#%d\n%s#%d\n%s##\n",
		chunkSize, strings.Repeat("x", chunkSize),
		chunkSize, strings.Repeat("y", chunkSize))

	reader := NewFramingReader(strings.NewReader(input), "1.1")
	_, err := reader.ReadMessage()
	if err == nil {
		t.Errorf("Expected error for cumulative size overflow, but got nil")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("Expected 'exceeds limit' error, got: %v", err)
	}
}
