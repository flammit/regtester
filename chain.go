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
		block.SetHeight(height)

		err = chain.ProcessBlock(block, false)
		if err != nil {
			return nil, nil, err
		}
	}

	return chain, db, nil
}
