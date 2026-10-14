package core

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
)

func NewRNG(seed string) (*rand.Rand, error) {
	b, err := hex.DecodeString(seed)
	if err != nil {
		return nil, err
	}

	if len(b) != 8 {
		return nil, fmt.Errorf("invalid seed length")
	}

	seedValue := int64(binary.BigEndian.Uint64(b))

	return rand.New(rand.NewSource(seedValue)), nil
}
