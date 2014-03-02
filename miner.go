package regtester

import (
	"errors"
	"github.com/conformal/btcchain"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"math"
	"math/big"
	"time"
)

var (
	coinbaseFlags = "/P2SH/"

	bigOne = big.NewInt(1)

	// Used for compatibility with bitcoind miner, it doesn't return a successful
	// hash less than this minTarget
	bitcoindMinTarget = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 240), bigOne)
)

var (
	ErrValidBlockHashNotFound = errors.New("couldn't find valid block hash")
)

// GenerateCoinbaseTx creates a new coinbase transaction with a single
// outpoint whose script is given by address.
// NOTE: the value is set to zero and must be set after the block
// contents is finalized.
func GenerateCoinbaseTx(coinbase []byte, address btcutil.Address) (*btcwire.MsgTx, error) {
	tx := btcwire.NewMsgTx()
	tx.AddTxIn(&btcwire.TxIn{
		PreviousOutpoint: btcwire.OutPoint{btcwire.ShaHash{}, btcwire.MaxTxInSequenceNum},
		SignatureScript:  coinbase,
		Sequence:         btcwire.MaxTxInSequenceNum,
	})
	pkScript, err := btcscript.PayToAddrScript(address)
	if err != nil {
		return nil, err
	}
	tx.AddTxOut(&btcwire.TxOut{
		PkScript: pkScript,
		Value:    0,
	})
	return tx, nil
}

// TODO: add priority
type blockTx struct {
	Tx             *btcutil.Tx
	TxInputAmounts []int64
}

func calcBlockTx(chain *btcchain.BlockChain, txs []*btcutil.Tx) ([]*blockTx, error) {
	if txs == nil {
		return []*blockTx{}, nil
	}

	blockTxs := make([]*blockTx, 0)

transaction:
	for _, tx := range txs {
		txStore, err := chain.FetchTransactionStore(tx)
		if err != nil {
			return nil, err
		}

		mtx := tx.MsgTx()
		blockTx := &blockTx{
			Tx:             tx,
			TxInputAmounts: make([]int64, len(mtx.TxIn)),
		}
		for txInIndex, txIn := range mtx.TxIn {
			var inMsgTx *btcwire.MsgTx

			txData, ok := txStore[txIn.PreviousOutpoint.Hash]
			if !ok || txData.Err != nil {
				// check our txs
				for _, t := range txs {
					if t.Sha().IsEqual(&txIn.PreviousOutpoint.Hash) {
						inMsgTx = t.MsgTx()
					}
				}
			} else {
				inMsgTx = txData.Tx.MsgTx()
			}

			if inMsgTx == nil {
				log.Infof("Skipping transaction because no input available")
				continue transaction
			}

			if int(txIn.PreviousOutpoint.Index) >= len(inMsgTx.TxOut) {
				return nil, ErrInvalidOutpointIndex
			}

			inMsgTxOut := inMsgTx.TxOut[txIn.PreviousOutpoint.Index]
			blockTx.TxInputAmounts[txInIndex] += inMsgTxOut.Value
		}
		blockTxs = append(blockTxs, blockTx)
	}

	return blockTxs, nil
}

// GenerateNewBlock creates a new block whose parent is prevBlock
// and which potentially contains all of the transactions in txs.
// The subsidy will go to the subsidyAddress.
func GenerateNewBlock(
	net btcwire.BitcoinNet,
	chain *btcchain.BlockChain,
	prevBlock *btcutil.Block,
	subsidyAddress btcutil.Address,
	txs []*btcutil.Tx,
	blockTime *time.Time,
) (*btcutil.Block, error) {
	// TODO: allow a coinbase tx generator function given total fees
	// to set coinbase vouts
	miningParams := ChainMiningParams(net)

	// setup block header
	prevHash, err := prevBlock.Sha()
	if err != nil {
		return nil, err
	}

	// TODO: calculate correctly based on retargeting
	nextDifficulty := prevBlock.MsgBlock().Header.Bits

	newBlockHeader := btcwire.NewBlockHeader(prevHash, &btcwire.ShaHash{}, nextDifficulty, 0)
	if blockTime != nil {
		newBlockHeader.Timestamp = *blockTime
	}
	newMsgBlock := btcwire.NewMsgBlock(newBlockHeader)
	newBlockHeight := prevBlock.Height() + 1
	newExtraNonce := 0

	// add coinbase transaction
	coinbaseScript := btcscript.NewScriptBuilder()
	// BIP0034 - block version 2 needs block height at start of coinbase
	coinbaseScript.AddInt64(int64(newBlockHeight))
	coinbaseScript.AddInt64(int64(newExtraNonce))
	coinbaseScript.AddData([]byte(coinbaseFlags))
	coinbaseTx, err := GenerateCoinbaseTx(coinbaseScript.Script(), subsidyAddress)
	if err != nil {
		return nil, err
	}

	newMsgBlock.AddTransaction(coinbaseTx)

	// calculate fees and total value for coinbase
	var totalFees int64

	blockTxs, err := calcBlockTx(chain, txs)
	if err != nil {
		return nil, err
	}
	for _, blockTx := range blockTxs {
		mtx := blockTx.Tx.MsgTx()
		// check inputs
		var inputValue int64
		for n := 0; n < len(blockTx.TxInputAmounts); n++ {
			inputValue += blockTx.TxInputAmounts[n]
		}

		var outputValue int64
		for _, txOut := range mtx.TxOut {
			outputValue += txOut.Value
		}

		totalFees += (inputValue - outputValue)
		newMsgBlock.AddTransaction(mtx)
	}

	// set coinbase value correctly
	coinbaseTx.TxOut[0].Value = totalFees + miningParams.BlockSubsidy(newBlockHeight)

	// set merkle root
	newBlock := btcutil.NewBlock(newMsgBlock)
	newBlock.SetHeight(newBlockHeight)
	merkleTreeStore := btcchain.BuildMerkleTreeStore(newBlock)
	newMsgBlock.Header.MerkleRoot = *merkleTreeStore[len(merkleTreeStore)-1]

	return CalculateNewBlockHash(newBlock)
}

// CalculateNewBlockHash iterates mutable fields to attempt to calculate
// a mutable block.
func CalculateNewBlockHash(newBlock *btcutil.Block) (*btcutil.Block, error) {
	msgBlock := newBlock.MsgBlock()

	// loop over standard block header nonce to get valid hash
	target := btcchain.CompactToBig(msgBlock.Header.Bits)
	for ; msgBlock.Header.Nonce < math.MaxInt32; msgBlock.Header.Nonce++ {
		hash, err := msgBlock.BlockSha()
		if err != nil {
			return nil, err
		}

		hashNum := btcchain.ShaHashToBig(&hash)
		if hashNum.Cmp(target) <= 0 && hashNum.Cmp(bitcoindMinTarget) <= 0 {
			return newBlock, nil
		}
	}

	return nil, ErrValidBlockHashNotFound
}
