package main

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
SimpleCoin single-file:
- Account model: map[address]balance
- ECDSA wallets (P-256) addresses = sha256(pub)[:20] hex
- Transactions: From, To, Amount, Sig, PubKey
- Blocks: list of transactions, prev hash, nonce, pow with prefix "000"
- Simple TCP P2P: peers exchange entire chain or broadcast txs
- Persistence: gob files chain.db wallets.db
- CLI commands: newwallet, address, balance <addr>, send <from> <to> <amt>, mine, chain, peers, connect <host:port>, exit
*/

const (
	dbChainFile   = "chain.db"
	dbWalletsFile = "wallets.db"
	difficulty    = "000" // PoW prefix
)

// --- Data structures

type Transaction struct {
	From    string
	To      string
	Amount  int
	Sig     []byte // r||s
	PubKey  []byte // X||Y
	Txid    string
}

type Block struct {
	Timestamp    int64
	Transactions []*Transaction
	PrevHash     string
	Nonce        int
	Hash         string
}

type Blockchain struct {
	Blocks []*Block
}

type Wallet struct {
	Priv *ecdsa.PrivateKey
	Addr string
}

// --- Global state

var (
	chain    *Blockchain
	wallets  map[string]*Wallet
	balances map[string]int
	mempool  []*Transaction

	peersMu sync.Mutex
	peers   = map[string]net.Conn{}

	stateMu sync.Mutex
)

// --- Utils

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func powHashBlock(b *Block) string {
	var buf bytes.Buffer
	for _, tx := range b.Transactions {
		buf.WriteString(tx.Txid)
	}
	buf.WriteString(b.PrevHash)
	buf.WriteString(strconv.FormatInt(b.Timestamp, 10))
	buf.WriteString(strconv.Itoa(b.Nonce))
	return hashBytes(buf.Bytes())
}

func makeAddressFromPub(pub []byte) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:20])
}

func serialize(v interface{}) []byte {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	_ = enc.Encode(v)
	return buf.Bytes()
}

// --- Wallets

func NewWallet() string {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub := append(priv.PublicKey.X.Bytes(), priv.PublicKey.Y.Bytes()...)
	addr := makeAddressFromPub(pub)
	wallets[addr] = &Wallet{Priv: priv, Addr: addr}
	SaveWallets()
	return addr
}

func listAddresses() []string {
	addrs := make([]string, 0, len(wallets))
	for a := range wallets {
		addrs = append(addrs, a)
	}
	return addrs
}

func findWallet(addr string) (*Wallet, error) {
	if w, ok := wallets[addr]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("wallet not found")
}

// --- Transactions (account model)

func txHash(tx *Transaction) string {
	b := serialize(struct {
		From   string
		To     string
		Amount int
	}{
		From:   tx.From,
		To:     tx.To,
		Amount: tx.Amount,
	})
	return hashBytes(b)
}

func SignTransaction(tx *Transaction, priv *ecdsa.PrivateKey) {
	tx.Txid = txHash(tx)
	r, s, _ := ecdsa.Sign(rand.Reader, priv, []byte(tx.Txid))
	rb := r.Bytes()
	sb := s.Bytes()
	sig := append(append(make([]byte, 32-len(rb)), rb...), append(make([]byte, 32-len(sb)), sb...)...)
	tx.Sig = sig
	pub := append(priv.PublicKey.X.Bytes(), priv.PublicKey.Y.Bytes()...)
	tx.PubKey = pub
}

func VerifyTransaction(tx *Transaction) bool {
	if tx.From == "coinbase" {
		return true
	}
	if tx.Sig == nil || tx.PubKey == nil {
		return false
	}
	if tx.Txid == "" {
		tx.Txid = txHash(tx)
	}
	// recover r,s and pub
	r := new(big.Int).SetBytes(tx.Sig[:32])
	s := new(big.Int).SetBytes(tx.Sig[32:])
	x := new(big.Int).SetBytes(tx.PubKey[:len(tx.PubKey)/2])
	y := new(big.Int).SetBytes(tx.PubKey[len(tx.PubKey)/2:])
	pub := ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	return ecdsa.Verify(&pub, []byte(tx.Txid), r, s)
}

// --- Chain & blocks

func NewGenesis() *Blockchain {
	cb := &Transaction{From: "coinbase", To: "genesis", Amount: 1000000}
	cb.Txid = txHash(cb)
	gen := &Block{
		Timestamp:    time.Now().Unix(),
		Transactions: []*Transaction{cb},
		PrevHash:     "",
		Nonce:        0,
	}
	gen.Hash = mineBlockHash(gen)
	b := &Blockchain{Blocks: []*Block{gen}}
	applyBlockState(gen)
	return b
}

