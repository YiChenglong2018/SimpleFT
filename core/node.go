/*
Package core implements the core protocol of yimchain,
including the data structures, votes, verification, and so on.
*/
package core

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/yimchain/config"
	"github.com/seafooler/yimchain/conn"
	"github.com/seafooler/yimchain/sign"
	"github.com/seafooler/yimchain/sortition"
	"go.dedis.ch/kyber/v3/share"
	"log"
	"math"
	"net"
	"net/http"
	"net/rpc"
	"reflect"
	"strconv"
	"sync"
	"time"
)

var proposal Block
var vote VoteWithPartialSig
var sortitionMsg sortition.SortitionMsg
var reflectedTypesMap = map[uint8]reflect.Type{
	ProposalTag: reflect.TypeOf(proposal),
	VoteTag:     reflect.TypeOf(vote),
	SortitionTag: reflect.TypeOf(sortitionMsg),
}

// Node defines a node.
type Node struct {
	name                   string
	lock                   sync.RWMutex
	chain                  *Chain
	candidateChain         *CandidateChain
	candidateHeightUpdated chan uint64

	voted map[uint64]bool

	pendingBlocksQC map[uint64]*BlockWithQC                 // map from height to the blockWithQC
	pendingBlocks   map[uint64]map[string]*Block            // map from height to the block, there may be multiple blocks for one height
	partialSigs     map[uint64]map[string]map[string][]byte // map from height to string(hash) to the partial signatures
	qcCreated       map[uint64]bool                         // mark if we has created a QC for the height

	sortitionResult	map[uint64]map[uint64]map[string]bool 	// map from height to: map from [round] to: map from node to boolean
	sortitionResultLock sync.Mutex

	logger hclog.Logger

	addr        string
	clusterAddr map[string]string // map from name to address
	clusterPort map[string]int    // map from name to p2pPort
	nodeNum     int               // record nodeNum and quorumNum to avoid repeated calculation
	quorumNum   int

	p2pListenPort int
	rpcListenPort int

	requestPool     *RequestPool
	c               *RPCHandler
	canProposeBlock bool // indicate if this node will receive requests and propose blocks

	maxPool int
	trans   *conn.NetworkTransport

	sortitioner *sortition.Sortitioner
	sortitionEnabled  bool // indicate whether enable vrf and sortition.

	//Used for ED25519 signature
	publicKeyMap map[string]ed25519.PublicKey // after the initialization, only read, safe for concurrency safety
	privateKey   ed25519.PrivateKey

	//Used for threshold signature
	tsPublicKey  *share.PubPoly
	tsPrivateKey *share.PriShare

	reflectedTypesMap map[uint8]reflect.Type

	shutdownCh chan struct{}
}

