package main

import (
	"bytes"
	"encoding/hex"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/conformal/btcws"
	"github.com/flammit/btcdcommander"
	"github.com/flammit/regtester"
	"time"
)

var (
	testNetPublicKeyHash = "n1hoJmy2JQcHQxMatmfy7QDW5nHTuH3peN"
	testNetPrivateKey    = "cV7u7JEeoSTzWiiEvTcBERHnTmftTGPWMKYRwDgv714gWHZ81BTh"

	defaultLogFile = "test.log"
)

func main() {
	setLogLevels("info")
	defer backendLog.Flush()

	net := btcwire.TestNet

	subsidyAddress, err := btcutil.DecodeAddr(testNetPublicKeyHash)
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
			if _, ok := cmd.(*btcws.BlockConnectedNtfn); ok {
				// addedBtcd <- struct{}{}
			}
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

	for numBlocks := 0; numBlocks < 6; numBlocks++ {
		newBlock, err := regtester.GenerateNewBlock(net, chain, prevBlock, subsidyAddress, nil)
		if err != nil {
			log.Errorf("Failed to generate new block: error=%v", err)
			return
		}

		msgBlock := newBlock.MsgBlock()
		blockHash, err := msgBlock.BlockSha()
		if err != nil {
			log.Errorf("Failed to calculate block hash: error=%v", err)
			return
		}

		blockBytes := new(bytes.Buffer)
		err = msgBlock.Serialize(blockBytes)
		if err != nil {
			log.Errorf("Failed to serialize block: error=%v", err)
			return
		}
		blockHexString := hex.EncodeToString(blockBytes.Bytes())
		log.Infof("Block hash (%d): %s", newBlock.Height(), blockHash.String())

		// update our local chain, make sure it adds
		err = chain.ProcessBlock(newBlock, false)
		if err != nil {
			log.Errorf("Failed to add block to chain: error=%v", err)
			return
		}

		response, jsonErr := btcd.SubmitBlock(blockHexString)
		if jsonErr != nil {
			log.Errorf("Failed to submit block to btcd: err=%v", jsonErr)
			return
		}
		if response != nil {
			log.Errorf("Failed to submit block: response is '%#v'", response)
			return
		}
		log.Infof("Sent Block %d", newBlock.Height())

		// <-addedBtcd

		// necessary to ensure next block timestamp is after this one
		time.Sleep(time.Second)

		prevBlock = newBlock
	}

}
