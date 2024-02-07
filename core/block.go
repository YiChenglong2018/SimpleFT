package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/seafooler/SimpleFT/sign"
	"go.dedis.ch/kyber/v3/share"
)

// Block defines the contents in a block.
type Block struct {
	Proposer     string
	Height       uint64
	PreviousHash []byte // hash of its previous block (Height-1)
	Cmds         []RequestWrappedByServer
}

// Equal compares if two blocks are equal.
func (b *Block) Equal(b2 *Block) (bool, error) {
	if b.Proposer != b2.Proposer {
		return false, errors.New(fmt.Sprintf("proposers are different, b1.Proposer: %s, b2.Proposer: %s",
			b.Proposer, b2.Proposer))
	}

	if b.Height != b2.Height {
		return false, errors.New(fmt.Sprintf("heights are different, b1.Height: %d, b2.Height: %d",
			b.Height, b2.Height))
	}

	if !bytes.Equal(b.PreviousHash, b2.PreviousHash) {
		return false, errors.New(fmt.Sprintf("previous hashes are different, b1.PreviousHash: %x, "+
			"b2.PreviousHash: %x", b.PreviousHash, b2.PreviousHash))
	}

	//deal with the situation of nil
	if b.Cmds == nil {
		if b2.Cmds == nil {
			return true, nil
		} else {
			return false, errors.New("one block has Cmds of nil, while the other is not")
		}
	} else {
		if b2.Cmds == nil {
			return false, errors.New("one block has Cmds of nil, while the other is not")
		}
	}

	// either b.Cmds or b2.Cmds is not nil
	if len(b.Cmds) != len(b2.Cmds) {
		return false, errors.New("two blocks has different lengths of Cmds")
	}
	for i, cmd := range b.Cmds {
		if cmd.Equal(&b2.Cmds[i]) {
			return false, errors.New(fmt.Sprintf("two blocks has different Cmds at index: %d", i))
		}
	}
	return true, nil
}

// Block encapsulates the block with a constructed QC.
type BlockWithQC struct {
	Block
	QC []byte // threshold signature constructed from n-f replicas
}

// String returns the human-readable string for a BlockWithQC.
func (bwq *BlockWithQC) String() string {
	currentHash, _ := bwq.getHash()
	return fmt.Sprintf("{height: %d, proposer: %s, previosHash: %x, currentHash: %x}; ",
		bwq.Height, bwq.Proposer, bwq.PreviousHash, currentHash)
}

// Equal compares if two blocks with QC are equal.
func (bwq *BlockWithQC) Equal(b2 *BlockWithQC) (bool, error) {
	_, err := bwq.Block.Equal(&b2.Block)
	if err != nil {
		return false, err
	}

	if !bytes.Equal(bwq.QC, b2.QC) {
		return false, errors.New(fmt.Sprintf("qcs are different, b1.QC: %x, b2.QC: %x", bwq.QC, b2.QC))
	}
	return true, nil
}

// NewBlock creates a new block from some arguments.
func NewBlock(proposer string, height uint64, previousHash []byte, requests []RequestWrappedByServer) *Block {
	if requests == nil {
		requests = []RequestWrappedByServer{}
	}

	return &Block{
		Proposer:     proposer,
		Height:       height,
		PreviousHash: previousHash,
		Cmds:         requests, // simply set cmds as empty, to be repaired ...
	}
}

func (b *Block) getHash() ([]byte, error) {
	encodedBlock, err := encode(b)
	if err != nil {
		return nil, err
	}
	return genMsgHashSum(encodedBlock)
}

func (b *Block) getHashAsString() (string, error) {
	hash, err := b.getHash()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash), nil
}

// String returns the human-readable string for a Block.
func (b *Block) String() string {
	currentHash, _ := b.getHash()
	return fmt.Sprintf("{height: %d, proposer: %s, previosHash: %x, currentHash: %x}; ", b.Height, b.Proposer,
		b.PreviousHash, currentHash)
}

// create the intact threshold signature for a block.
func createIntactSig(msgBody interface{}, partialSigs [][]byte, pubPloy *share.PubPoly, quorumNum, nodeNum int) ([]byte, error) {
	// generate the bytes for signing

	encodedBytes, err := encode(msgBody)
	if err != nil {
		return nil, err
	}

	intactSig := sign.AssembleIntactTSPartial(partialSigs, pubPloy, encodedBytes, quorumNum, nodeNum)

	return intactSig, nil
}