// NewNode creates a new node from a config.Config variable.
func NewNode(conf *config.Config) *Node {
	var n Node
	n.name = conf.Name
	n.addr = conf.AddrStr
	n.clusterAddr = conf.ClusterAddr
	n.clusterPort = conf.ClusterPort
	n.nodeNum = len(n.clusterAddr)
	n.quorumNum = int(math.Ceil(float64(2*len(n.clusterAddr)) / 3.0))
	n.maxPool = conf.MaxPool

	n.p2pListenPort = conf.P2PListenPort
	n.rpcListenPort = conf.RPCListenPort

	n.requestPool = &RequestPool{}
	n.c = &RPCHandler{reqPool: n.requestPool, nodeName: n.name, nextSN: 0}
	n.canProposeBlock = conf.CanProposeBlock

	n.chain = &Chain{height: 0, blocks: make(map[uint64]*BlockWithQC)}
	n.chain.blocks[0] = &BlockWithQC{
		Block: Block{
			Proposer:     "CivCrafter",
			Height:       0,
			PreviousHash: []byte(""),
			Cmds:         []RequestWrappedByServer{},
		},
		QC: []byte(""),
	}
	n.candidateChain = &CandidateChain{height: 0, blocks: make(map[uint64]map[string]*Block)}
	block := n.chain.blocks[0].Block
	hash, _ := block.getHashAsString()
	n.candidateChain.blocks[0] = map[string]*Block{
		hash: &block,
	}
	n.candidateHeightUpdated = make(chan uint64, 1)
	n.candidateHeightUpdated <- 0
	n.voted = make(map[uint64]bool)

	n.pendingBlocksQC = make(map[uint64]*BlockWithQC)
	n.pendingBlocks = make(map[uint64]map[string]*Block)
	n.partialSigs = make(map[uint64]map[string]map[string][]byte)
	n.qcCreated = make(map[uint64]bool)

	n.sortitionResult = make(map[uint64]map[uint64]map[string]bool)

	n.logger = hclog.New(&hclog.LoggerOptions{
		Name:   "yimchain-node",
		Output: hclog.DefaultOutput,
		Level:  hclog.Level(conf.LogLevel),
	})

	sortitioner, err := sortition.NewSortitioner(conf.Probability, conf.PublicKeyMap[conf.Name], conf.PrivateKey)
	if err != nil {
		panic(err)
	}
	n.sortitioner = sortitioner

	n.privateKey = conf.PrivateKey
	n.publicKeyMap = conf.PublicKeyMap

	n.tsPrivateKey = conf.TsPrivateKey
	n.tsPublicKey = conf.TsPublicKey

	n.reflectedTypesMap = reflectedTypesMap
	n.shutdownCh = make(chan struct{})

	return &n
}

// EstablishP2PConns establishes P2P connections with other nodes.
func (n *Node) EstablishP2PConns() error {
	if n.trans == nil {
		return errors.New("networktransport has not been created")
	}
	for name, addr := range n.clusterAddr {
		addrWithPort := addr + ":" + strconv.Itoa(n.clusterPort[name])
		conn, err := n.trans.GetConn(addrWithPort)
		if err != nil {
			return err
		}
		n.trans.ReturnConn(conn)
		n.logger.Debug("connection has been established", "sender", n.name, "receiver", addr)
	}
	return nil
}

// CanProposeBlock checks if the node is qualified propose a new block
func (n *Node) CanProposeBlock() bool {
	return n.canProposeBlock
}

// StartP2PListen starts the node to listen for P2P connection.
func (n *Node) StartP2PListen() error {
	var err error
	n.trans, err = conn.NewTCPTransport(":"+strconv.Itoa(n.clusterPort[n.name]), 2*time.Second,
		nil, n.maxPool, n.reflectedTypesMap)
	if err != nil {
		return err
	}
	return nil
}

// StartRPCListen starts the node to listen for requests from clients.
func (n *Node) StartRPCListen() error {
	err := rpc.Register(n.c)
	if err != nil {
		return err
	}

	rpc.HandleHTTP()

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(n.rpcListenPort))
	if err != nil {
		log.Fatal("Request listen error: ", err)
	}

	n.logger.Info("Serving request listening", "addr", n.addr+":"+strconv.Itoa(n.rpcListenPort))

	go http.Serve(listener, nil)

	go func(l net.Listener) {
		<-n.shutdownCh
		l.Close()
	}(listener)

	return nil
}

