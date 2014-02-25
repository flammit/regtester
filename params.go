package regtester

import (
	"github.com/conformal/btcchain"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
)

type MiningParams struct {
	Subsidy                int64
	SubsidyHalvingInterval int64
	ChainParams            *btcchain.Params
}

func (mp *MiningParams) BlockSubsidy(height int64) int64 {
	subsidy := 50 * btcutil.SatoshiPerBitcoin
	subsidy >>= uint64(height / mp.SubsidyHalvingInterval)
	return subsidy
}

var (
	mainNetMiningParams = MiningParams{
		Subsidy:                50,
		SubsidyHalvingInterval: 210000,
		ChainParams:            btcchain.ChainParams(btcwire.MainNet),
	}

	testNetMiningParams = MiningParams{
		Subsidy:                50,
		SubsidyHalvingInterval: 210000,
		ChainParams:            btcchain.ChainParams(btcwire.TestNet3),
	}

	regressionNetMiningParams = MiningParams{
		Subsidy:                50,
		SubsidyHalvingInterval: 150,
		ChainParams:            btcchain.ChainParams(btcwire.TestNet),
	}
)

func ChainMiningParams(btcnet btcwire.BitcoinNet) *MiningParams {
	switch btcnet {
	case btcwire.TestNet:
		return &regressionNetMiningParams
	case btcwire.TestNet3:
		return &testNetMiningParams
	case btcwire.MainNet:
		fallthrough
	default:
		return &mainNetMiningParams
	}
}
