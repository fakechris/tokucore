// tokucore
//
// Copyright (c) 2018 TokuBlock
// BSD License

package xcore

import (
	"encoding/hex"

	"github.com/tokublock/tokucore/xbase"
	"github.com/tokublock/tokucore/xcrypto"
	"github.com/tokublock/tokucore/xerror"
	"github.com/tokublock/tokucore/xvm"
)

const (
	// Unit -- satoshi unit
	Unit = 1e8
)

type change struct {
	addr Address
}

type output struct {
	addr   Address
	value  uint64
	script string
}

type input struct {
	compressed bool
	keys       []*xcrypto.PrvKey
}

// Group -- the group includes from/sendto/changeto.
type Group struct {
	coin         *Coin
	keys         []*xcrypto.PrvKey
	output       *output
	redeemScript []byte
	stepin       bool
	compressed   bool
}

// TransactionBuilder --
type TransactionBuilder struct {
	idx           int
	sign          bool
	maxFees       int64
	sendFees      int64
	relayFeePerKb int64
	lockTime      uint32
	change        *change
	groups        []Group
	pushDatas     [][]byte
}

// NewTransactionBuilder -- creates new TransactionBuilder.
func NewTransactionBuilder() *TransactionBuilder {
	// Default all is 1000 satoshis.
	return &TransactionBuilder{
		sendFees: 1000,
		maxFees:  Unit * 1,
		groups:   make([]Group, 1),
	}
}

// AddCoin -- set the from coin.
func (b *TransactionBuilder) AddCoin(coin *Coin) *TransactionBuilder {
	b.groups[b.idx].stepin = true
	b.groups[b.idx].compressed = true
	b.groups[b.idx].coin = coin
	return b
}

// AddKeys -- set the private keys for signing.
func (b *TransactionBuilder) AddKeys(keys ...*xcrypto.PrvKey) *TransactionBuilder {
	b.groups[b.idx].stepin = true
	b.groups[b.idx].keys = keys
	return b
}

// To -- set the to address and value.
func (b *TransactionBuilder) To(addr Address, value uint64) *TransactionBuilder {
	b.groups[b.idx].stepin = true
	b.groups[b.idx].output = &output{
		value: value,
		addr:  addr,
	}
	return b
}

// SetRedeemScript -- set the redeemscript to group.
func (b *TransactionBuilder) SetRedeemScript(redeem []byte) *TransactionBuilder {
	b.groups[b.idx].stepin = true
	b.groups[b.idx].redeemScript = redeem
	return b
}

// SetPubKeyUncompressed -- set the pubkey to uncompressed format.
func (b *TransactionBuilder) SetPubKeyUncompressed() *TransactionBuilder {
	b.groups[b.idx].compressed = false
	return b
}

// SetChange -- set the change address.
func (b *TransactionBuilder) SetChange(addr Address) *TransactionBuilder {
	b.change = &change{addr: addr}
	return b
}

// SendFees -- set the amount fee of this send.
func (b *TransactionBuilder) SendFees(fees uint64) *TransactionBuilder {
	b.sendFees = int64(fees)
	return b
}

// SetRelayFeePerKb -- set the relay fee per Kb.
func (b *TransactionBuilder) SetRelayFeePerKb(relayFeePerKb int64) *TransactionBuilder {
	b.relayFeePerKb = relayFeePerKb
	return b
}

// SetMaxFees -- set the max fee, the maxFees is non-zero after setting.
// If the tx fees larger than the max, it returns error after the building.
func (b *TransactionBuilder) SetMaxFees(max int64) *TransactionBuilder {
	b.maxFees = max
	return b
}

// SetLockTime -- set the locktime.
func (b *TransactionBuilder) SetLockTime(lockTime uint32) *TransactionBuilder {
	b.lockTime = lockTime
	return b
}

// AddPushData -- add pushdata, such as OP_RETURN.
func (b *TransactionBuilder) AddPushData(data []byte) *TransactionBuilder {
	b.pushDatas = append(b.pushDatas, data)
	return b
}

// Sign -- sets the sign flag to tell the builder do sign or not.
func (b *TransactionBuilder) Sign() *TransactionBuilder {
	b.sign = true
	return b
}

