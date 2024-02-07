package core

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestEncodeBlock(t *testing.T) {
	block := Block{
		Height:       5,
		PreviousHash: []byte("previous hash"),
		Cmds: []RequestWrappedByServer{
			{Request: Request{Cmd: []byte("cmd1")}, ServerName: "Node0", SN: 1},
			{Request: Request{Cmd: []byte("cmd2")}, ServerName: "Node0", SN: 2},
		},
	}

	encodedBlock, err := encode(block)
	if err != nil {
		t.Fatal(err)
	}

	var newBlock Block
	if err = decode(encodedBlock, &newBlock); err != nil {
		t.Fatal(err)
	}

	if newBlock.Height != block.Height || !bytes.Equal(newBlock.PreviousHash, block.PreviousHash) ||
		len(newBlock.Cmds) != len(block.Cmds) {
		t.Fatal("blocks before and after encoding do not match")
	}

	for i, cmd := range newBlock.Cmds {
		if !cmd.Equal(&block.Cmds[i]) {
			t.Fatal("cmds before and after encoding do not match")
		}
	}

}

func TestEncodeHash(t *testing.T) {
	hash := []byte("mock a hash")
	stringFromHash := hex.EncodeToString(hash)
	hashFromString, err := hex.DecodeString(stringFromHash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, hashFromString) {
		t.Fatal("hashes before and after encoding do not match")
	}
}
