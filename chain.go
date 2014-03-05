package regtester

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/conformal/btcchain"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/flammit/btcdcommander"
	"time"
)

func extendChain(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander, txs []*btcutil.Tx, blockTime *time.Time) (*btcutil.Block, error) {
	newBlock, err := GenerateNewBlock(net, chain, prevBlock, subsidyAddress, txs, blockTime)
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

// ExtendChainEmptyWithTime creates a new block that extends the main chain
// but contains no transactions with a specified block time.
func ExtendChainEmptyWithTime(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander, time *time.Time) (*btcutil.Block, error) {
	return extendChain(net, chain, prevBlock, subsidyAddress, btcd, nil, time)
}

// ExtendChainEmpty creates a new block that extends the main chain
// but contains no transactions.
func ExtendChainEmpty(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander) (*btcutil.Block, error) {
	return extendChain(net, chain, prevBlock, subsidyAddress, btcd, nil, nil)
}

// ExtendChainWithAllMempool creates a new block that extends the main
// chain and contains all the transactions that are currently in
// the mempool of the btcd instance.
func ExtendChainWithAllMempool(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander) (*btcutil.Block, error) {
	mempoolTxs, err := RetrieveCurrentMempoolTxs(btcd)
	if err != nil {
		return nil, err
	}
	return extendChain(net, chain, prevBlock, subsidyAddress, btcd, mempoolTxs, nil)
}

// ExtendChainWithAllMalleatedMempool creates a new block that extends the main
// chain and contains all the transactions that are currently in
// the mempool of the btcd instance.
func ExtendChainWithAllMalleatedMempool(net btcwire.BitcoinNet, chain *btcchain.BlockChain, prevBlock *btcutil.Block, subsidyAddress btcutil.Address, btcd *btcdcommander.Commander) (*btcutil.Block, error) {
	mempoolTxs, err := RetrieveCurrentMempoolTxs(btcd)

	malMempoolTxs := make([]*btcutil.Tx, 0)
transactions:
	for _, tx := range mempoolTxs {
		log.Infof("Trying to add malleated tx: origTx.sha=%s", tx.Sha())
		// make sure inputs aren't in mempool, is so remove
		for _, in := range tx.MsgTx().TxIn {
			inSha := in.PreviousOutpoint.Hash
			for _, txCheck := range mempoolTxs {
				if inSha.IsEqual(txCheck.Sha()) {
					log.Infof("Transaction depends on another being malleated, skipping: tx.sha=%v, txinput.sha%v",
						tx.Sha(), txCheck.Sha())
					continue transactions
				}
			}
		}

		malTx := malleateTxAddOp0(tx)
		log.Infof("Added malleated tx: malTx.sha=%s", malTx.Sha())
		malMempoolTxs = append(malMempoolTxs, malTx)
	}
	if err != nil {
		return nil, err
	}
	return extendChain(net, chain, prevBlock, subsidyAddress, btcd, malMempoolTxs, nil)
}

// malleateTxAddOp0 takes a transaction and creates a new valid transaction with a
// different transaction hash by adding OP_0 to the first input's script.
func malleateTxAddOp0(tx *btcutil.Tx) *btcutil.Tx {
	oldMtx := tx.MsgTx()
	newMtx := oldMtx.Copy()

	oldBytes := oldMtx.TxIn[0].SignatureScript
	newBytes := make([]byte, len(oldBytes)+1)
	newBytes[0] = btcscript.OP_0
	copy(newBytes[1:], oldBytes[0:])

	newMtx.TxIn[0].SignatureScript = newBytes

	return btcutil.NewTx(newMtx)
}
