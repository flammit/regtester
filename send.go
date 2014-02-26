package regtester

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/conformal/btcec"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/flammit/btcdcommander"
	"math/big"
)

var (
	ErrNetMismatch          = errors.New("invalid bitcoin networks don't match")
	ErrInvalidOutpointIndex = errors.New("invalid outpoint index for transaction input")

	zeroByte    = []byte{0}
	sigHashCode = []byte{1, 0, 0, 0}
	sigHashType = []byte{1}
)

type TxInDetails struct {
	Tx    *btcutil.Tx
	Index uint32
	PkWif string
}

func decodeKeyPair(net btcwire.BitcoinNet, pkWif string) (*ecdsa.PrivateKey, bool, error) {
	pk, pkNet, compressed, err := btcutil.DecodePrivateKey(pkWif)
	if err != nil {
		return nil, false, err
	}

	_ = pkNet
	/*
		if pkNet != net {
			return nil, ErrNetMismatch
		}
	*/

	x, y := btcec.S256().ScalarBaseMult(pk)

	pubKey := &ecdsa.PublicKey{
		Curve: btcec.S256(),
		X:     x,
		Y:     y,
	}

	return &ecdsa.PrivateKey{
		PublicKey: *pubKey,
		D:         new(big.Int).SetBytes(pk),
	}, compressed, nil
}

// SendTransaction creates a signed transaction
// that spends a coins created by new blocks mined in regtester.
func SendTransaction(
	net btcwire.BitcoinNet,
	txIns []*TxInDetails,
	txOuts []*btcwire.TxOut,
	btcd *btcdcommander.Commander,
) (*btcutil.Tx, error) {
	mtx := btcwire.NewMsgTx()
	for _, txIn := range txIns {
		if txIn.Index >= uint32(len(txIn.Tx.MsgTx().TxOut)) {
			return nil, ErrInvalidOutpointIndex
		}
		mtx.AddTxIn(&btcwire.TxIn{
			PreviousOutpoint: btcwire.OutPoint{*txIn.Tx.Sha(), txIn.Index},
			SignatureScript:  zeroByte,
			Sequence:         btcwire.MaxTxInSequenceNum,
		})
	}
	for _, txOut := range txOuts {
		mtx.AddTxOut(txOut)
	}

	// sign each input
	for i, txIn := range txIns {
		privateKey, compress, err := decodeKeyPair(net, txIns[i].PkWif)
		if err != nil {
			return nil, err
		}

		subscript := txIn.Tx.MsgTx().TxOut[txIn.Index].PkScript

		scriptSig, err := btcscript.SignatureScript(mtx, i, subscript, btcscript.SigHashAll,
			privateKey, compress)
		if err != nil {
			return nil, err
		}

		mtx.TxIn[i].SignatureScript = scriptSig
	}

	txBytes := new(bytes.Buffer)
	err := mtx.Serialize(txBytes)
	if err != nil {
		return nil, err
	}

	txHex := hex.EncodeToString(txBytes.Bytes())
	log.Infof("Sending Raw Transaction: hex=%s", txHex)
	_, jsonErr := btcd.SendRawTransaction(txHex)
	if jsonErr != nil {
		return nil, errors.New(jsonErr.Message)
	}

	return btcutil.NewTx(mtx), nil
}
