package steamlang

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestMsgHdrProtoBufDeserializeRejectsNegativeLength(t *testing.T) {
	var payload bytes.Buffer
	if err := binary.Write(&payload, binary.LittleEndian, uint32(ProtoMask)); err != nil {
		t.Fatalf("write msg: %v", err)
	}
	if err := binary.Write(&payload, binary.LittleEndian, int32(-1)); err != nil {
		t.Fatalf("write header length: %v", err)
	}

	err := NewMsgHdrProtoBuf().Deserialize(bytes.NewReader(payload.Bytes()))
	if err == nil {
		t.Fatal("expected error for negative protobuf header length")
	}
	if !strings.Contains(err.Error(), "invalid protobuf header length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgHdrProtoBufDeserializeRejectsLengthLargerThanRemainingBytes(t *testing.T) {
	var payload bytes.Buffer
	if err := binary.Write(&payload, binary.LittleEndian, uint32(ProtoMask)); err != nil {
		t.Fatalf("write msg: %v", err)
	}
	if err := binary.Write(&payload, binary.LittleEndian, int32(4)); err != nil {
		t.Fatalf("write header length: %v", err)
	}
	if _, err := payload.Write([]byte{1, 2, 3}); err != nil {
		t.Fatalf("write header payload: %v", err)
	}

	err := NewMsgHdrProtoBuf().Deserialize(bytes.NewReader(payload.Bytes()))
	if err == nil {
		t.Fatal("expected error when protobuf header exceeds remaining bytes")
	}
	if !strings.Contains(err.Error(), "exceeds remaining packet bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgHdrProtoBufDeserializeRejectsOversizedLengthWithoutLenAwareReader(t *testing.T) {
	var payload bytes.Buffer
	if err := binary.Write(&payload, binary.LittleEndian, uint32(ProtoMask)); err != nil {
		t.Fatalf("write msg: %v", err)
	}
	if err := binary.Write(&payload, binary.LittleEndian, int32(maxProtoBufHeaderLength+1)); err != nil {
		t.Fatalf("write header length: %v", err)
	}

	err := NewMsgHdrProtoBuf().Deserialize(io.NopCloser(bytes.NewReader(payload.Bytes())))
	if err == nil {
		t.Fatal("expected error for oversized protobuf header length")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgGCHdrProtoBufDeserializeRejectsNegativeLength(t *testing.T) {
	var payload bytes.Buffer
	if err := binary.Write(&payload, binary.LittleEndian, uint32(ProtoMask)); err != nil {
		t.Fatalf("write msg: %v", err)
	}
	if err := binary.Write(&payload, binary.LittleEndian, int32(-1)); err != nil {
		t.Fatalf("write header length: %v", err)
	}

	err := NewMsgGCHdrProtoBuf().Deserialize(bytes.NewReader(payload.Bytes()))
	if err == nil {
		t.Fatal("expected error for negative protobuf header length")
	}
	if !strings.Contains(err.Error(), "invalid protobuf header length") {
		t.Fatalf("unexpected error: %v", err)
	}
}
