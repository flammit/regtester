package regtester

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"github.com/conformal/btcdb"
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
	ErrNotEnoughFunds       = errors.New("unspent transaction doesn't have enough funds")
)

type TxInDetails struct {
	Tx    *btcutil.Tx
	Index uint32
	PkWif string
}

func decodeKeyPair(pkWif string) (*ecdsa.PrivateKey, bool, error) {
	pk, _, compressed, err := btcutil.DecodePrivateKey(pkWif)
	if err != nil {
		return nil, false, err
	}

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

// SendTransaction creates a signed transaction and sends to
// btcd using sendrawtransaction.
func SendTransaction(net btcwire.BitcoinNet, txIns []*TxInDetails, txOuts []*btcwire.TxOut, btcd *btcdcommander.Commander) (*btcutil.Tx, error) {
	mtx := btcwire.NewMsgTx()
	for _, txIn := range txIns {
		if txIn.Index >= uint32(len(txIn.Tx.MsgTx().TxOut)) {
			return nil, ErrInvalidOutpointIndex
		}
		mtx.AddTxIn(&btcwire.TxIn{
			PreviousOutpoint: btcwire.OutPoint{*txIn.Tx.Sha(), txIn.Index},
			SignatureScript:  nil,
			Sequence:         btcwire.MaxTxInSequenceNum,
		})
	}
	for _, txOut := range txOuts {
		mtx.AddTxOut(txOut)
	}

	// sign each input
	for i, txIn := range txIns {
		privateKey, compress, err := decodeKeyPair(txIns[i].PkWif)
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
	log.Infof("Tx hex: %s", txHex)
	_, jsonErr := btcd.SendRawTransaction(txHex)
	if jsonErr != nil {
		return nil, errors.New(jsonErr.Message)
	}
	return btcutil.NewTx(mtx), nil
}

func PubKeyHashTxOut(pubKeyHash string, value int64) (*btcwire.TxOut, error) {
	outAddress, err := btcutil.DecodeAddr(pubKeyHash)
	if err != nil {
		log.Errorf("Failed to decode public key hash of target address: error=%v", err)
		return nil, err
	}
	outPkScript, err := btcscript.PayToAddrScript(outAddress)
	if err != nil {
		log.Errorf("Failed to generate pay to addr script: error=%v", err)
		return nil, err
	}
	return &btcwire.TxOut{
		Value:    value,
		PkScript: outPkScript,
	}, nil
}

// SpendCoinbaseTransaction sends the coinbase transaction value at
// the given height to the pubKeyHash specified.
func SpendCoinbaseTransaction(net btcwire.BitcoinNet, db btcdb.Db, btcd *btcdcommander.Commander, height int64, subsidyPrivateKeyWif string, pubKeyHash string) (*btcutil.Tx, error) {
	tx, err := RetrieveCoinbaseTransaction(db, height)
	if err != nil {
		log.Error("Failed to retreive coinbase transaction to spend: error=%v", err)
		return nil, err
	}

	txIns := []*TxInDetails{
		&TxInDetails{
			Tx:    tx,
			Index: 0,
			PkWif: subsidyPrivateKeyWif,
		},
	}

	txOut, err := PubKeyHashTxOut(pubKeyHash, tx.MsgTx().TxOut[0].Value)
	if err != nil {
		return nil, err
	}
	txOuts := []*btcwire.TxOut{txOut}

	sentTx, err := SendTransaction(net, txIns, txOuts, btcd)
	if err != nil {
		log.Errorf("Failed to spend transaction: error=%v", err)
		return nil, err
	}

	sentTxBytes := new(bytes.Buffer)
	sentTx.MsgTx().Serialize(sentTxBytes)
	log.Infof("Tx sha: %s", sentTx.Sha().String())

	return sentTx, nil
}

// SendFromTxToAddress keeps change in vout 0
func SendFromTxToAddress(net btcwire.BitcoinNet, btcd *btcdcommander.Commander, tx *btcutil.Tx, sourceAddress, sourcePk, address string, amount int64) (*btcutil.Tx, error) {
	txIns := []*TxInDetails{
		&TxInDetails{
			Tx:    tx,
			Index: 0,
			PkWif: sourcePk,
		},
	}
	change := tx.MsgTx().TxOut[0].Value - amount
	if change < 0 {
		return nil, ErrNotEnoughFunds
	}
	txChange, err := PubKeyHashTxOut(sourceAddress, change)
	if err != nil {
		return nil, err
	}
	txOut, err := PubKeyHashTxOut(address, amount)
	if err != nil {
		return nil, err
	}
	txOuts := []*btcwire.TxOut{txChange, txOut}
	sentTx, err := SendTransaction(net, txIns, txOuts, btcd)
	log.Infof("Tx sha: %s", sentTx.Sha().String())
	return sentTx, err
}
