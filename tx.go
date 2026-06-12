package tx

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
)

type TxInput struct {
	Txid      string
	OutIndex  int
	Signature []byte
	PubKey    []byte
}

type TxOutput struct {
	Amount  int
	Address string
}

type Transaction struct {
	ID   string
	Vin  []TxInput
	Vout []TxOutput
}

func (tx *Transaction) Hash() {
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	_ = enc.Encode(tx)
	sum := sha256.Sum256(buff.Bytes())
	tx.ID = fmt.Sprintf("%x", sum[:])
}

func NewCoinbase(to string, amount int) *Transaction {
	tx := &Transaction{
		Vin:  []TxInput{{Txid: "", OutIndex: -1, Signature: nil, PubKey: nil}},
		Vout: []TxOutput{{Amount: amount, Address: to}},
	}
	tx.Hash()
	return tx
}

func NewUTXOTransaction(from, to string, amount int) (*Transaction, error) {
	// very simple UTXO selection
	utxos, balance := FindSpendableUTXOs(from)
	if balance < amount {
		return nil, errors.New("insufficient funds")
	}
	var inputs []TxInput
	var outputs []TxOutput

	accum := 0
	for txid, outs := range utxos {
		for _, outIdx := range outs {
			inputs = append(inputs, TxInput{Txid: txid, OutIndex: outIdx, Signature: nil, PubKey: nil})
			accum += GetOutputAmount(txid, outIdx)
			if accum >= amount {
				break
			}
		}
		if accum >= amount {
			break
		}
	}
	outputs = append(outputs, TxOutput{Amount: amount, Address: to})
	if accum > amount {
		outputs = append(outputs, TxOutput{Amount: accum - amount, Address: from})
	}
	tx := &Transaction{Vin: inputs, Vout: outputs}
	tx.Hash()
	return tx, nil
}

func SignTx(tx *Transaction, priv *ecdsa.PrivateKey) {
	// naive: sign tx.ID bytes
	r, s, _ := ecdsa.Sign(randReader(), priv, []byte(tx.ID))
	sig := append(r.Bytes(), s.Bytes()...)
	for i := range tx.Vin {
		tx.Vin[i].Signature = sig
		pub := append(priv.PublicKey.X.Bytes(), priv.PublicKey.Y.Bytes()...)
		tx.Vin[i].PubKey = pub
	}
}

func VerifyTx(tx *Transaction) bool {
	// very naive verification: check signatures exist
	for _, in := range tx.Vin {
		if len(in.Signature) == 0 || len(in.PubKey) == 0 {
			return false
		}
	}
	return true
}

//-------------------------------------------------------------------------
//----------------------------------------------------------------------------------

func FindSpendableUTXOs(address string) (map[string][]int, int) {
	utxos := map[string][]int{}
	balance := 0
	for _, b := range chain.Blocks {
		for _, tx := range b.Transactions {
			for i, out := range tx.Vout {
				if out.Address == address {
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
		return nil, errors.New("проверка транзакции не прошла")
	}
	return tx, nil
}

func mempoolAdd(tx *Transaction) error {
	if !VerifyTx(tx) {
		return errors.New("некорректная tx")
	}
	mempool = append(mempool, tx)
	return nil
}