// ProposeBlockLoop starts a loop to tentatively package block proposed by
// time to package: 1. timeout (5 seconds); 2. over 1000 pending requests.
func (n *Node) ProposeBlockLoop() {
	timeOut := make(chan struct{}, 1)
	for {
		var reqs []RequestWrappedByServer
		go func(ch chan struct{}) {
			<-time.After(time.Second * 5)
			ch <- struct{}{}
		}(timeOut)
		if len(n.requestPool.pool) < 1000 {
			<-timeOut
		}

		select {
		case <-n.shutdownCh:
			return
		case <-n.candidateHeightUpdated:
		}

		previousBlock, err := n.candidateChain.selectBlock()
		if err != nil {
			panic(err)
		}
		nextHeight := previousBlock.Height+1
		var nextRound uint64
		n.sortitionResultLock.Lock()
		if s, ok := n.sortitionResult[nextHeight]; !ok {
			nextRound = 0
			n.sortitionResult[nextHeight]= make(map[uint64]map[string]bool)
			n.sortitionResult[nextHeight][0] = make(map[string]bool)
		} else {
			// find the largest round with 2/3+ false results
			maxRound := uint64(0)
			for round, results := range s {
				if len(results) >= n.quorumNum {
					if round > maxRound {
						maxRound = round
					}
				}
			}
			nextRound = maxRound+1
		}
		n.sortitionResultLock.Unlock()
		seedForSortition := nextHeight<<20 + nextRound	// To do, 20 should be defined as an argument
		win, _ := n.sortitioner.Once(seedForSortition) // To do, proof handling ...
		if win {
			n.requestPool.Lock()
			endNum := 1000
			if len(n.requestPool.pool)< 1000 {
				endNum = len(n.requestPool.pool)
			}
			reqs = n.requestPool.pool[:endNum]
			n.requestPool.pool = n.requestPool.pool[endNum:]
			n.requestPool.Unlock()

			previousHash, err := previousBlock.getHash()
			if err != nil {
				panic(err)
			}
			block := NewBlock(n.name, nextHeight, previousHash, reqs)
			n.broadcast(ProposalTag, block, nil)
			n.logger.Info("Propose a new block", "Node", n.name, "blockHeight", block.Height,
				"round", nextRound, "reqNum", len(reqs))
			timeOut = make(chan struct{}, 1)
		} else {
			sortitionMsg := sortition.NewSortitionMsg(n.name, nextHeight, nextRound)
			n.broadcast(SortitionTag, sortitionMsg, nil)
			n.logger.Info("Sortition result false", "Node", n.name, "blockHeight", nextHeight,
				"round", nextRound)
			go func() {
				n.candidateHeightUpdated <- 0
			}()
		}
	}
}

// handleNewBlockQC handles a new block with QC.
// It simply adds the block with QC to the pendingBlocksQC, delete the block from the pendingBlocks,
// and call the function of tryExtendChain.
func (n *Node) handleNewBlockQC(bqc *BlockWithQC) {
	n.lock.Lock()
	if _, ok := n.pendingBlocksQC[bqc.Height]; ok {
		n.logger.Debug("a block with QC has been received before") // To be repaired ...: this check can be done at a former place
		return
	}
	n.pendingBlocksQC[bqc.Height] = bqc
	// delete(n.pendingBlocks, bqc.Height)
	n.lock.Unlock()
	n.CommitAncestorByBlockQC(bqc)
	go n.tryExtendChain()
}

// CommitAncestorByBlockQC commits all the ancestor blocks of a block with QC.
func (n *Node) CommitAncestorByBlockQC(bqc *BlockWithQC) {
	n.lock.Lock()
	defer n.lock.Unlock()
	// all the ancestor blocks which can be committed must make up a prefix in the candidate chain
	// thus, we only need to check if the father block of bqc is in the candidate chain
	if len(n.chain.blocks) == len(n.candidateChain.blocks) {
		// if the candidate chain has a same length with chain, just do nothing
		// the extension of chain will be conducted in function tryExtendChain()
		return
	}
	previousHash := bqc.PreviousHash
	previousHeight := bqc.Height - 1
	if blocks, ok := n.candidateChain.blocks[previousHeight]; ok {
		for hashAsString, block := range blocks {
			if hashAsString == hex.EncodeToString(previousHash) {
				n.logger.Debug("previous blocks are successive", "Node", n.name, "blockWithQC", bqc.String(),
					"previousHeight", previousHeight, "candidateChainHeight", n.candidateChain.height,
					"chainHeight", n.chain.height)
				// commit the blocks from the candidate chain to the chain
				hash := hashAsString
				for i := previousHeight; i > n.chain.height; i-- {
					n.logger.Info("a block is committed from candidate chain to the chain", "node",
						n.name, "height", i, "hash", hash)
					n.chain.blocks[i] = &BlockWithQC{
						Block: *block,
						QC:    bqc.QC,
					}
					hash = hex.EncodeToString(block.PreviousHash)
					block, ok = n.candidateChain.blocks[i-1][hash]
					if !ok {
						n.logger.Error("no block in the candidateChain", "node", n.name, "height", i-1, "hash", hash)
					}
				}
				n.chain.height = previousHeight
			}
		}
	}
}

