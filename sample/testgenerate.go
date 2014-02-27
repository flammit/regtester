package main

import (
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/flammit/btcdcommander"
	"github.com/flammit/regtester"
	"time"
)

var (
	testNetMinerPublicKeyAddr = "n1hoJmy2JQcHQxMatmfy7QDW5nHTuH3peN"
	testNetMinerPrivateKey    = "cV7u7JEeoSTzWiiEvTcBERHnTmftTGPWMKYRwDgv714gWHZ81BTh"

	testNetSendPublicKeyAddr = "mrcQmNDsFsZfhXtbwVcB2enm8pgqfNtMts"
	testNetSendPrivateKey    = "cQrsP8dzR9BoqjF51rfooWyBEiAtoZ4A1BQ1pTuCXmipwSDJsJ7W"

	defaultLogFile = "test.log"
)

func main() {
	setLogLevels("info")
	defer backendLog.Flush()

	net := btcwire.TestNet

	subsidyAddress, err := btcutil.DecodeAddr(testNetMinerPublicKeyAddr)
	if err != nil {
		log.Errorf("Failed to parse subsidy address: error=%v", err)
		return
	}

	cfg := &btcdcommander.Config{
		CAFileName: "/Users/flam/Library/Application Support/Btcd/rpc.cert",
		Connect:    "127.0.0.1:18334",
		Username:   "rt",
		Password:   "rt",
	}
	cfg.SetNet(net)

	// addedBtcd := make(chan struct{}, 1)

	btcd := btcdcommander.NewCommander(cfg)
	go func() {
		ntfnChan := btcd.NtfnChan()
		for {
			cmd, ok := <-ntfnChan
			if !ok {
				return
			}
			log.Infof("Received notification: %#v", cmd)
		}
	}()
	btcd.Start()

	chain, db, err := regtester.SyncChain(btcd)
	if err != nil {
		log.Errorf("Failed to Sync Chain to BTCD: error=%v", err)
		return
	}

	latestBlockLocator, err := chain.LatestBlockLocator()
	if err != nil {
		log.Errorf("Failed to get latestBlockLocator: error=%v", err)
		return
	}
	prevBlock, err := db.FetchBlockBySha(latestBlockLocator[0])
	if err != nil {
		log.Errorf("Failed to fetch best block from memdb: error=%v", err)
		return
	}

	// ensure height is up to 110 so first few blocks are spendable.
	for height := prevBlock.Height(); height < 110; height++ {
		newBlock, err := regtester.ExtendChainEmpty(net, chain, prevBlock, subsidyAddress, btcd)
		if err != nil {
			log.Errorf("Failed to extend chain with empty block")
			return
		}

		// necessary to ensure next block timestamp is after this one
		time.Sleep(time.Second)

		prevBlock = newBlock
	}

	err = regtester.SpendCoinbaseTransaction(net, db, btcd, 1, testNetMinerPrivateKey, testNetSendPublicKeyAddr)
	if err != nil {
		log.Errorf("Failed to spend coinbase transaction from height 1")
		return
	}
	err = regtester.SpendCoinbaseTransaction(net, db, btcd, 2, testNetMinerPrivateKey, testNetSendPublicKeyAddr)
	if err != nil {
		log.Errorf("Failed to extend coinbase transaction from height 2")
		return
	}

	_, err = regtester.ExtendChainWithAllMempool(net, chain, prevBlock, subsidyAddress, btcd)
	if err != nil {
		log.Errorf("Failed to extend chain with mempool transactions")
		return
	}
}
