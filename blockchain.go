package blockchain


import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Block struct {
	Timestamp    int64
	Transactions []*Transaction
	PrevHash     string
	Hash         string
	Nonce        int
}

type Blockchain struct {
	Blocks []*Block
}

var chain *Blockchain
var mempool []*Transaction

func initOnce() {
	chain = LoadChain()
	if chain == nil {
		cb := NewCoinbase("genesis", 1000)
		gen := &Block{Timestamp: time.Now().Unix(), Transactions: []*Transaction{cb}, PrevHash: "", Nonce: 0}
		gen.Hash = MineProof(gen)
		chain = &Blockchain{Blocks: []*Block{gen}}
	}
	LoadWallets() // optional persistence
}

func MineProof(b *Block) string {
	// trivial PoW: find hash with prefix "000"
	for {
		data := blockData(b)
		h := Hash([]byte(data))
		if strings.HasPrefix(h, "000") {
			return h
		}
		b.Nonce++
	}
}

func blockData(b *Block) string {
	var txids []string
	for _, t := range b.Transactions {
		txids = append(txids, t.ID)
	}
	return fmt.Sprintf("%d%s%s%d", b.Timestamp, strings.Join(txids, ""), b.PrevHash, b.Nonce)
}

func MineBlock() *Block {
	// include mempool txs
	txs := append([]*Transaction{}, mempool...)
	cb := NewCoinbase("miner", 50)
	txs = append([]*Transaction{cb}, txs...)
	prev := chain.Blocks[len(chain.Blocks)-1]
	b := &Block{Timestamp: time.Now().Unix(), Transactions: txs, PrevHash: prev.Hash}
	b.Hash = MineProof(b)
	chain.Blocks = append(chain.Blocks, b)
	ApplyBlock(b)
	mempool = nil
	SaveChain(chain)
	return b
}

func PrintChain() {
	for i, b := range chain.Blocks {
		fmt.Printf("=== Block %d ===\nHash: %s\nPrev: %s\nTime: %d\nTxs: %d\n\n", i, b.Hash, b.PrevHash, b.Timestamp, len(b.Transactions))
	}
}

