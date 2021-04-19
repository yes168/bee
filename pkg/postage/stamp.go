// Copyright 2020 The Swarm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package postage

import (
	"bytes"
	"errors"

	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
)

// StampSize is the number of bytes in the serialisation of a stamp
const (
	StampSize = 105
	IndexSize = 8
)

var (
	// ErrOwnerMismatch is the error given for invalid signatures.
	ErrOwnerMismatch = errors.New("owner mismatch")
	// ErrStampInvalid is the error given if stamp cannot deserialise.
	ErrStampInvalid = errors.New("invalid stamp")
)

var _ swarm.Stamp = (*Stamp)(nil)

// Stamp represents a postage stamp as attached to a chunk.
type Stamp struct {
	batchID []byte // postage batch ID
	index   []byte // index of the batch
	sig     []byte // common r[32]s[32]v[1]-style 65 byte ECDSA signature of batchID|index|address by owner or grantee
}

// NewStamp constructs a new stamp from a given batch ID, index and signatures.
func NewStamp(batchID, index, sig []byte) *Stamp {
	return &Stamp{batchID, index, sig}
}

// BatchID returns the batch ID of the stamp.
func (s *Stamp) BatchID() []byte {
	return s.batchID
}

// Index returns the within-batch index of the stamp.
func (s *Stamp) Index() []byte {
	return s.index
}

// Sig returns the signature of the stamp by the user
func (s *Stamp) Sig() []byte {
	return s.sig
}

// MarshalBinary gives the byte slice serialisation of a stamp:
// batchID[32]|index[32]|SignatureUser[65]|SignatureOwner[65].
func (s *Stamp) MarshalBinary() ([]byte, error) {
	buf := make([]byte, StampSize)
	copy(buf, s.batchID)
	copy(buf, s.index)
	copy(buf[40:], s.sig)
	return buf, nil
}

// UnmarshalBinary parses a serialised stamp into id and signature.
func (s *Stamp) UnmarshalBinary(buf []byte) error {
	if len(buf) != StampSize {
		return ErrStampInvalid
	}
	s.batchID = buf[:32]
	s.index = buf[32:40]
	s.sig = buf[40:105]
	return nil
}

// toSignDigest creates a digest to represent the stamp which is to be signed by
// the owner.
func toSignDigest(addr, batchId, index []byte) ([]byte, error) {
	h := swarm.NewHasher()
	_, err := h.Write(addr)
	if err != nil {
		return nil, err
	}
	_, err = h.Write(batchId)
	if err != nil {
		return nil, err
	}
	_, err = h.Write(index)
	if err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// ValidStamp returns a stampvalidator function passed to protocols with chunk entrypoints.
func ValidStamp(batchStore Storer) func(chunk swarm.Chunk, stampBytes []byte) (swarm.Chunk, error) {
	return func(chunk swarm.Chunk, stampBytes []byte) (swarm.Chunk, error) {
		stamp := new(Stamp)
		err := stamp.UnmarshalBinary(stampBytes)
		if err != nil {
			return nil, err
		}
		b, err := batchStore.Get(stamp.BatchID())
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if err = stamp.Valid(chunk.Address(), b.Owner); err != nil {
			return nil, err
		}
		return chunk.WithStamp(stamp).WithBatch(b.Radius, b.Depth), nil
	}
}

// Valid checks the validity of the postage stamp; in particular:
// - authenticity - check batch is valid on the blockchain
// - authorisation - the batch owner is the stamp signer
// the validity  check is only meaningful in its association of a chunk
// this chunk address needs to be given as argument
func (s *Stamp) Valid(chunkAddr swarm.Address, ownerAddr []byte) error {
	toSign, err := toSignDigest(chunkAddr.Bytes(), s.batchID, s.index)
	if err != nil {
		return err
	}
	signerPubkey, err := crypto.Recover(s.sig, toSign)
	if err != nil {
		return err
	}
	signerAddr, err := crypto.NewEthereumAddress(*signerPubkey)
	if err != nil {
		return err
	}
	if !bytes.Equal(signerAddr, ownerAddr) {
		return ErrOwnerMismatch
	}
	return nil
}