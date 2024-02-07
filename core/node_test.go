package core

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"github.com/hashicorp/go-hclog"
	config "github.com/seafooler/yimchain/config"
	"github.com/seafooler/yimchain/sign"
	"net/rpc"
	"strconv"
	"testing"
	"time"
)

var logger = hclog.New(&hclog.LoggerOptions{
	Name:   "yimchain-test",
	Output: hclog.DefaultOutput,
	Level:  hclog.Info,
})

// set up a cluster environment for the testing
func setup(nodeNumber uint8, logLevel int, exp float64, sortitionEnabled bool, canProposeBlock []bool) ([]*Node, error) {
	names := make([]string, nodeNumber)
	clusterAddr := make(map[string]string, nodeNumber)
	clusterPort := make(map[string]int, nodeNumber)
	for i := 0; i < int(nodeNumber); i++ {
		name := fmt.Sprintf("node%d", i)
		names[i] = name
		clusterAddr[name] = "127.0.0.1"
		clusterPort[name] = 8000 + i
	}

	privKeys := make([]ed25519.PrivateKey, nodeNumber)
	pubKeys := make([]ed25519.PublicKey, nodeNumber)

	// create the ED25519 keys
	for i := 0; i < int(nodeNumber); i++ {
		privKeys[i], pubKeys[i] = sign.GenED25519Keys()
	}

	pubKeyMap := make(map[string]ed25519.PublicKey)
	for i := 0; i < int(nodeNumber); i++ {
		pubKeyMap[names[i]] = pubKeys[i]
	}

	// create the threshold keys
	numT := nodeNumber - nodeNumber/3
	shares, pubPoly := sign.GenTSKeys(int(numT), int(nodeNumber))

	if len(shares) != int(nodeNumber) {
		return []*Node{}, errors.New("number of generated private keys is incorrect")
	}

	confs := make([]*config.Config, nodeNumber)
	nodes := make([]*Node, nodeNumber)
	for i := 0; i < int(nodeNumber); i++ {
		confs[i] = config.New(names[i], 10, "127.0.0.1", clusterAddr, clusterPort, pubKeyMap, privKeys[i],
			pubPoly, shares[i], exp/float64(nodeNumber), sortitionEnabled, 8000+i, 9000+i, logLevel, canProposeBlock[i])
		nodes[i] = NewNode(confs[i])
		if err := nodes[i].StartP2PListen(); err != nil {
			panic(err)
		}
		if canProposeBlock[i] {
			// start rpc listen only if it is allowed to propose the blocks
			if err := nodes[i].StartRPCListen(); err != nil {
				panic(err)
			}
		}
	}

	for i := 0; i < int(nodeNumber); i++ {
		go nodes[i].EstablishP2PConns()
	}

	//Wait the all the connections to be established
	time.Sleep(time.Second)

	for i := 0; i < int(nodeNumber); i++ {
		go nodes[i].HandleMsgsLoop()
		if nodes[i].canProposeBlock {
			go nodes[i].ProposeBlockLoop()
		}
	}

	return nodes, nil
}

func clean(nodes []*Node) {
	for _, n := range nodes {
		n.trans.GetStreamContext().Done()
		n.trans.Close()
		close(n.shutdownCh)
	}
}