// tryExtendChain tries to extend the chain with blocks from pendingBlocksQC.
func (n *Node) tryExtendChain() {
	n.lock.Lock()
	defer n.lock.Unlock()
	newHeight := n.chain.height + 1
	if blockWithQC, ok := n.pendingBlocksQC[newHeight]; ok {
		n.chain.blocks[newHeight] = blockWithQC
		delete(n.pendingBlocksQC, newHeight)
		n.chain.height = newHeight
		n.logger.Info("commit the block", "node", n.name, "height", newHeight, "block-proposer", blockWithQC.Proposer)
		go n.tryExtendChain()
	}
}

// broadcast broadcasts the msg to each node, excluding the addrs in excAddrs.
func (n *Node) broadcast(msgType uint8, msg interface{}, excAddrs map[string]bool) error {
	msgAsBytes, err := encode(msg)
	if err != nil {
		return err
	}
	sig := sign.SignEd25519(n.privateKey, msgAsBytes)

	var netConn *conn.NetConn
	for name, addr := range n.clusterAddr {
		addrWithPort := addr + ":" + strconv.Itoa(n.clusterPort[name])
		if excAddrs != nil {
			if _, ok := excAddrs[addrWithPort]; ok {
				continue
			}
		}
		if netConn, err = n.trans.GetConn(addrWithPort); err != nil {
			return err
		}
		if err = conn.SendMsg(netConn, msgType, msg, sig); err != nil {
			return err
		}

		if err = n.trans.ReturnConn(netConn); err != nil {
			return err
		}
	}
	return nil
}

// signPlainVote signs the plain vote.
func (n *Node) signPlainVote(v *PlainVote) (*VoteWithPartialSig, error) {
	voteAsBytes, err := encode(v)
	if err != nil {
		return nil, err
	}
	partialSig := sign.SignTSPartial(n.tsPrivateKey, voteAsBytes)
	voteWithPartialSig := &VoteWithPartialSig{
		PlainVote:  *v,
		Sender:     n.name,
		PartialSig: partialSig,
	}
	return voteWithPartialSig, nil
}

// BroadcastVote broadcasts the vote for a block.
func (n *Node) BroadcastVote(block *Block) error {
	n.lock.RLock()
	if n.voted[block.Height] {
		// if has voted for another block with the same height, just return
		n.lock.RUnlock()
		return nil
	}
	n.lock.RUnlock()

	// construct vote
	hash, _ := block.getHash()
	plainVote := &PlainVote{
		BlockHash:   hash,
		BlockHeight: block.Height,
		Opinion:     true,
	}

	voteWithPartialSig, err := n.signPlainVote(plainVote)
	if err != nil {
		return err
	}

	n.lock.Lock()
	defer n.lock.Unlock()
	if !n.voted[block.Height] {
		n.logger.Debug("vote the block", "voter", n.name, "height", block.Height, "block-proposer", block.Proposer)
		n.voted[block.Height] = true
		return n.broadcast(VoteTag, voteWithPartialSig, nil)
	}
	return nil
}

func (n *Node) verifySigED25519(peer string, data interface{}, sig []byte) bool {
	pubKey, ok := n.publicKeyMap[peer]
	if !ok {
		n.logger.Error("peer is unknown", "peer", peer)
		return false
	}
	dataAsBytes, err := encode(data)
	if err != nil {
		n.logger.Error("fail to encode the data", "error", err)
		return false
	}
	ok, err = sign.VerifySignEd25519(pubKey, dataAsBytes, sig)
	if err != nil {
		n.logger.Error("fail to verify the ED25519 signature", "error", err)
		return false
	}
	return ok
}

