package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

type Wallet struct {
	Priv *ecdsa.PrivateKey
	Addr string
}

var wallets = map[string]*Wallet{}

func CreateWallet() string {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub := append(priv.PublicKey.X.Bytes(), priv.PublicKey.Y.Bytes()...)
	addr := addrFromPub(pub)
	w := &Wallet{Priv: priv, Addr: addr}
	wallets[addr] = w
	return addr
}

func ListWallets() {
	for a := range wallets {
		fmt.Println(a)
	}
}

func addrFromPub(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:20]) // 20 bytes
}

func findWallet(addr string) (*Wallet, error) {
	if w, ok := wallets[addr]; ok {
		return w, nil
	}
	return nil, errors.New("wallet not found")
}