func TestNormalCase4Nodes(t *testing.T) {

	// do not use Node.ProposeBlockLoop() in this test, just pass []bool{false, false, false, false}
	nodes, err := setup(4, 3, 1, false, []bool{false, false, false, false})
	if err != nil {
		t.Fatal(err)
	}

	// propose the 1st block
	highestHeight := nodes[0].chain.height
	hash, _ := nodes[0].chain.blocks[highestHeight].getHash()
	block := NewBlock(nodes[0].name, highestHeight+1, hash, nil)

	if err := nodes[0].broadcast(ProposalTag, block, nil); err != nil {
		t.Fatal(err)
	}

	// propose ten more blocks
	for i := 1; i <= 10; i++ {
		// wait for a new block is received
		highestCandidateHeight := <-nodes[0].candidateHeightUpdated
		// propose a new one
		nodes[0].lock.RLock()
		previousBlock, err := nodes[0].candidateChain.selectBlock()
		nodes[0].lock.RUnlock()
		if err != nil {
			t.Fatal(err)
		}
		hash, err = previousBlock.getHash()
		if err != nil {
			t.Fatal(err)
		}
		block = NewBlock(nodes[0].name, highestCandidateHeight+1, hash, nil)

		if err := nodes[0].broadcast(ProposalTag, block, nil); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(time.Second)

	compareChainsIgnoreQC(nodes, t)
	logger.Info("all the nodes have the same chain", "blocks", nodes[0].chain.String())

	// close the connections
	clean(nodes)
}

// Test if two nodes propose the blocks with the same height
func TestConcurrentProposals(t *testing.T) {
	// do not use Node.ProposeBlockLoop() in this test, just pass []bool{false, false, false, false}
	nodes, err := setup(4, 3, 1, false, []bool{false, false, false, false})
	if err != nil {
		t.Fatal(err)
	}
	// node0
	highestHeight0 := nodes[0].chain.height
	hash0, _ := nodes[0].chain.blocks[highestHeight0].getHash()
	block0 := NewBlock(nodes[0].name, highestHeight0+1, hash0, nil)

	// node1
	highestHeight1 := nodes[1].chain.height
	hash1, _ := nodes[1].chain.blocks[highestHeight1].getHash()
	block1 := NewBlock(nodes[1].name, highestHeight1+1, hash1, nil)

	go func() {
		if err := nodes[1].broadcast(ProposalTag, block1, nil); err != nil {
			t.Fatal(err)
		}
	}()

	go func() {
		if err := nodes[0].broadcast(ProposalTag, block0, nil); err != nil {
			t.Fatal(err)
		}
	}()

	time.Sleep(time.Second * 2)

	compareChainsIgnoreQC(nodes, t)
	logger.Info("all the nodes have the same chain", "blocks", nodes[0].chain.String())

	// close the connections
	clean(nodes)
}

// Each node tries to propose a new block based on the highest block in the candidateChain
// Only when it win the sortition sortition, it can propose the new block
func TestNormalCaseWithSortitionLoop(t *testing.T) {
	// do not use Node.ProposeBlockLoop() in this test, just pass []bool{false, false, false, false}
	nodes, err := setup(4, 3, 1, true, []bool{false, false, false, false})
	if err != nil {
		t.Fatal(err)
	}

	// to record the sortition result of each round for each block
	// only if more than 2/3 nodes fail to propose a block in round r, can they run another round of r+1
	sortitionResults := make(map[uint64]map[uint64]map[string]bool)

	// propose ten more blocks
	for i := 1; i <= 10; i++ {
		sortitionResults[uint64(i)] = make(map[uint64]map[string]bool)
		for k := 0; ; k++ {
			sortitionResults[uint64(i)][uint64(k)] = make(map[string]bool)
			for j, node := range nodes {
				// wait for a new block is received
				var highestCandidateHeight uint64
				select {
				case highestCandidateHeight = <-node.candidateHeightUpdated:
					logger.Debug("receive candidate height updated", "node", j, "highestCandidateHeight", highestCandidateHeight)
				}
				//highestHeight := node.candidateChain.height
				ok, _ := node.sortitioner.Once(highestCandidateHeight + uint64(k))
				if ok {
					logger.Debug("!!! get the right to create a new block", "node", j, "height", highestCandidateHeight+1)
					node.lock.RLock()
					prevBlock, err := node.candidateChain.selectBlock()
					node.lock.RUnlock()
					if err != nil {
						t.Fatal(err)
					}
					hash, err := prevBlock.getHash()
					if err != nil {
						t.Fatal(err)
					}
					block := NewBlock(node.name, highestCandidateHeight+1, hash, nil)
					go func(n *Node, block *Block) {
						if err := n.broadcast(ProposalTag, block, nil); err != nil {
							t.Fatal(err)
						}
					}(node, block)
					// clear the msgs from channel
					for m := 0; m < len(nodes); m++ {
						if m == j {
							continue
						}
						<-nodes[m].candidateHeightUpdated
					}

					goto FirstLoop
				} else {
					node.candidateHeightUpdated <- highestCandidateHeight
					sortitionResults[uint64(i)][uint64(k)][node.name] = false
					logger.Debug("no right to create a new block", "node", j, "height", highestCandidateHeight+1)
				}
			}
			for {
				if len(sortitionResults[uint64(i)][uint64(k)]) >= nodes[0].quorumNum {
					break
				}
				time.Sleep(time.Millisecond * 500)
			}
		}
	FirstLoop:
	}

	time.Sleep(time.Second * 2)
	compareChainsIgnoreQC(nodes, t)
	logger.Info("all the nodes have the same chain", "blocks", nodes[0].chain.String())

	// close the connections
	clean(nodes)
}

// append num blocks directly
func appendBlocksDirectly(nodes []*Node, num uint64) {
	start := nodes[0].candidateChain.height
	for i := start + 1; i <= start+num; i++ {
		selectedBlock, _ := nodes[0].candidateChain.selectBlock()
		previousHash, _ := selectedBlock.getHash()
		block := &Block{
			Proposer:     nodes[0].name,
			Height:       i,
			PreviousHash: previousHash,
			Cmds:         nil,
		}
		blockHashAsString, _ := block.getHashAsString()
		for _, node := range nodes {
			<-node.candidateHeightUpdated
			node.candidateChain.blocks[i] = map[string]*Block{blockHashAsString: block}
			node.candidateChain.height++
			node.candidateHeightUpdated <- i
		}
	}
}

// broadcast blocks
func broadcastBlock(nodes []*Node, num int, t *testing.T) {
	// although only node0 propose blocks, we simply consume all the elements from channel: candidateHeightUpdated
	for i := 0; i < num; i++ {
		for _, node := range nodes {
			<-node.candidateHeightUpdated
		}

		selectedBlock, _ := nodes[0].candidateChain.selectBlock()
		highestCandidateHeight := selectedBlock.Height
		previousHash, _ := selectedBlock.getHash()

		block := NewBlock(nodes[0].name, highestCandidateHeight+1, previousHash, nil)
		if err := nodes[0].broadcast(ProposalTag, block, nil); err != nil {
			t.Fatal(err)
		}
	}
}

// ancestor blocks may be committed by blocks of different heights in different nodes
// thus, ignore the QCs comparison
func compareChainsIgnoreQC(nodes []*Node, t *testing.T) {
	for i, node := range nodes {
		for j, node2 := range nodes {
			if i == j {
				continue
			}
			_, err := node.chain.EqualIgnoreQC(node2.chain)
			if err != nil {
				t.Fatalf("committed chain in node %d does not match with node %d, err: %v", i, j, err)
			}
		}
	}
}

// when a block gets committed, all of its ancestor blocks should also be committed
func TestCommitAncestorBlocks(t *testing.T) {
	// do not use Node.ProposeBlockLoop() in this test, just pass []bool{false, false, false, false}
	nodes, err := setup(4, 3, 1, false, []bool{false, false, false, false})
	if err != nil {
		t.Fatal(err)
	}

	/************************************************************/
	/************************* round 1 **************************/
	/************************************************************/
	// append the blocks directly to the candidate blocks
	appendBlocksDirectly(nodes, 10)

	logger.Debug("candidate chain", "cc", nodes[0].candidateChain.String())

	// broadcast a new block to trigger the votes for it
	broadcastBlock(nodes, 1, t)

	time.Sleep(time.Second)

	compareChainsIgnoreQC(nodes, t)

	logger.Info("all the nodes have the same chain", "blocks", nodes[0].chain.String())

	/************************************************************/
	/************************* round 2 **************************/
	/************************************************************/
	// append ten eight blocks directly to the candidate blocks
	appendBlocksDirectly(nodes, 8)

	// broadcast two more block to trigger the votes for it
	broadcastBlock(nodes, 2, t)

	time.Sleep(time.Second)

	compareChainsIgnoreQC(nodes, t)
	logger.Info("all the nodes have the same chain", "blocks", nodes[0].chain.String())
	// close the connections
	clean(nodes)
}

// send the new request via cmd_handler
func TestCmdHandler(t *testing.T) {
	// only allow one node to call Node.ProposeBlockLoop() in this test, just pass []bool{true, false, false, false}
	nodes, err := setup(4, 3, 1, false, []bool{true, false, false, false})
	if err != nil {
		t.Fatal(err)
	}

	client, err := rpc.DialHTTP("tcp", nodes[0].addr+":"+strconv.Itoa(nodes[0].rpcListenPort))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2000; i++ {
		req := Request{
			Cmd: []byte("A new request" + strconv.Itoa(i)),
		}

		var reply Reply

		err = client.Call("RPCHandler.NewRequest", req, &reply)
		if err != nil {
			t.Fatal(err)
		}
		if i%100 == 0 {
			fmt.Printf("Reply from node0: %v\n", reply)
		}
	}

	time.Sleep(time.Second * 8)
	clean(nodes)
}
