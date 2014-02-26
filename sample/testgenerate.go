package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/conformal/btcchain"
	"github.com/conformal/btcdb"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/conformal/btcws"
	"github.com/flammit/btcdcommander"
	"github.com/flammit/regtester"
	"time"
)

var (
	testNetPublicKeyAddr = "n1hoJmy2JQcHQxMatmfy7QDW5nHTuH3peN"
	testNetPrivateKey    = "cV7u7JEeoSTzWiiEvTcBERHnTmftTGPWMKYRwDgv714gWHZ81BTh"

	testNetSendPublicKeyAddr = "mrcQmNDsFsZfhXtbwVcB2enm8pgqfNtMts"
	testNetSendPrivateKey    = "cQrsP8dzR9BoqjF51rfooWyBEiAtoZ4A1BQ1pTuCXmipwSDJsJ7W"

	defaultLogFile = "test.log"

	ErrNoTxInfo = errors.New("couldn't find tx")
)

func extendChainEmpty(
	net btcwire.BitcoinNet,
	chain *btcchain.BlockChain,
	prevBlock *btcutil.Block,
	subsidyAddress btcutil.Address,
	btcd *btcdcommander.Commander,
) (*btcutil.Block, error) {
	newBlock, err := regtester.GenerateNewBlock(net, chain, prevBlock, subsidyAddress, nil)
	if err != nil {
		log.Errorf("Failed to generate new block: error=%v", err)
		return nil, err
	}

	msgBlock := newBlock.MsgBlock()
	blockHash, err := msgBlock.BlockSha()
	if err != nil {
		log.Errorf("Failed to calculate block hash: error=%v", err)
		return nil, err
	}

	blockBytes := new(bytes.Buffer)
	err = msgBlock.Serialize(blockBytes)
	if err != nil {
		log.Errorf("Failed to serialize block: error=%v", err)
		return nil, err
	}
	blockHexString := hex.EncodeToString(blockBytes.Bytes())
	log.Infof("Block hash (%d): %s", newBlock.Height(), blockHash.String())

	// update our local chain, make sure it adds
	err = chain.ProcessBlock(newBlock, false)
	if err != nil {
		log.Errorf("Failed to add block to chain: error=%v", err)
		return nil, err
	}

	response, jsonErr := btcd.SubmitBlock(blockHexString)
	if jsonErr != nil {
		log.Errorf("Failed to submit block to btcd: err=%v", jsonErr)
		return nil, errors.New(jsonErr.Message)
	}
	if response != nil {
		log.Errorf("Failed to submit block: response is '%#v'", response)
		return nil, errors.New(response.(string))
	}
	log.Infof("Sent Block %d", newBlock.Height())
	return newBlock, nil
}

func retrieveCoinbaseTransaction(db btcdb.Db, height int64) (*btcutil.Tx, error) {
	blockSha, err := db.FetchBlockShaByHeight(height)
	if err != nil {
		log.Errorf("Failed to get block sha for height 1: error=%v", err)
		return nil, err
	}
	block, err := db.FetchBlockBySha(blockSha)
	if err != nil {
		log.Errorf("Failed to get block for height 1: error=%v", err)
		return nil, err
	}

	coinbaseTxSha := block.Transactions()[0].Sha()

	txList, err := db.FetchTxBySha(coinbaseTxSha)
	if err != nil || len(txList) == 0 {
		return nil, ErrNoTxInfo
	}

	mtx := txList[len(txList)-1].Tx
	return btcutil.NewTx(mtx), nil
}

func spendCoinbaseTransaction(
	net btcwire.BitcoinNet,
	db btcdb.Db,
	btcd *btcdcommander.Commander,
	height int64,
	pubKeyHash string,
) error {
	tx, err := retrieveCoinbaseTransaction(db, height)
	if err != nil {
		log.Error("Failed to retreive coinbase transaction to spend: error=%v", err)
		return err
	}

	txIns := []*regtester.TxInDetails{
		&regtester.TxInDetails{
			Tx:    tx,
			Index: 0,
			PkWif: testNetPrivateKey,
		},
	}

	outAddress, err := btcutil.DecodeAddr(pubKeyHash)
	if err != nil {
		log.Errorf("Failed to decode public key hash of target address: error=%v", err)
		return err
	}
	outPkScript, err := btcscript.PayToAddrScript(outAddress)
	if err != nil {
		log.Errorf("Failed to generate pay to addr script: error=%v", err)
		return err
	}
	txOuts := []*btcwire.TxOut{
		&btcwire.TxOut{
			Value:    50 * btcutil.SatoshiPerBitcoin,
			PkScript: outPkScript,
		},
	}

	sentTx, err := regtester.SendTransaction(net, txIns, txOuts, btcd)
	if err != nil {
		log.Errorf("Failed to spend transaction: error=%v", err)
		return err
	}

	sentTxBytes := new(bytes.Buffer)
	sentTx.MsgTx().Serialize(sentTxBytes)
	log.Infof("Tx sha: %s", sentTx.Sha().String())
	log.Infof("Tx hex: %64x", sentTxBytes.Bytes())

	return nil
}

func main() {
	setLogLevels("info")
	defer backendLog.Flush()

	net := btcwire.TestNet

	subsidyAddress, err := btcutil.DecodeAddr(testNetPublicKeyAddr)
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

	for numBlocks := 0; numBlocks < 101; numBlocks++ {
		newBlock, err := extendChainEmpty(net, chain, prevBlock, subsidyAddress, btcd)
		if err != nil {
			log.Errorf("Failed to extend chain with empty block")
			return
		}
		// <-addedBtcd

		// necessary to ensure next block timestamp is after this one
		time.Sleep(time.Second)

		prevBlock = newBlock
	}

	spendCoinbaseTransaction(net, db, btcd, 1, testNetSendPublicKeyAddr)
}
