package regtester

import (
	"encoding/hex"
	"errors"
	"github.com/conformal/btcchain"
	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/memdb"
	"github.com/conformal/btcutil"
	"github.com/flammit/btcdcommander"
)

var (
	ErrNoTxInfo = errors.New("couldn't find tx")
)

// SyncChain puts the pulls the full blockchain from btcd
// and places it in a memdb instance of the BlockChain
func SyncChain(btcd *btcdcommander.Commander) (*btcchain.BlockChain, btcdb.Db, error) {
	net := btcd.Cfg.Net()
	chainParams := btcchain.ChainParams(net)

	db, err := btcdb.CreateDB("memdb")
	if err != nil {
		log.Errorf("Failed to make new memdb: error=%v", err)
		return nil, nil, err
	}
	genesisBlock := btcutil.NewBlock(chainParams.GenesisBlock)
	genesisBlock.SetHeight(0)
	db.InsertBlock(genesisBlock)

	chain := btcchain.New(db, net, nil)

	bestBlockInfo, jsonErr := btcd.GetBestBlock()
	if jsonErr != nil {
		return nil, nil, errors.New(jsonErr.Message)
	}

	for height := int64(1); height <= int64(bestBlockInfo.Height); height++ {
		blockHash, jsonErr := btcd.GetBlockHash(height)
		if jsonErr != nil {
			return nil, nil, errors.New(jsonErr.Message)
		}

		blockHex, jsonErr := btcd.GetRawBlock(blockHash)
		if jsonErr != nil {
			return nil, nil, errors.New(jsonErr.Message)
		}

		blockBytes, err := hex.DecodeString(blockHex)
		if err != nil {
			return nil, nil, err
		}

		block, err := btcutil.NewBlockFromBytes(blockBytes)
		if err != nil {
			return nil, nil, err
		}
		block.SetHeight(height)

		err = chain.ProcessBlock(block, false)
		if err != nil {
			return nil, nil, err
		}
	}

	return chain, db, nil
}

// RetrieveCurrentMempoolTxs returns all the transactions currently in the
// mempool of the btcd instance.
func RetrieveCurrentMempoolTxs(btcd *btcdcommander.Commander) ([]*btcutil.Tx, error) {
	txShas, jsonErr := btcd.GetRawMempool()
	if jsonErr != nil {
		return nil, errors.New(jsonErr.Message)
	}

	txs := make([]*btcutil.Tx, len(txShas))
	for i, txSha := range txShas {
		txHex, jsonErr := btcd.GetRawTransaction(txSha)
		if jsonErr != nil {
			return nil, errors.New(jsonErr.Message)
		}

		txBytes, err := hex.DecodeString(txHex)
		if err != nil {
			return nil, err
		}

		txs[i], err = btcutil.NewTxFromBytes(txBytes)
		if err != nil {
			return nil, err
		}
	}

	return txs, nil
}

// RetrieveCoinbaseTransaction returns the coinbase transaction for the
// block at the given height in the main chain.
func RetrieveCoinbaseTransaction(db btcdb.Db, height int64) (*btcutil.Tx, error) {
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