// HandleMsgsLoop starts a loop to deal with the msgs from other peers.
func (n *Node) HandleMsgsLoop() {
	msgCh := n.trans.MsgChan()
	for {
		select {
		case msgWithSig := <-msgCh:
			switch msgAsserted := msgWithSig.Msg.(type) {
			case Block:
				if !n.verifySigED25519(msgAsserted.Proposer, msgWithSig.Msg, msgWithSig.Sig) {
					n.logger.Error("fail to verify the block's signature", "height", msgAsserted.Height,
						"proposer", msgAsserted.Proposer)
					continue
				}
				n.logger.Debug("signature of the proposed block is right", "height", msgAsserted.Height,
					"proposer", msgAsserted.Proposer)
				go n.handleNewBlockMsg(&msgAsserted)
			case VoteWithPartialSig:
				if !n.verifySigED25519(msgAsserted.Sender, msgWithSig.Msg, msgWithSig.Sig) {
					n.logger.Error("fail to verify the vote's signature", "height", msgAsserted.BlockHeight,
						"sender", msgAsserted.Sender)
					continue
				}
				n.logger.Debug("signature of the vote is right", "height", msgAsserted.BlockHeight,
					"sender", msgAsserted.Sender)
				go n.handleVoteMsg(&msgAsserted)
			case sortition.SortitionMsg:
				if !n.verifySigED25519(msgAsserted.Sortitioner, msgWithSig.Msg, msgWithSig.Sig) {
					n.logger.Error("fail to verify the sortitionmsg's signature", "height", msgAsserted.Height,
						"round", msgAsserted.Round, "sortitioner", msgAsserted.Sortitioner)
					continue
				}
				n.logger.Debug("signature of the sortitionmsg is right", "height", msgAsserted.Height,
					"round", msgAsserted.Round, "sortitioner", msgAsserted.Sortitioner)
				go n.handleSortitionMsg(&msgAsserted)
			}
		}
	}
}

// validateBlockMsg validates if the block msg is legal.
// To be repaired ...
// Just return true here.
func (n *Node) validateBlockMsg(block *Block) bool {
	return true
}

func (n *Node) handleNewBlockMsg(block *Block) {
	if ok := n.validateBlockMsg(block); !ok {
		// if the new vote message is illegal, ignore it
		return
	}
	go n.handleNewBlock(block)
	go n.BroadcastVote(block)
}

func (n *Node) handleNewBlock(block *Block) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if _, ok := n.pendingBlocks[block.Height]; !ok {
		n.pendingBlocks[block.Height] = make(map[string]*Block)
	}
	hash, _ := block.getHashAsString()
	n.pendingBlocks[block.Height][hash] = block
	go n.tryUpdateCandidateChain(block)
	go n.checkIfQuorumVotes(block.Height, hash)
}

// tryUpdateCandidateChain tries to update the candidate chain with blocks from pendingBlocks.
// If there is already a block with the same height, append the block;
// If there is no, extend the candidate chain.
func (n *Node) tryUpdateCandidateChain(block *Block) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if _, ok := n.candidateChain.blocks[block.Height]; ok {
		// add the new block to existing height
		n.logger.Debug("add a new block to existing height", "height", block.Height, "block-proposer", block.Proposer)
		blockHashAsString, _ := block.getHashAsString()
		n.candidateChain.blocks[block.Height][blockHashAsString] = block
	} else {
		go n.tryExtendCandidateChain()
	}
}
func (n *Node) tryExtendCandidateChain() {
	n.lock.Lock()
	defer n.lock.Unlock()
	newHeight := n.candidateChain.height + 1
	if blocks, ok := n.pendingBlocks[newHeight]; ok {
		n.candidateChain.blocks[newHeight] = blocks
		n.candidateChain.height = newHeight
		go func() {
			n.candidateHeightUpdated <- newHeight
		}()
		n.logger.Debug("extend the height with blocks from pending blocks", "height", newHeight)
		go n.tryExtendCandidateChain()
	}
}

