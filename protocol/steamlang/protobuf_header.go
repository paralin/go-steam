package steamlang

import (
	"fmt"
	"io"
)

const maxProtoBufHeaderLength = 1 << 20

type remainingReader interface {
	Len() int
}

func readProtoBufHeaderBytes(r io.Reader, headerLength int32) ([]byte, error) {
	if headerLength < 0 {
		return nil, fmt.Errorf("invalid protobuf header length: %d", headerLength)
	}
	if headerLength > maxProtoBufHeaderLength {
		return nil, fmt.Errorf("protobuf header length %d exceeds maximum %d", headerLength, maxProtoBufHeaderLength)
	}
	if rr, ok := r.(remainingReader); ok && int(headerLength) > rr.Len() {
		return nil, fmt.Errorf("protobuf header length %d exceeds remaining packet bytes %d", headerLength, rr.Len())
	}

	buf := make([]byte, int(headerLength))
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}