// Then --
// say that one group is end we will start a new one.
func (b *TransactionBuilder) Then() *TransactionBuilder {
	b.idx++
	b.groups = append(b.groups, Group{})
	return b
}

// BuildTransaction -- build the transaction.
func (b *TransactionBuilder) BuildTransaction() (*Transaction, error) {
	var totalIn int64
	var totalOut int64
	var estimateSize int64
	var estimateFees int64

	var inputs []*input
	var txins []*TxIn
	var txouts []*TxOut
	var changeTxOut *TxOut

	for i, group := range b.groups {
		if !group.stepin {
			continue
		}

		from := group.coin
		output := group.output

		// Input check.
		if from == nil {
			return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_FROM_EMPTY, i)
		}

		// Output check.
		if output == nil {
			return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_SENDTO_EMPTY, i)
		}

		// Input.
		{
			// Hex to TxID.
			txid, err := xbase.NewIDFromString(from.txID)
			if err != nil {
				return nil, err
			}
			// Hex to script.
			script, err := hex.DecodeString(from.script)
			if err != nil {
				return nil, err
			}
			txin := NewTxIn(txid, from.n, from.value, script, group.redeemScript)
			txins = append(txins, txin)
			totalIn += int64(from.value)

			// inputs.
			input := &input{
				keys:       group.keys,
				compressed: group.compressed,
			}
			inputs = append(inputs, input)
		}

		// Output.
		{
			if output != nil {
				var err error
				var script []byte

				if output.addr != nil {
					script, err = PayToAddrScript(output.addr)
					if err != nil {
						return nil, err
					}
				}

				if output.script != "" {
					script, err = hex.DecodeString(output.script)
					if err != nil {
						return nil, err
					}
				}
				txout := NewTxOut(output.value, script)
				txouts = append(txouts, txout)
				totalOut += int64(output.value)
			}
		}
	}

	// Estimate fee.
	fees := b.sendFees
	if b.relayFeePerKb > 0 {
		// Pushdata size.
		pushDataSize := 0
		for _, pushData := range b.pushDatas {
			pushDataSize += len(pushData)
		}

		estimateSize = EstimateSize(txins, txouts) + int64(pushDataSize)
		estimateFees = EstimateFees(estimateSize, b.relayFeePerKb)
		fees = estimateFees
	}
	if fees > b.maxFees {
		return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_FEE_TOO_HIGH, fees, b.maxFees)
	}

	// Check amount.
	if totalOut > totalIn {
		return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_AMOUNT_NOT_ENOUGH_ERROR, totalOut, totalIn)
	}
	changeAmount := totalIn - totalOut - fees
	if changeAmount < 0 {
		return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_MIN_FEE_NOT_ENOUGH, fees, (totalIn - totalOut))
	}

	// Change.
	{
		if changeAmount > 0 {
			if b.change == nil {
				return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_CHANGETO_EMPTY)
			}
			script, err := PayToAddrScript(b.change.addr)
			if err != nil {
				return nil, err
			}
			changeTxOut = NewTxOut(uint64(changeAmount), script)
		}
	}

	// Build tx.
	transaction := NewTransaction()
	{
		// LockTime.
		transaction.SetLockTime(b.lockTime)

		// Txin.
		for _, txin := range txins {
			transaction.AddInput(txin)
		}

		// Txout.
		for _, txout := range txouts {
			transaction.AddOutput(txout)
		}

		// Change Txout.
		if changeTxOut != nil {
			transaction.AddOutput(changeTxOut)
		}

		// Push datas.
		for _, data := range b.pushDatas {
			pushData, err := xvm.NewScriptBuilder().AddOp(xvm.OP_RETURN).AddData(data).Script()
			if err != nil {
				return nil, err
			}
			transaction.AddOutput(NewTxOut(0, pushData))
		}
	}

	// Sign.
	if b.sign {
		for i, input := range inputs {
			if input.keys == nil {
				return nil, xerror.NewError(Errors, ER_TRANSACTION_BUILDER_SIGN_KEY_EMPTY, i)
			}
			if err := transaction.SignIndex(i, input.compressed, input.keys...); err != nil {
				return nil, err
			}
		}
	}
	return transaction, nil
}
