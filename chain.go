package regtester

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/conformal/btcchain"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/flammit/btcdcommander"
)

func extendChain(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander, txs []*btcutil.Tx) (*btcutil.Block, error) {
	newBlock, err := GenerateNewBlock(net, chain, prevBlock, subsidyAddress, txs)
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

// ExtendChainEmpty creates a new block that extends the main chain
// but contains no transactions.
func ExtendChainEmpty(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander) (*btcutil.Block, error) {
	return extendChain(net, chain, prevBlock, subsidyAddress, btcd, nil)
}

// ExtendChainWithAllMempool creates a new block that extends the main
// chain and contains all the transactions that are currently in
// the mempool of the btcd instance.
func ExtendChainWithAllMempool(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander) (*btcutil.Block, error) {
	mempoolTxs, err := RetrieveCurrentMempoolTxs(btcd)
	if err != nil {
		return nil, err
	}
	return extendChain(net, chain, prevBlock, subsidyAddress, btcd, mempoolTxs)
}