func mineBlockHash(b *Block) string {
	for {
		h := powHashBlock(b)
		if strings.HasPrefix(h, difficulty) {
			return h
		}
		b.Nonce++
	}
}

func MineBlock(minerAddr string) *Block {
	stateMu.Lock()
	defer stateMu.Unlock()
	// include mempool
	txs := make([]*Transaction, 0, len(mempool)+1)
	cb := &Transaction{From: "coinbase", To: minerAddr, Amount: 50}
	cb.Txid = txHash(cb)
	txs = append(txs, cb)
	for _, t := range mempool {
		if VerifyTransaction(t) && balances[t.From] >= t.Amount {
			t.Txid = txHash(t)
			txs = append(txs, t)
		}
	}
	prev := chain.Blocks[len(chain.Blocks)-1]
	b := &Block{
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		PrevHash:     prev.Hash,
		Nonce:        0,
	}
	b.Hash = mineBlockHash(b)
	chain.Blocks = append(chain.Blocks, b)
	applyBlockState(b)
	mempool = nil
	SaveChain()
	broadcastMessage("CHAIN", serialize(chain))
	return b
}

func applyBlockState(b *Block) {
	for _, tx := range b.Transactions {
		if tx.From != "coinbase" {
			// subtract (assume verified earlier)
			balances[tx.From] -= tx.Amount
		}
		balances[tx.To] += tx.Amount
	}
}

// --- Mempool

func AddTxToMempool(tx *Transaction) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if !VerifyTransaction(tx) {
		return fmt.Errorf("invalid tx")
	}
	if tx.From != "coinbase" {
		if balances[tx.From] < tx.Amount {
			return fmt.Errorf("insufficient funds")
		}
	}
	mempool = append(mempool, tx)
	broadcastMessage("TX", serialize(tx))
	return nil
}

// --- Persistence

func SaveChain() {
	f, err := os.Create(dbChainFile)
	if err != nil {
		log.Println("save chain:", err)
		return
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	_ = enc.Encode(chain)
}

func LoadChain() *Blockchain {
	f, err := os.Open(dbChainFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	var c Blockchain
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&c); err == nil {
		return &c
	}
	return nil
}

func SaveWallets() {
	f, err := os.Create(dbWalletsFile)
	if err != nil {
		log.Println("save wallets:", err)
		return
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	// cannot encode private keys directly; store d and pub coords
	store := map[string]map[string][]byte{}
	for a, w := range wallets {
		store[a] = map[string][]byte{
			"D":  w.Priv.D.Bytes(),
			"X":  w.Priv.PublicKey.X.Bytes(),
			"Y":  w.Priv.PublicKey.Y.Bytes(),
		}
	}
	_ = enc.Encode(store)
}

func LoadWallets() map[string]*Wallet {
	f, err := os.Open(dbWalletsFile)
	if err != nil {
		return map[string]*Wallet{}
	}
	defer f.Close()
	var store map[string]map[string][]byte
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&store); err != nil {
		return map[string]*Wallet{}
	}
	out := map[string]*Wallet{}
	for a, m := range store {
		d := new(big.Int).SetBytes(m["D"])
		x := new(big.Int).SetBytes(m["X"])
		y := new(big.Int).SetBytes(m["Y"])
		priv := &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
			D:         d,
		}
		out[a] = &Wallet{Priv: priv, Addr: a}
	}
	return out
}

// --- P2P networking (very simple): messages: "CHAIN" <gob chain>, "TX" <gob tx>

func startListener(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen %s: %v", port, err)
	}
	log.Printf("Listening on :%s\n", port)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Println("accept:", err)
				continue
			}
			registerPeer(conn)
			go handleConn(conn)
		}
	}()
}

func registerPeer(c net.Conn) {
	peersMu.Lock()
	defer peersMu.Unlock()
	peers[c.RemoteAddr().String()] = c
}

func unregisterPeer(addr string) {
	peersMu.Lock()
	defer peersMu.Unlock()
	if c, ok := peers[addr]; ok {
		_ = c.Close()
		delete(peers, addr)
	}
}

func handleConn(c net.Conn) {
	defer func() {
		unregisterPeer(c.RemoteAddr().String())
	}()
	r := bufio.NewReader(c)
	for {
		hdr, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Println("peer read hdr:", err)
			}
			return
		}
		hdr = strings.TrimSpace(hdr)
		// next read gob payload length line
		lenLine, _ := r.ReadString('\n')
		l, _ := strconv.Atoi(strings.TrimSpace(lenLine))
		buf := make([]byte, l)
		_, err = io.ReadFull(r, buf)
		if err != nil {
			log.Println("peer read payload:", err)
			return
		}
		switch hdr {
		case "CHAIN":
			var c Blockchain
			if err := gob.NewDecoder(bytes.NewReader(buf)).Decode(&c); err == nil {
				handleIncomingChain(&c)
			}
		case "TX":
			var t Transaction
			if err := gob.NewDecoder(bytes.NewReader(buf)).Decode(&t); err == nil {
				handleIncomingTx(&t)
			}
		}
	}
}