// validateVoteMsg Validates if the vote msg is legal.
// To be repaired ...
// Just return true here.
func (n *Node) validateVoteMsg(vote *VoteWithPartialSig) bool {
	return true
}

func (n *Node) handleVoteMsg(vote *VoteWithPartialSig) {
	if ok := n.validateVoteMsg(vote); !ok {
		// if the new vote message is illegal, ignore it
		return
	}
	n.lock.Lock()
	defer n.lock.Unlock()
	parSigs, ok := n.partialSigs[vote.BlockHeight]
	if !ok {
		n.partialSigs[vote.BlockHeight] = make(map[string]map[string][]byte)
		parSigs, _ = n.partialSigs[vote.BlockHeight]
	}
	hashString := hex.EncodeToString(vote.BlockHash)
	_, ok = parSigs[hashString]
	if !ok {
		n.partialSigs[vote.BlockHeight][hashString] = make(map[string][]byte)
	}

	n.partialSigs[vote.BlockHeight][hashString][vote.Sender] = vote.PartialSig
	go n.checkIfQuorumVotes(vote.BlockHeight, hashString)
}

func (n *Node) buildBlockWithQC(height uint64, hashString string) {
	block := n.pendingBlocks[height][hashString]
	partialSigs := n.extractPartialSigs(height, hashString)
	hash, _ := block.getHash()
	plainVote := &PlainVote{
		BlockHash:   hash,
		BlockHeight: block.Height,
		Opinion:     true,
	}

	intactSig, err := createIntactSig(plainVote, partialSigs, n.tsPublicKey, n.quorumNum, n.nodeNum)
	if err != nil {
		return
	}
	blockWithQC := &BlockWithQC{
		Block: *block,
		QC:    intactSig,
	}
	n.logger.Debug("create a QC", "node", n.name, "plainvote", plainVote, "qc", intactSig)
	go n.handleNewBlockQC(blockWithQC)
}

// extractPartialSigs extracts inner elements of size: quorum.
// @return: [][]byte
func (n *Node) extractPartialSigs(height uint64, hashString string) [][]byte {
	n.lock.RLock()
	defer n.lock.RUnlock()
	var partialSigs [][]byte
	num := n.quorumNum
	for _, parSig := range n.partialSigs[height][hashString] {
		if num == 0 {
			break // only need quorumNum partial signatures
		}
		partialSigs = append(partialSigs, parSig)
		num--
	}
	return partialSigs
}

// checkIfQuorumVotes checks if there are votes of quorum for a block.
func (n *Node) checkIfQuorumVotes(height uint64, hashString string) {
	n.lock.Lock()
	parSigs, _ := n.partialSigs[height][hashString]
	_, ok := n.pendingBlocks[height][hashString]
	defer n.lock.Unlock()
	if len(parSigs) >= n.quorumNum && !n.qcCreated[height] && ok {
		n.qcCreated[height] = true
		go n.buildBlockWithQC(height, hashString)
	}
}

// handleSortitionMsg deals with the sortition message of 'false' result
func (n *Node) handleSortitionMsg(smsg *sortition.SortitionMsg) {
	n.sortitionResultLock.Lock()
	defer n.sortitionResultLock.Unlock()
	_, ok := n.sortitionResult[smsg.Height]
	if !ok {
		n.sortitionResult[smsg.Height] = make(map[uint64]map[string]bool)
	}
	_, ok = n.sortitionResult[smsg.Height][smsg.Round]
	if !ok {
		n.sortitionResult[smsg.Height][smsg.Round] = make(map[string]bool)
	}
	n.sortitionResult[smsg.Height][smsg.Round][smsg.Sortitioner]=false
}
