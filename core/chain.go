package core

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
)

// Chain maintains the successive blocks with QC.
// In other words, the blocks maintained in the chain are committed.
// For concurrency safe: modification to this type should be protected in a locking environment.
type Chain struct {
	lock   sync.RWMutex
	blocks map[uint64]*BlockWithQC
	height uint64
}

// String converts the blocks in a chain to string, to make it more readable.
func (chain *Chain) String() string {
	var blockDesc string
	height := chain.height
	for i := 0; i <= int(height); i++ {
		block := chain.blocks[uint64(i)]
		blockDesc += block.String()
	}
	return blockDesc
}

// Equal compares if two chains are equal.
func (chain *Chain) Equal(chain2 *Chain) (bool, error) {
	chain.lock.RLock()
	chain2.lock.RLock()
	defer chain.lock.RUnlock()
	defer chain2.lock.RUnlock()
	if len(chain.blocks) != len(chain2.blocks) || chain.height != chain2.height {
		return false, errors.New("height is different")
	}
	for i, block1 := range chain.blocks {
		_, err := block1.Equal(chain2.blocks[i])
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// EqualIgnoreQC compares if two chains are equal without considering QCs.
func (chain *Chain) EqualIgnoreQC(chain2 *Chain) (bool, error) {
	chain.lock.RLock()
	chain2.lock.RLock()
	defer chain.lock.RUnlock()
	defer chain2.lock.RUnlock()
	if len(chain.blocks) != len(chain2.blocks) || chain.height != chain2.height {
		return false, errors.New("height is different")
	}
	for i, block1 := range chain.blocks {
		_, err := block1.Block.Equal(&chain2.blocks[i].Block)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// Candidate chain maintains the successive blocks (without needing QC).
// A proposal of new block is based on the highest block in the candidate chain.
// the variable blocks is in the format: map[height]map[currentBlockHash]*Block.
// For concurrency safe: modification to this type should be protected in a locking environment.
type CandidateChain struct {
	blocks map[uint64]map[string]*Block
	height uint64
}

// Select the highest block in the candidate chain.
// If there are more than one, select one at random.
// For concurrency safe: call of this function should be protected in a locking environment.
func (cc *CandidateChain) selectBlock() (*Block, error) {
	highBlocks, ok := cc.blocks[cc.height]
	blocksCount := len(highBlocks)
	if !ok || blocksCount == 0 {
		return nil, errors.New("the height does not match the blocks in the candidate chain")
	}
	ranNum := rand.Intn(len(highBlocks))
	i := 0
	for _, block := range highBlocks {
		if i == ranNum {
			return block, nil
		}
		i++
	}
	return nil, errors.New("fail to select a block from the candidate chain")
}

// String converts the blocks in a candidate chain to string, to make it more readable.
func (cc *CandidateChain) String() string {
	var blockDesc string
	height := cc.height
	for i := 0; i < int(height); i++ {
		blocks := cc.blocks[uint64(i)]
		oneStringOneHeight := fmt.Sprintf("[Height: %d...]", i)
		for _, block := range blocks {
			oneStringOneHeight += block.String()
		}
		blockDesc += oneStringOneHeight
		blockDesc += "          "
	}
	return blockDesc
}