func connectPeer(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Println("connect error:", err)
		return
	}
	registerPeer(conn)
	// send our chain
	sendMessageToConn(conn, "CHAIN", serialize(chain))
	// start handling incoming
	go handleConn(conn)
}

func sendMessageToConn(c net.Conn, hdr string, payload []byte) {
	fmt.Fprintf(c, "%s\n%d\n", hdr, len(payload))
	_, _ = c.Write(payload)
}

func broadcastMessage(hdr string, payload []byte) {
	peersMu.Lock()
	defer peersMu.Unlock()
	for addr, c := range peers {
		if c == nil {
			delete(peers, addr)
			continue
		}
		go sendMessageToConn(c, hdr, payload)
	}
}

func handleIncomingChain(in *Blockchain) {
	stateMu.Lock()
	defer stateMu.Unlock()
	// accept longer chain
	if len(in.Blocks) > len(chain.Blocks) {
		chain = in
		// rebuild balances from chain
		balances = map[string]int{}
		for _, b := range chain.Blocks {
			applyBlockState(b)
		}
		SaveChain()
		log.Println("Replaced chain with incoming longer chain")
	}
}

func handleIncomingTx(t *Transaction) {
	stateMu.Lock()
	defer stateMu.Unlock()
	// basic check and add to mempool
	if VerifyTransaction(t) {
		// check funds available currently
		if t.From == "coinbase" || balances[t.From] >= t.Amount {
			mempool = append(mempool, t)
			log.Println("Added incoming tx to mempool:", t.Txid)
		}
	}
}

// --- Init and CLI

func initState(port string) {
	// load wallets
	wallets = LoadWallets()
	balances = map[string]int{}
	// load chain
	chain = LoadChain()
	if chain == nil {
		chain = NewGenesis()
		SaveChain()
	}
	// build balances from chain
	for _, b := range chain.Blocks {
		for _, tx := range b.Transactions {
			if tx.From != "coinbase" {
				balances[tx.From] -= tx.Amount
			}
			balances[tx.To] += tx.Amount
		}
	}
	// start P2P listener
	startListener(port)
}

func main() {
	port := flag.String("port", "3000", "listening port")
	peer := flag.String("peer", "", "peer to connect (host:port)")
	flag.Parse()

	initState(*port)
	if *peer != "" {
		connectPeer(*peer)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("SimpleCoin node (single-file). Commands: newwallet | address | balance <addr> | send <from> <to> <amt> | mine | chain | peers | connect <host:port> | exit")
	for {
		fmt.Print("> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		switch parts[0] {
		case "newwallet":
			a := NewWallet()
			fmt.Println("New address:", a)
		case "address":
			for _, a := range listAddresses() {
				fmt.Println(a)
			}
		case "balance":
			if len(parts) < 2 {
				fmt.Println("usage: balance <addr>")
				continue
			}
			stateMu.Lock()
			fmt.Println("Balance:", balances[parts[1]])
			stateMu.Unlock()
		case "send":
			if len(parts) < 4 {
				fmt.Println("usage: send <from> <to> <amount>")
				continue
			}
			amt, _ := strconv.Atoi(parts[3])
			w, err := findWallet(parts[1])
			if err != nil {
				fmt.Println("wallet error:", err)
				continue
			}
			tx := &Transaction{From: parts[1], To: parts[2], Amount: amt}
			SignTransaction(tx, w.Priv)
			if err := AddTxToMempool(tx); err != nil {
				fmt.Println("mempool add error:", err)
			} else {
				fmt.Println("tx added:", tx.Txid)
			}
		case "mine":
			// choose first wallet as miner if any, else "miner"
			miner := "miner"
			for a := range wallets {
				miner = a
				break
			}
			b := MineBlock(miner)
			SaveChain()
			fmt.Println("Mined block:", b.Hash, "txs:", len(b.Transactions))
		case "chain":
			for i, b := range chain.Blocks {
				fmt.Printf("=== Block %d ===\nHash:%s\nPrev:%s\nTime:%d\nNonce:%d\nTxs:%d\n\n", i, b.Hash, b.PrevHash, b.Timestamp, b.Nonce, len(b.Transactions))
			}
		case "peers":
			peersMu.Lock()
			for p := range peers {
				fmt.Println(p)
			}
			peersMu.Unlock()
		case "connect":
			if len(parts) < 2 {
				fmt.Println("usage: connect host:port")
				continue
			}
			connectPeer(parts[1])
		case "exit":
			SaveChain()
			SaveWallets()
			return
		default:
			fmt.Println("unknown command")
		}
	}
} 
