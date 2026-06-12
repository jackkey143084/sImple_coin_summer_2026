package main

import (
	"encoding/gob"
	"os"
	"errors"
)

func SaveChain(c *Blockchain) {
	f, _ := os.Create("chain.db")
	defer f.Close()
	enc := gob.NewEncoder(f)
	_ = enc.Encode(c)
}

func LoadChain() *Blockchain {
	f, err := os.Open("chain.db")
	if err != nil {
		return nil
	}
	defer f.Close()
	var c Blockchain
	dec := gob.NewDecoder(f)
	if err := dec.Decode(&c); err != nil {
		return nil
	}
	return &c
}

func SaveWallets() {
	f, _ := os.Create("wallets.db")
	defer f.Close()
	enc := gob.NewEncoder(f)
	_ = enc.Encode(wallets)
}

func LoadWallets() {
	f, err := os.Open("wallets.db")
	if err != nil {
		return
	}
	defer f.Close()
	dec := gob.NewDecoder(f)
	_ = dec.Decode(&wallets)
}

func SaveAll() {
	SaveChain(chain)
	SaveWallets()
}

//UTXO helpers & mempool (add to tx.go or storage)

func FindSpendableUTXOs(address string) (map[string][]int, int) {
	utxos := map[string][]int{}
	balance := 0
	for _, b := range chain.Blocks {
		for _, tx := range b.Transactions {
			// outputs
			for i, out := range tx.Vout {
				if out.Address == address {
					// check not spent by later txs
					if !isSpent(tx.ID, i) {
						utxos[tx.ID] = append(utxos[tx.ID], i)
						balance += out.Amount
					}
				}
			}
		}
	}
	return utxos, balance
}

func isSpent(txid string, index int) bool {
	for _, b := range chain.Blocks {
		for _, tx := range b.Transactions {
			for _, in := range tx.Vin {
				if in.Txid == txid && in.OutIndex == index {
					return true
				}
			}
		}
	}
	return false
}

func GetOutputAmount(txid string, index int) int {
	for _, b := range chain.Blocks {
		for _, tx := range b.Transactions {
			if tx.ID == txid {
				return tx.Vout[index].Amount
			}
		}
	}
	return 0
}

func GetBalance(addr string) int {
	_, bal := FindSpendableUTXOs(addr)
	return bal
}

func CreateAndSignTx(from, to string, amount int) (*Transaction, error) {
	tx, err := NewUTXOTransaction(from, to, amount)
	if err != nil {
		return nil, err
	}
	w, err := findWallet(from)
	if err != nil {
		return nil, err
	}
	SignTx(tx, w.Priv)
	if !VerifyTx(tx) {
		return nil, errors.New("tx verification failed")
	}
	return tx, nil
}

func mempoolAdd(tx *Transaction) error {
	// simple check
	if !VerifyTx(tx) {
		return errors.New("invalid tx")
	}
	mempool = append(mempool, tx)
	return nil
}



