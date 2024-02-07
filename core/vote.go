package core

// PlainVote defines the content of a plain vote.
type PlainVote struct {
	BlockHash   []byte
	BlockHeight uint64
	Opinion     bool // agree or disagree
}

// VoteWithPartialSig encapsulates the PlainVote with the partial signature.
type VoteWithPartialSig struct {
	PlainVote
	Sender     string
	PartialSig []byte
}
