package main

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

